package application

import (
	"context"
	"log/slog"
	"time"

	"github.com/avatarsik6699/smotryashchiy/internal/analytics/domain"
)

// rollupInterval is the aggregate job's cadence: today's aggregate lags raw rows by at most this.
const rollupInterval = time.Hour

// Rollup keeps the daily aggregates (Change 36, docs/SPEC.md §4i): for every site it rebuilds
// today and yesterday in the site's timezone, backfills any day the raw rows still hold whole that
// has no aggregate yet, and drops aggregate days older than 25 months. RefreshDay replaces a day,
// so a rerun never double-counts.
func (q *Queries) Rollup(ctx context.Context) error {
	sites, err := q.repo.ListSites(ctx)
	if err != nil {
		return err
	}
	now := q.now()
	for _, site := range sites {
		if err := q.rollupSite(ctx, site, now); err != nil {
			return err
		}
	}
	before := domain.Day{Time: domain.DayOf(now, time.UTC).AddDate(0, -domain.MaxHistoryMonths, 0)}
	_, err = q.repo.PurgeDaily(ctx, before)
	return err
}

func (q *Queries) rollupSite(ctx context.Context, site domain.Site, now time.Time) error {
	loc := site.Location()
	today := domain.DayOf(now, loc)
	first, found, err := q.repo.FirstVisit(ctx, site.ID)
	if err != nil || !found {
		return err
	}
	from := domain.DayOf(first, loc)
	if raw := q.rawFrom(loc); from.Before(raw.Time) {
		from = raw
	}
	done, err := q.repo.AggregatedDays(ctx, site.ID, from, today)
	if err != nil {
		return err
	}
	for d := from; !d.After(today.Time); d = d.AddDays(1) {
		if done[d.String()] && d.Before(today.AddDays(-1).Time) {
			continue
		}
		start, end := domain.Range{From: d, To: d}.Bounds(loc)
		if err := q.repo.RefreshDay(ctx, site.ID, d, start, end); err != nil {
			return err
		}
	}
	return nil
}

// Run builds the aggregates immediately and then hourly until ctx is done; failures are logged and
// retried at the next tick, like the raw purge in Service.Run.
func (q *Queries) Run(ctx context.Context) {
	q.runOnce(ctx)
	tick := time.NewTicker(rollupInterval)
	defer tick.Stop()
	for {
		select {
		case <-ctx.Done():
			return
		case <-tick.C:
			q.runOnce(ctx)
		}
	}
}

func (q *Queries) runOnce(ctx context.Context) {
	if err := q.Rollup(ctx); err != nil && ctx.Err() == nil {
		slog.Error("analytics maintenance job failed", "job", "rollup", "err", err)
	}
}
