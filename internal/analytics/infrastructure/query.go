package infrastructure

import (
	"context"
	"database/sql"
	"errors"
	"fmt"
	"strconv"
	"strings"
	"time"

	"github.com/avatarsik6699/smotryashchiy/internal/analytics/application"
	"github.com/avatarsik6699/smotryashchiy/internal/analytics/domain"
)

var _ application.QueryRepository = (*Store)(nil)

// Change 36 (docs/SPEC.md §4i): the query API's reads. Raw rows answer ranges inside the raw
// retention, with filters; the daily aggregates answer longer ones. The aggregates are built from
// the same raw breakdowns, so both paths count alike.

// maxTimelineVisits bounds a raw time series read (it buckets visits in Go, by the site's clock).
const maxTimelineVisits = 500_000

// filterSQL renders the query's filters against visits `v` (and, for a page breakdown, its
// pageviews `p`). A page filter on a visit breakdown keeps visits that include such a page.
func filterSQL(filters []domain.Filter, pageAlias string) (string, []any) {
	var b strings.Builder
	var args []any
	for _, f := range filters {
		col := "v." + domain.VisitColumns[f.Dimension]
		if f.Dimension == domain.DimensionPage {
			if pageAlias != "" {
				col = pageAlias + ".path"
			} else {
				cond, condArgs := matchSQL("pf.path", f)
				b.WriteString(" AND EXISTS (SELECT 1 FROM pageviews pf WHERE pf.visit_id = v.id AND " + cond + ")")
				args = append(args, condArgs...)
				continue
			}
		}
		cond, condArgs := matchSQL(col, f)
		b.WriteString(" AND " + cond)
		args = append(args, condArgs...)
	}
	return b.String(), args
}

func matchSQL(col string, f domain.Filter) (string, []any) {
	args := make([]any, 0, len(f.Values))
	switch f.Op {
	case domain.FilterContains:
		parts := make([]string, 0, len(f.Values))
		for _, v := range f.Values {
			parts = append(parts, col+` LIKE ? ESCAPE '\'`)
			args = append(args, "%"+likeEscape(v)+"%")
		}
		return "(" + strings.Join(parts, " OR ") + ")", args
	default:
		marks := strings.TrimSuffix(strings.Repeat("?,", len(f.Values)), ",")
		for _, v := range f.Values {
			args = append(args, v)
		}
		if f.Op == domain.FilterIsNot {
			return col + " NOT IN (" + marks + ")", args
		}
		return col + " IN (" + marks + ")", args
	}
}

func likeEscape(s string) string {
	return strings.NewReplacer(`\`, `\\`, `%`, `\%`, `_`, `\_`).Replace(s)
}

// RawRows breaks raw visits (or, by page, their pageviews) of [from, to) down by one dimension;
// "" is the total.
func (s *Store) RawRows(ctx context.Context, siteID, dimension string, from, to time.Time, filters []domain.Filter) ([]domain.Row, error) {
	query, args := rawRowsSQL(siteID, dimension, from, to, filters)
	return s.rows(ctx, query, args...)
}

// topColumns are the RawRows columns a count metric sorts by (value is column 1).
var topColumns = map[string]int{domain.MetricVisitors: 2, domain.MetricVisits: 3, domain.MetricPageviews: 4}

// RawTopRows is RawRows cut to the first limit rows by a count metric, then value — the same order
// the query API sorts in, so the database does the cut (Change 38). Other metrics are derived and
// rounded in Go, so they keep the full read.
func (s *Store) RawTopRows(ctx context.Context, siteID, dimension string, from, to time.Time, filters []domain.Filter, metric string, limit int) ([]domain.Row, error) {
	col, ok := topColumns[metric]
	if !ok {
		return s.RawRows(ctx, siteID, dimension, from, to, filters)
	}
	query, args := rawRowsSQL(siteID, dimension, from, to, filters)
	return s.rows(ctx, `SELECT * FROM (`+query+`) ORDER BY `+strconv.Itoa(col)+` DESC, 1 LIMIT ?`, append(args, limit)...)
}

func rawRowsSQL(siteID, dimension string, from, to time.Time, filters []domain.Filter) (string, []any) {
	if dimension == domain.DimensionPage {
		where, args := filterSQL(filters, "p")
		return `SELECT x.path, COUNT(DISTINCT x.hash), COUNT(*), SUM(x.n), SUM(x.bounce), SUM(x.dur) FROM (
			SELECT p.path, p.visitor_hash AS hash, COUNT(*) AS n, MAX(v.pageviews = 1) AS bounce,
				MAX(v.last_at - v.started_at) AS dur
			FROM pageviews p JOIN visits v ON v.id = p.visit_id
			WHERE p.site_id = ? AND p.ts >= ? AND p.ts < ?` + where + `
			GROUP BY p.path, p.visit_id) x GROUP BY x.path`,
			append([]any{siteID, from.UnixMilli(), to.UnixMilli()}, args...)
	}
	value := "''"
	if dimension != "" {
		value = "v." + domain.VisitColumns[dimension]
	}
	where, args := filterSQL(filters, "")
	return `SELECT ` + value + `, COUNT(DISTINCT v.visitor_hash), COUNT(*), COALESCE(SUM(v.pageviews), 0),
		COALESCE(SUM(v.pageviews = 1), 0), COALESCE(SUM(v.last_at - v.started_at), 0)
		FROM visits v WHERE v.site_id = ? AND v.started_at >= ? AND v.started_at < ?` + where + `
		GROUP BY 1 HAVING COUNT(*) > 0`,
		append([]any{siteID, from.UnixMilli(), to.UnixMilli()}, args...)
}

// DailyRows sums the aggregates of [from, to] by one dimension's values.
func (s *Store) DailyRows(ctx context.Context, siteID, dimension string, from, to domain.Day) ([]domain.Row, error) {
	return s.rows(ctx, `SELECT value, SUM(visitors), SUM(visits), SUM(pageviews), SUM(bounces), SUM(duration_ms)
		FROM analytics_daily WHERE site_id = ? AND dimension = ? AND day >= ? AND day <= ? GROUP BY value`,
		siteID, dimension, from.String(), to.String())
}

// DailyTotals is each aggregated day's total, keyed by the day.
func (s *Store) DailyTotals(ctx context.Context, siteID string, from, to domain.Day) ([]domain.Row, error) {
	return s.rows(ctx, `SELECT day, visitors, visits, pageviews, bounces, duration_ms FROM analytics_daily
		WHERE site_id = ? AND dimension = '' AND day >= ? AND day <= ? ORDER BY day`,
		siteID, from.String(), to.String())
}

func (s *Store) rows(ctx context.Context, query string, args ...any) ([]domain.Row, error) {
	rows, err := s.db.QueryContext(ctx, query, args...)
	if err != nil {
		return nil, fmt.Errorf("analytics: query: %w", err)
	}
	defer rows.Close()
	out := []domain.Row{}
	for rows.Next() {
		var r domain.Row
		var value sql.NullString
		if err := rows.Scan(&value, &r.Counts.Visitors, &r.Counts.Visits, &r.Counts.Pageviews, &r.Counts.Bounces, &r.Counts.DurationMS); err != nil {
			return nil, fmt.Errorf("analytics: scan row: %w", err)
		}
		r.Value = value.String
		out = append(out, r)
	}
	return out, rows.Err()
}

// RawVisits lists the filtered visits started in [from, to) for a time series.
func (s *Store) RawVisits(ctx context.Context, siteID string, from, to time.Time, filters []domain.Filter) ([]domain.TimelineVisit, error) {
	where, args := filterSQL(filters, "")
	rows, err := s.db.QueryContext(ctx, `SELECT v.started_at, v.visitor_hash, v.pageviews, v.last_at - v.started_at
		FROM visits v WHERE v.site_id = ? AND v.started_at >= ? AND v.started_at < ?`+where+` LIMIT ?`,
		append(append([]any{siteID, from.UnixMilli(), to.UnixMilli()}, args...), maxTimelineVisits+1)...)
	if err != nil {
		return nil, fmt.Errorf("analytics: timeline: %w", err)
	}
	defer rows.Close()
	var out []domain.TimelineVisit
	for rows.Next() {
		var started int64
		var v domain.TimelineVisit
		if err := rows.Scan(&started, &v.Hash, &v.Pageviews, &v.DurationMS); err != nil {
			return nil, fmt.Errorf("analytics: scan timeline: %w", err)
		}
		v.StartedAt = time.UnixMilli(started)
		out = append(out, v)
	}
	if len(out) > maxTimelineVisits {
		return nil, errors.New("analytics: too many visits for a raw time series; narrow the range")
	}
	return out, rows.Err()
}

// RefreshDay rebuilds one site day of the aggregates from raw rows: every dimension's breakdown of
// [start, end), replacing what the day had. Idempotent. A day without visits still stores a zero
// total, so the backfill knows it is done.
func (s *Store) RefreshDay(ctx context.Context, siteID string, day domain.Day, start, end time.Time) error {
	type dimRows struct {
		dim  string
		rows []domain.Row
	}
	var all []dimRows
	for _, dim := range domain.Dimensions() {
		rows, err := s.RawRows(ctx, siteID, dim, start, end, nil)
		if err != nil {
			return err
		}
		if dim == "" && len(rows) == 0 {
			rows = []domain.Row{{}}
		}
		all = append(all, dimRows{dim, rows})
	}
	tx, err := s.db.BeginTx(ctx, nil)
	if err != nil {
		return fmt.Errorf("analytics: begin: %w", err)
	}
	defer func() { _ = tx.Rollback() }()
	if _, err := tx.ExecContext(ctx, `DELETE FROM analytics_daily WHERE site_id = ? AND day = ?`, siteID, day.String()); err != nil {
		return fmt.Errorf("analytics: clear day: %w", err)
	}
	for _, d := range all {
		for _, r := range d.rows {
			if _, err := tx.ExecContext(ctx, `INSERT INTO analytics_daily (site_id, day, dimension, value, visitors,
				visits, pageviews, bounces, duration_ms) VALUES (?, ?, ?, ?, ?, ?, ?, ?, ?)`,
				siteID, day.String(), d.dim, r.Value, r.Counts.Visitors, r.Counts.Visits, r.Counts.Pageviews,
				r.Counts.Bounces, r.Counts.DurationMS); err != nil {
				return fmt.Errorf("analytics: store day: %w", err)
			}
		}
	}
	if err := tx.Commit(); err != nil {
		return fmt.Errorf("analytics: commit day: %w", err)
	}
	return nil
}

// AggregatedDays lists the site's days that already have aggregates in [from, to].
func (s *Store) AggregatedDays(ctx context.Context, siteID string, from, to domain.Day) (map[string]bool, error) {
	rows, err := s.db.QueryContext(ctx, `SELECT DISTINCT day FROM analytics_daily WHERE site_id = ? AND day >= ? AND day <= ?`,
		siteID, from.String(), to.String())
	if err != nil {
		return nil, fmt.Errorf("analytics: aggregated days: %w", err)
	}
	defer rows.Close()
	out := map[string]bool{}
	for rows.Next() {
		var d string
		if err := rows.Scan(&d); err != nil {
			return nil, err
		}
		out[d] = true
	}
	return out, rows.Err()
}

// FirstVisit is the start of the site's earliest stored visit; false when it has none.
func (s *Store) FirstVisit(ctx context.Context, siteID string) (time.Time, bool, error) {
	var ms sql.NullInt64
	if err := s.db.QueryRowContext(ctx, `SELECT MIN(started_at) FROM visits WHERE site_id = ?`, siteID).Scan(&ms); err != nil {
		return time.Time{}, false, fmt.Errorf("analytics: first visit: %w", err)
	}
	return time.UnixMilli(ms.Int64), ms.Valid, nil
}

// PurgeDaily deletes aggregate days before the given day (all sites; day strings sort by date).
func (s *Store) PurgeDaily(ctx context.Context, before domain.Day) (int, error) {
	res, err := s.db.ExecContext(ctx, `DELETE FROM analytics_daily WHERE day < ?`, before.String())
	if err != nil {
		return 0, fmt.Errorf("analytics: purge daily: %w", err)
	}
	n, _ := res.RowsAffected()
	return int(n), nil
}

// Realtime counts distinct visitors with a pageview since the given moment.
func (s *Store) Realtime(ctx context.Context, siteID string, since time.Time) (int, error) {
	var n int
	err := s.db.QueryRowContext(ctx, `SELECT COUNT(DISTINCT visitor_hash) FROM pageviews WHERE site_id = ? AND ts >= ?`,
		siteID, since.UnixMilli()).Scan(&n)
	if err != nil {
		return 0, fmt.Errorf("analytics: realtime: %w", err)
	}
	return n, nil
}
