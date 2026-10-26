package application

import (
	"context"
	"errors"
	"sort"
	"strconv"
	"time"

	"github.com/avatarsik6699/smotryashchiy/internal/analytics/domain"
	"github.com/avatarsik6699/smotryashchiy/internal/platform/apierror"
)

// Change 36 (docs/SPEC.md §4i): the query API. A query reads raw rows for the site days the raw
// retention still holds whole and the daily aggregates for older days; counts are additive, so a
// range across the boundary sums both parts.

// queryDeadline bounds one query, its comparison included.
const queryDeadline = 10 * time.Second

// realtimeWindow is how far back "visitors on the site now" looks.
const realtimeWindow = 5 * time.Minute

// Source labels of a result.
const (
	SourceRaw        = "raw"
	SourceAggregates = "aggregates"
)

// QueryRepository is the persistence port of queries and the aggregate job.
type QueryRepository interface {
	ListSites(ctx context.Context) ([]domain.Site, error)
	SiteExists(ctx context.Context, id string) (domain.Site, bool, error)
	RawRows(ctx context.Context, siteID, dimension string, from, to time.Time, filters []domain.Filter) ([]domain.Row, error)
	RawTopRows(ctx context.Context, siteID, dimension string, from, to time.Time, filters []domain.Filter, metric string, limit int) ([]domain.Row, error)
	RawVisits(ctx context.Context, siteID string, from, to time.Time, filters []domain.Filter) ([]domain.TimelineVisit, error)
	DailyRows(ctx context.Context, siteID, dimension string, from, to domain.Day) ([]domain.Row, error)
	DailyTotals(ctx context.Context, siteID string, from, to domain.Day) ([]domain.Row, error)
	RefreshDay(ctx context.Context, siteID string, day domain.Day, start, end time.Time) error
	AggregatedDays(ctx context.Context, siteID string, from, to domain.Day) (map[string]bool, error)
	FirstVisit(ctx context.Context, siteID string) (time.Time, bool, error)
	PurgeDaily(ctx context.Context, before domain.Day) (int, error)
	Realtime(ctx context.Context, siteID string, since time.Time) (int, error)
}

// Queries answers the query API and keeps the daily aggregates.
type Queries struct {
	repo QueryRepository
	now  func() time.Time
	// retention is the raw retention (Options.Retention); 0 keeps raw rows forever.
	retention time.Duration
}

// NewQueries wires the query service; retention must match the raw purge.
func NewQueries(repo QueryRepository, now func() time.Time, retention time.Duration) *Queries {
	return &Queries{repo: repo, now: now, retention: retention}
}

// rawFrom is the first site day the raw rows still hold whole; the zero Day when nothing is purged.
func (q *Queries) rawFrom(loc *time.Location) domain.Day {
	if q.retention <= 0 {
		return domain.Day{}
	}
	return domain.DayOf(q.now().Add(-q.retention), loc).AddDays(1)
}

// Resolve turns a preset or an explicit date range into site days.
func Resolve(query domain.Query, today domain.Day) (domain.Range, error) {
	if len(query.DateRange) == 2 {
		from, err := domain.ParseDay(query.DateRange[0])
		if err != nil {
			return domain.Range{}, err
		}
		to, err := domain.ParseDay(query.DateRange[1])
		if err != nil {
			return domain.Range{}, err
		}
		if to.Before(from.Time) {
			return domain.Range{}, apierror.Invalid("date_range ends before it starts")
		}
		if from.Before(to.AddDate(0, -domain.MaxHistoryMonths, 0)) {
			return domain.Range{}, apierror.Invalid("date_range is longer than " + strconv.Itoa(domain.MaxHistoryMonths) + " months")
		}
		return domain.Range{From: from, To: to}, nil
	}
	monthStart := domain.Day{Time: today.AddDate(0, 0, 1-today.Day())}
	switch query.Period {
	case "", "today":
		return domain.Range{From: today, To: today}, nil
	case "yesterday":
		return domain.Range{From: today.AddDays(-1), To: today.AddDays(-1)}, nil
	case "7d":
		return domain.Range{From: today.AddDays(-6), To: today}, nil
	case "30d":
		return domain.Range{From: today.AddDays(-29), To: today}, nil
	case "month":
		return domain.Range{From: monthStart, To: today}, nil
	case "last_month":
		return domain.Range{From: domain.Day{Time: monthStart.AddDate(0, -1, 0)}, To: monthStart.AddDays(-1)}, nil
	case "12mo":
		return domain.Range{From: domain.Day{Time: monthStart.AddDate(0, -11, 0)}, To: today}, nil
	}
	return domain.Range{}, apierror.Invalid("period must be today, yesterday, 7d, 30d, month, last_month or 12mo")
}

// CompareRange is the period a range is compared with: the same number of days just before it, or
// the same dates a year earlier.
func CompareRange(r domain.Range, compare string) (domain.Range, bool) {
	switch compare {
	case domain.ComparePrevious:
		n := r.Days()
		return domain.Range{From: r.From.AddDays(-n), To: r.From.AddDays(-1)}, true
	case domain.CompareYearAgo:
		return domain.Range{From: domain.Day{Time: r.From.AddDate(-1, 0, 0)}, To: domain.Day{Time: r.To.AddDate(-1, 0, 0)}}, true
	}
	return domain.Range{}, false
}

// Query answers one validated-on-entry query for a site.
func (q *Queries) Query(ctx context.Context, siteID string, query domain.Query) (domain.QueryResult, error) {
	query, err := query.Validate()
	if err != nil {
		return domain.QueryResult{}, err
	}
	site, found, err := q.repo.SiteExists(ctx, siteID)
	if err != nil {
		return domain.QueryResult{}, err
	}
	if !found {
		return domain.QueryResult{}, apierror.NotFound("site not found")
	}
	loc := site.Location()
	r, err := Resolve(query, domain.DayOf(q.now(), loc))
	if err != nil {
		return domain.QueryResult{}, err
	}
	ctx, cancel := context.WithTimeout(ctx, queryDeadline)
	defer cancel()

	result := domain.QueryResult{DateRange: [2]string{r.From.String(), r.To.String()}, Timezone: loc.String(), Rows: []domain.ResultRow{}}
	cmp, comparing := CompareRange(r, query.Compare)
	if comparing {
		result.CompareRange = &[2]string{cmp.From.String(), cmp.To.String()}
	}
	result.Source = q.source(r, cmp, comparing, loc)
	if result.Source == SourceAggregates && len(query.Filters) > 0 {
		return domain.QueryResult{}, apierror.Invalid("filters need a range inside the last " + q.rawDaysText() + " days")
	}

	if interval, ok := domain.IsTimeDimension(query.Dimension); ok {
		if interval == "hour" && (r.Days() != 1 || result.Source == SourceAggregates) {
			return domain.QueryResult{}, apierror.Invalid("hour intervals need a single day inside the last " + q.rawDaysText() + " days")
		}
		cur, err := q.timeline(ctx, site, r, interval, query.Filters)
		if err != nil {
			return domain.QueryResult{}, deadline(err)
		}
		var prev []domain.Row
		if comparing {
			if prev, err = q.timeline(ctx, site, cmp, interval, query.Filters); err != nil {
				return domain.QueryResult{}, deadline(err)
			}
		}
		for i, row := range cur {
			out := domain.ResultRow{Value: row.Value, Metrics: metrics(query.Metrics, row.Counts)}
			if comparing && i < len(prev) {
				out.Compare = metrics(query.Metrics, prev[i].Counts)
				out.CompareValue = prev[i].Value
			}
			result.Rows = append(result.Rows, out)
		}
		return result, nil
	}

	// the current period may be cut in the database; the comparison needs every value it matches
	cur, err := q.breakdown(ctx, site, r, query.Dimension, query.Filters, &topCut{metric: query.OrderBy, limit: query.Limit})
	if err != nil {
		return domain.QueryResult{}, deadline(err)
	}
	var prev map[string]domain.Counts
	if comparing {
		rows, err := q.breakdown(ctx, site, cmp, query.Dimension, query.Filters, nil)
		if err != nil {
			return domain.QueryResult{}, deadline(err)
		}
		prev = make(map[string]domain.Counts, len(rows))
		for _, row := range rows {
			prev[row.Value] = row.Counts
		}
	}
	if query.Dimension == "" && len(cur) == 0 {
		cur = []domain.Row{{}}
	}
	sort.SliceStable(cur, func(i, j int) bool {
		a, b := cur[i].Counts.Metric(query.OrderBy), cur[j].Counts.Metric(query.OrderBy)
		if a != b {
			return a > b
		}
		return cur[i].Value < cur[j].Value
	})
	if len(cur) > query.Limit {
		cur = cur[:query.Limit]
	}
	for _, row := range cur {
		out := domain.ResultRow{Value: row.Value, Metrics: metrics(query.Metrics, row.Counts)}
		if comparing {
			out.Compare = metrics(query.Metrics, prev[row.Value])
		}
		result.Rows = append(result.Rows, out)
	}
	return result, nil
}

// source is "aggregates" when any requested day is older than the raw retention.
func (q *Queries) source(r, cmp domain.Range, comparing bool, loc *time.Location) string {
	from := q.rawFrom(loc)
	if r.From.Before(from.Time) || (comparing && cmp.From.Before(from.Time)) {
		return SourceAggregates
	}
	return SourceRaw
}

func (q *Queries) rawDaysText() string {
	return strconv.Itoa(int(q.retention.Hours() / 24))
}

func deadline(err error) error {
	if errors.Is(err, context.DeadlineExceeded) {
		return apierror.Invalid("the query took too long; narrow the range or the filters")
	}
	return err
}

func metrics(names []string, c domain.Counts) map[string]float64 {
	out := make(map[string]float64, len(names))
	for _, m := range names {
		out[m] = c.Metric(m)
	}
	return out
}

// split cuts a range at the raw boundary: the days before it read aggregates, the rest raw rows.
func (q *Queries) split(r domain.Range, loc *time.Location) (agg, raw *domain.Range) {
	from := q.rawFrom(loc)
	switch {
	case !r.From.Before(from.Time):
		return nil, &r
	case r.To.Before(from.Time):
		return &r, nil
	}
	return &domain.Range{From: r.From, To: from.AddDays(-1)}, &domain.Range{From: from, To: r.To}
}

// topCut asks a raw-only breakdown for its first rows by a metric (Change 38).
type topCut struct {
	metric string
	limit  int
}

func (q *Queries) breakdown(ctx context.Context, site domain.Site, r domain.Range, dimension string, filters []domain.Filter, top *topCut) ([]domain.Row, error) {
	loc := site.Location()
	agg, raw := q.split(r, loc)
	sums := map[string]domain.Counts{}
	var order []string
	add := func(rows []domain.Row) {
		for _, row := range rows {
			if _, seen := sums[row.Value]; !seen {
				order = append(order, row.Value)
			}
			sums[row.Value] = sums[row.Value].Add(row.Counts)
		}
	}
	if agg != nil {
		rows, err := q.repo.DailyRows(ctx, site.ID, dimension, agg.From, agg.To)
		if err != nil {
			return nil, err
		}
		add(rows)
	}
	if raw != nil {
		start, end := raw.Bounds(loc)
		var rows []domain.Row
		var err error
		if agg == nil && top != nil && dimension != "" {
			rows, err = q.repo.RawTopRows(ctx, site.ID, dimension, start, end, filters, top.metric, top.limit)
		} else {
			rows, err = q.repo.RawRows(ctx, site.ID, dimension, start, end, filters)
		}
		if err != nil {
			return nil, err
		}
		add(rows)
	}
	out := make([]domain.Row, 0, len(order))
	for _, v := range order {
		if c := sums[v]; c.Visits > 0 || c.Pageviews > 0 {
			out = append(out, domain.Row{Value: v, Counts: c})
		}
	}
	return out, nil
}

// bucket is one point of a time series: [start, end) in the site's clock, labelled by its start.
type bucket struct {
	label      string
	start, end time.Time
	counts     domain.Counts
}

// buckets lays a range out by interval. Weeks start on Monday and months on the 1st; the first and
// last buckets are clipped to the range. An hour bucket's label carries its UTC offset, so the
// repeated hour of a DST change stays distinct.
func buckets(r domain.Range, interval string, loc *time.Location) []bucket {
	start, end := r.Bounds(loc)
	var out []bucket
	if interval == "hour" {
		for t := start; t.Before(end); t = t.Add(time.Hour) {
			out = append(out, bucket{label: t.In(loc).Format(time.RFC3339), start: t, end: t.Add(time.Hour)})
		}
		return out
	}
	for d := r.From; !d.After(r.To.Time); {
		next := d.AddDays(1)
		switch interval {
		case "week":
			next = d.AddDays(7 - (int(d.Weekday())+6)%7)
		case "month":
			next = domain.Day{Time: d.AddDate(0, 1, 1-d.Day())}
		}
		if next.After(r.To.Time) {
			next = r.To.AddDays(1)
		}
		out = append(out, bucket{label: d.String(), start: d.Start(loc), end: next.Start(loc)})
		d = next
	}
	return out
}

func (q *Queries) timeline(ctx context.Context, site domain.Site, r domain.Range, interval string, filters []domain.Filter) ([]domain.Row, error) {
	loc := site.Location()
	bs := buckets(r, interval, loc)
	at := func(t time.Time) int {
		i := sort.Search(len(bs), func(i int) bool { return bs[i].end.After(t) })
		if i < len(bs) && !t.Before(bs[i].start) {
			return i
		}
		return -1
	}
	agg, raw := q.split(r, loc)
	if agg != nil {
		days, err := q.repo.DailyTotals(ctx, site.ID, agg.From, agg.To)
		if err != nil {
			return nil, err
		}
		for _, row := range days {
			day, err := domain.ParseDay(row.Value)
			if err != nil {
				continue
			}
			if i := at(day.Start(loc)); i >= 0 {
				bs[i].counts = bs[i].counts.Add(row.Counts)
			}
		}
	}
	if raw != nil {
		start, end := raw.Bounds(loc)
		visits, err := q.repo.RawVisits(ctx, site.ID, start, end, filters)
		if err != nil {
			return nil, err
		}
		// a visitor hash lives one site day, so distinct hashes per bucket add up across days
		seen := make([]map[string]bool, len(bs))
		for _, v := range visits {
			i := at(v.StartedAt)
			if i < 0 {
				continue
			}
			if seen[i] == nil {
				seen[i] = map[string]bool{}
			}
			c := &bs[i].counts
			if !seen[i][v.Hash] {
				seen[i][v.Hash] = true
				c.Visitors++
			}
			c.Visits++
			c.Pageviews += v.Pageviews
			if v.Pageviews == 1 {
				c.Bounces++
			}
			c.DurationMS += v.DurationMS
		}
	}
	out := make([]domain.Row, len(bs))
	for i, b := range bs {
		out[i] = domain.Row{Value: b.label, Counts: b.counts}
	}
	return out, nil
}

// Realtime counts the site's visitors with a pageview in the last five minutes.
func (q *Queries) Realtime(ctx context.Context, siteID string) (int, error) {
	if _, found, err := q.repo.SiteExists(ctx, siteID); err != nil {
		return 0, err
	} else if !found {
		return 0, apierror.NotFound("site not found")
	}
	return q.repo.Realtime(ctx, siteID, q.now().Add(-realtimeWindow))
}
