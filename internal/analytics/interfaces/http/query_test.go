package http

import (
	"encoding/csv"
	"encoding/json"
	"io"
	"net/http"
	"strings"
	"testing"

	"github.com/avatarsik6699/smotryashchiy/internal/analytics/domain"
)

func TestQueryAnswersJSONAndCSVAndRejectsUnknownFields(t *testing.T) {
	srv, svc := newTestServer(t)
	site, err := svc.CreateSite(t.Context(), domain.NewSite{Name: "A", Domain: "a.example"})
	if err != nil {
		t.Fatal(err)
	}
	for _, path := range []string{"/", "/docs"} {
		beacon := `{"site":"` + site.ID + `","url":"` + path + `","utm":{"campaign":"-sale"}}`
		if resp := post(t, srv.URL+"/api/collect", "https://a.example", []byte(beacon)); resp.StatusCode != http.StatusNoContent {
			t.Fatalf("collect = %d", resp.StatusCode)
		}
	}
	url := srv.URL + "/api/sites/" + site.ID

	resp := post(t, url+"/query", "", []byte(`{"metrics":["pageviews"],"dimension":"page"}`))
	var res domain.QueryResult
	if err := json.NewDecoder(resp.Body).Decode(&res); err != nil || resp.StatusCode != http.StatusOK || len(res.Rows) != 2 {
		t.Fatalf("json query = %d %+v (%v)", resp.StatusCode, res, err)
	}

	resp = post(t, url+"/query", "", []byte(`{"metrics":["pageviews"],"dimension":"page","compare":"previous","format":"csv"}`))
	if ct := resp.Header.Get("Content-Type"); !strings.HasPrefix(ct, "text/csv") {
		t.Fatalf("content type = %q", ct)
	}
	lines, err := csv.NewReader(resp.Body).ReadAll()
	if err != nil {
		t.Fatal(err)
	}
	if strings.Join(lines[0], ",") != "page,pageviews,compare_pageviews" || len(lines) != 3 {
		t.Fatalf("csv = %v", lines)
	}
	resp = post(t, url+"/query", "", []byte(`{"dimension":"utm_campaign","format":"csv"}`))
	if lines, err = csv.NewReader(resp.Body).ReadAll(); err != nil || len(lines) != 2 || lines[1][0] != "'-sale" {
		t.Fatalf("a cell starting with - must be defused: %v (%v)", lines, err)
	}

	resp = post(t, url+"/query", "", []byte(`{"metric":["visits"]}`))
	if resp.StatusCode != http.StatusBadRequest {
		t.Fatalf("unknown field = %d, want 400", resp.StatusCode)
	}
	resp = post(t, url+"/query", "", []byte(`{"dimension":"time:hour","period":"7d"}`))
	body, _ := io.ReadAll(resp.Body)
	if resp.StatusCode != http.StatusBadRequest || !strings.Contains(string(body), "hour") {
		t.Fatalf("hour over a week = %d %s", resp.StatusCode, body)
	}

	resp, err = http.Get(url + "/realtime")
	if err != nil {
		t.Fatal(err)
	}
	var rt map[string]int
	if err := json.NewDecoder(resp.Body).Decode(&rt); err != nil || rt["visitors"] != 1 {
		t.Fatalf("realtime = %v (%v)", rt, err)
	}
}

func TestCSVCellDefusesFormulas(t *testing.T) {
	for in, want := range map[string]string{
		"=SUM(A1)": "'=SUM(A1)", "+1": "'+1", "-1": "'-1", "@x": "'@x", "\tx": "'\tx", "/docs": "/docs", "": "",
	} {
		if got := csvCell(in); got != want {
			t.Errorf("csvCell(%q) = %q, want %q", in, got, want)
		}
	}
}
