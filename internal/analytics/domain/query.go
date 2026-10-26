package domain

import (
	"strconv"
	"strings"
	"time"

	"github.com/avatarsik6699/smotryashchiy/internal/platform/apierror"
)

// Query is one request to the analytics query API (Change 36, docs/SPEC.md §4i): metrics over a
// range of the site's days, optionally broken down by one dimension or a time interval, filtered
// and compared with an earlier period. Everything is allow-listed; Validate rejects the rest.
type Query struct {
	Metrics   []string `json:"metrics"`
	Dimension string   `json:"dimension,omitempty"`
	Filters   []Filter `json:"filters,omitempty"`
	// DateRange is [from, to] in the site's days ("2026-10-01"), both included; Period is a preset
	// used when DateRange is empty.
	DateRange []string `json:"date_range,omitempty"`
	Period    string   `json:"period,omitempty"`
	Compare   string   `json:"compare,omitempty"`
	OrderBy   string   `json:"order_by,omitempty"`
	Limit     int      `json:"limit,omitempty"`
	Format    string   `json:"format,omitempty"`
}

// Filter keeps rows whose dimension is (or is not, or contains) one of Values.
type Filter struct {
	Dimension string   `json:"dimension"`
	Op        string   `json:"op"`
	Values    []string `json:"values"`
}

// Query vocabulary.
const (
	MetricVisitors      = "visitors"
	MetricVisits        = "visits"
	MetricPageviews     = "pageviews"
	MetricViewsPerVisit = "views_per_visit"
	MetricBounceRate    = "bounce_rate"
	MetricVisitDuration = "visit_duration"

	DimensionPage = "page"

	CompareNone     = ""
	ComparePrevious = "previous"
	CompareYearAgo  = "year_ago"

	FilterIs       = "is"
	FilterIsNot    = "is_not"
	FilterContains = "contains"

	MaxQueryLimit    = 1000
	DefaultLimit     = 100
	MaxFilterValues  = 20
	MaxFilters       = 8
	MaxValueBytes    = 2048
	MaxHistoryMonths = 25
)

var metricSet = map[string]bool{
	MetricVisitors: true, MetricVisits: true, MetricPageviews: true,
	MetricViewsPerVisit: true, MetricBounceRate: true, MetricVisitDuration: true,
}

// VisitColumns maps each visit dimension to its column in `visits` (and in the daily aggregates).
var VisitColumns = map[string]string{
	"entry_page": "entry_path", "exit_page": "exit_path",
	"channel": "channel", "source": "source",
	"utm_source": "utm_source", "utm_medium": "utm_medium", "utm_campaign": "utm_campaign",
	"utm_content": "utm_content", "utm_term": "utm_term",
	"country": "country", "region": "region",
	"browser": "browser", "browser_version": "browser_version",
	"os": "os", "os_version": "os_version", "device": "device", "screen": "screen",
	"hostname": "hostname",
}

// Dimensions lists every breakdown the daily aggregates keep: the total ("") and the page first.
func Dimensions() []string {
	out := []string{"", DimensionPage}
	for _, d := range []string{"entry_page", "exit_page", "channel", "source", "utm_source", "utm_medium",
		"utm_campaign", "utm_content", "utm_term", "country", "region", "browser", "browser_version", "os",
		"os_version", "device", "screen", "hostname"} {
		out = append(out, d)
	}
	return out
}

// TimeIntervals of a `time:` dimension.
var TimeIntervals = map[string]bool{"hour": true, "day": true, "week": true, "month": true}

// IsTimeDimension reports a `time:<interval>` dimension and its interval.
func IsTimeDimension(d string) (string, bool) {
	interval, ok := strings.CutPrefix(d, "time:")
	return interval, ok && TimeIntervals[interval]
}

// IsKnownDimension reports a breakdown or filter dimension (not a time one).
func IsKnownDimension(d string) bool {
	_, visit := VisitColumns[d]
	return d == DimensionPage || visit
}

// Validate normalizes the query (default metric, limit and order) or explains what is wrong.
func (q Query) Validate() (Query, error) {
	if len(q.Metrics) == 0 {
		q.Metrics = []string{MetricVisitors}
	}
	seen := map[string]bool{}
	for _, m := range q.Metrics {
		if !metricSet[m] {
			return Query{}, apierror.Invalid("unknown metric " + strconv.Quote(m))
		}
		if seen[m] {
			return Query{}, apierror.Invalid("metric " + strconv.Quote(m) + " is listed twice")
		}
		seen[m] = true
	}
	if q.Dimension != "" && !IsKnownDimension(q.Dimension) {
		if _, ok := IsTimeDimension(q.Dimension); !ok {
			return Query{}, apierror.Invalid("unknown dimension " + strconv.Quote(q.Dimension))
		}
	}
	if len(q.Filters) > MaxFilters {
		return Query{}, apierror.Invalid("at most " + strconv.Itoa(MaxFilters) + " filters")
	}
	for _, f := range q.Filters {
		if !IsKnownDimension(f.Dimension) {
			return Query{}, apierror.Invalid("unknown filter dimension " + strconv.Quote(f.Dimension))
		}
		if f.Op != FilterIs && f.Op != FilterIsNot && f.Op != FilterContains {
			return Query{}, apierror.Invalid("filter op must be is, is_not or contains")
		}
		if len(f.Values) == 0 || len(f.Values) > MaxFilterValues {
			return Query{}, apierror.Invalid("a filter takes 1.." + strconv.Itoa(MaxFilterValues) + " values")
		}
		for _, v := range f.Values {
			if len(v) > MaxValueBytes {
				return Query{}, apierror.Invalid("a filter value is too long")
			}
		}
	}
	switch q.Compare {
	case CompareNone, ComparePrevious, CompareYearAgo:
	default:
		return Query{}, apierror.Invalid("compare must be previous or year_ago")
	}
	if q.Limit <= 0 {
		q.Limit = DefaultLimit
	}
	if q.Limit > MaxQueryLimit {
		return Query{}, apierror.Invalid("limit must be at most " + strconv.Itoa(MaxQueryLimit))
	}
	if q.OrderBy == "" {
		q.OrderBy = q.Metrics[0]
	}
	if !seen[q.OrderBy] {
		return Query{}, apierror.Invalid("order_by must be one of the requested metrics")
	}
	if q.Format != "" && q.Format != "json" && q.Format != "csv" {
		return Query{}, apierror.Invalid("format must be json or csv")
	}
	if len(q.DateRange) != 0 && len(q.DateRange) != 2 {
		return Query{}, apierror.Invalid("date_range must be [from, to]")
	}
	return q, nil
}

// Day is a site-local calendar date.
type Day struct{ time.Time }

// ParseDay reads "2006-01-02" as a date at midnight UTC (only the calendar date matters).
func ParseDay(s string) (Day, error) {
	t, err := time.Parse(time.DateOnly, s)
	if err != nil {
		return Day{}, apierror.Invalid("dates are YYYY-MM-DD")
	}
	return Day{t}, nil
}

// DayOf is the calendar date of a moment in loc.
func DayOf(t time.Time, loc *time.Location) Day {
	y, m, d := t.In(loc).Date()
	return Day{time.Date(y, m, d, 0, 0, 0, 0, time.UTC)}
}

func (d Day) String() string    { return d.Format(time.DateOnly) }
func (d Day) AddDays(n int) Day { return Day{d.AddDate(0, 0, n)} }
func (d Day) Start(loc *time.Location) time.Time {
	y, m, dd := d.Date()
	return time.Date(y, m, dd, 0, 0, 0, 0, loc)
}

// Range is an inclusive span of site days.
type Range struct{ From, To Day }

// Days counts the days of the range.
func (r Range) Days() int { return int(r.To.Sub(r.From.Time).Hours()/24) + 1 }

// Bounds is the range as [start, end) instants in loc; a DST day is 23 or 25 hours long.
func (r Range) Bounds(loc *time.Location) (time.Time, time.Time) {
	return r.From.Start(loc), r.To.AddDays(1).Start(loc)
}

// Counts are the additive per-row sums every metric is derived from.
type Counts struct {
	Visitors   int64
	Visits     int64
	Pageviews  int64
	Bounces    int64
	DurationMS int64
}

// Add sums two counts.
func (c Counts) Add(o Counts) Counts {
	return Counts{c.Visitors + o.Visitors, c.Visits + o.Visits, c.Pageviews + o.Pageviews, c.Bounces + o.Bounces, c.DurationMS + o.DurationMS}
}

// Metric derives one requested metric. Rates and averages over no visits are 0.
func (c Counts) Metric(name string) float64 {
	switch name {
	case MetricVisitors:
		return float64(c.Visitors)
	case MetricVisits:
		return float64(c.Visits)
	case MetricPageviews:
		return float64(c.Pageviews)
	case MetricViewsPerVisit:
		if c.Visits == 0 {
			return 0
		}
		return round(float64(c.Pageviews)/float64(c.Visits), 2)
	case MetricBounceRate:
		if c.Visits == 0 {
			return 0
		}
		return round(100*float64(c.Bounces)/float64(c.Visits), 1)
	case MetricVisitDuration:
		if c.Visits == 0 {
			return 0
		}
		return float64(c.DurationMS / c.Visits / 1000)
	}
	return 0
}

func round(v float64, places int) float64 {
	p := 1.0
	for range places {
		p *= 10
	}
	if v < 0 {
		return -float64(int64(-v*p+0.5)) / p
	}
	return float64(int64(v*p+0.5)) / p
}

// Row is one breakdown value (or time bucket) with its counts.
type Row struct {
	Value  string
	Counts Counts
}

// TimelineVisit is what a raw time series buckets: one visit's start and counts.
type TimelineVisit struct {
	StartedAt  time.Time
	Hash       string
	Pageviews  int64
	DurationMS int64
}

// ResultRow is a row as the API returns it.
type ResultRow struct {
	Value   string             `json:"value"`
	Metrics map[string]float64 `json:"metrics"`
	// Compare holds the same metrics for the comparison period: the same value in a breakdown, the
	// bucket at the same position in a time series. Absent without a comparison.
	Compare map[string]float64 `json:"compare,omitempty"`
	// CompareValue labels the comparison bucket of a time series.
	CompareValue string `json:"compare_value,omitempty"`
}

// QueryResult answers a Query.
type QueryResult struct {
	DateRange    [2]string   `json:"date_range"`
	CompareRange *[2]string  `json:"compare_range,omitempty"`
	Timezone     string      `json:"timezone"`
	Source       string      `json:"source"`
	Rows         []ResultRow `json:"rows"`
}
