package application

import (
	"context"
	"log/slog"
	"math/rand/v2"
	"sync"
	"time"

	"github.com/avatarsik6699/smotryashchiy/internal/uptime/domain"
)

// Scheduling constants (docs/SPEC.md §4e).
const (
	maxConcurrentChecks = 8
	firstRunJitterMax   = 10 * time.Second
	resyncInterval      = time.Minute
	purgeInterval       = time.Hour
	purgeBatch          = 5000
	historyWindow       = time.Hour
)

// Repository is the persistence port.
type Repository interface {
	Create(ctx context.Context, n domain.NewTarget, now time.Time) (domain.Target, error)
	List(ctx context.Context) ([]domain.Target, error)
	Delete(ctx context.Context, id string) error
	InsertResult(ctx context.Context, r domain.Result) error
	Latest(ctx context.Context) (map[string]domain.Result, error)
	History(ctx context.Context, since time.Time) (map[string][]domain.LatencyPoint, error)
	PurgeResults(ctx context.Context, before time.Time, limit int) (int, error)
}

// ResultPublisher receives every stored result (the live stream).
type ResultPublisher interface{ PublishResult(domain.Result) }

// Options tune the scheduler; the zero value is production behavior.
type Options struct {
	// IntervalFor overrides a target's check interval (tests).
	IntervalFor func(domain.Target) time.Duration
	// FirstRunDelay overrides the jittered first-run delay (tests).
	FirstRunDelay func(domain.Target) time.Duration
	// Retention is how long results are kept; 0 disables purging.
	Retention time.Duration
	Log       *slog.Logger
}

// TargetView is a target with its newest result and the last hour of latency.
type TargetView struct {
	domain.Target
	Last    *domain.Result        `json:"last"`
	Latency []domain.LatencyPoint `json:"latency"`
}

// Service manages targets and runs their checks.
type Service struct {
	repo    Repository
	checker *Checker
	pub     ResultPublisher
	now     func() time.Time
	opts    Options
	log     *slog.Logger
	sync    chan struct{}
}

// NewService wires the service. pub may be nil.
func NewService(repo Repository, checker *Checker, pub ResultPublisher, now func() time.Time, opts Options) *Service {
	log := opts.Log
	if log == nil {
		log = slog.Default()
	}
	return &Service{repo: repo, checker: checker, pub: pub, now: now, opts: opts, log: log, sync: make(chan struct{}, 1)}
}

// Create validates and stores a target; probing starts immediately.
func (s *Service) Create(ctx context.Context, n domain.NewTarget) (domain.Target, error) {
	valid, err := n.Validate()
	if err != nil {
		return domain.Target{}, err
	}
	t, err := s.repo.Create(ctx, valid, s.now())
	if err != nil {
		return domain.Target{}, err
	}
	s.Sync()
	return t, nil
}

// Delete removes a target and its results; probing stops immediately.
func (s *Service) Delete(ctx context.Context, id string) error {
	if err := s.repo.Delete(ctx, id); err != nil {
		return err
	}
	s.Sync()
	return nil
}

// Targets lists targets with their newest result and last-hour latency.
func (s *Service) Targets(ctx context.Context) ([]TargetView, error) {
	targets, err := s.repo.List(ctx)
	if err != nil {
		return nil, err
	}
	latest, err := s.repo.Latest(ctx)
	if err != nil {
		return nil, err
	}
	history, err := s.repo.History(ctx, s.now().Add(-historyWindow))
	if err != nil {
		return nil, err
	}
	views := make([]TargetView, 0, len(targets))
	for _, t := range targets {
		v := TargetView{Target: t, Latency: history[t.ID]}
		if v.Latency == nil {
			v.Latency = []domain.LatencyPoint{}
		}
		if r, ok := latest[t.ID]; ok {
			r := r
			v.Last = &r
		}
		views = append(views, v)
	}
	return views, nil
}

// Sync asks the scheduler to reconcile with the stored targets now (non-blocking).
func (s *Service) Sync() {
	select {
	case s.sync <- struct{}{}:
	default:
	}
}

// Run schedules checks until ctx is done: one worker per target, at most maxConcurrentChecks probes in
// flight, reconciled on Sync and every minute, results purged by retention.
func (s *Service) Run(ctx context.Context) {
	workers := map[string]context.CancelFunc{}
	var wg sync.WaitGroup
	slots := make(chan struct{}, maxConcurrentChecks)

	reconcile := func() {
		targets, err := s.repo.List(ctx)
		if err != nil {
			if ctx.Err() == nil {
				s.log.Error("uptime: could not list targets", "err", err)
			}
			return
		}
		present := make(map[string]bool, len(targets))
		for _, t := range targets {
			present[t.ID] = true
			if _, running := workers[t.ID]; running {
				continue
			}
			wctx, cancel := context.WithCancel(ctx)
			workers[t.ID] = cancel
			wg.Add(1)
			go func(t domain.Target) {
				defer wg.Done()
				s.worker(wctx, t, slots)
			}(t)
		}
		for id, cancel := range workers {
			if !present[id] {
				cancel()
				delete(workers, id)
			}
		}
	}

	reconcile()
	s.purge(ctx)
	resync := time.NewTicker(resyncInterval)
	purge := time.NewTicker(purgeInterval)
	defer resync.Stop()
	defer purge.Stop()
	for {
		select {
		case <-ctx.Done():
			for _, cancel := range workers {
				cancel()
			}
			wg.Wait()
			return
		case <-s.sync:
			reconcile()
		case <-resync.C:
			reconcile()
		case <-purge.C:
			s.purge(ctx)
		}
	}
}

func (s *Service) interval(t domain.Target) time.Duration {
	if s.opts.IntervalFor != nil {
		return s.opts.IntervalFor(t)
	}
	return time.Duration(t.IntervalSeconds) * time.Second
}

func (s *Service) firstDelay(t domain.Target) time.Duration {
	if s.opts.FirstRunDelay != nil {
		return s.opts.FirstRunDelay(t)
	}
	limit := min(s.interval(t), firstRunJitterMax)
	return time.Duration(rand.Int64N(int64(limit)))
}

func (s *Service) worker(ctx context.Context, t domain.Target, slots chan struct{}) {
	if !sleep(ctx, s.firstDelay(t)) {
		return
	}
	for {
		select {
		case slots <- struct{}{}:
		case <-ctx.Done():
			return
		}
		res := s.checker.Check(ctx, t)
		<-slots
		if ctx.Err() != nil {
			return // stopped or deleted while probing: the result is moot
		}
		if err := s.repo.InsertResult(ctx, res); err != nil {
			s.log.Warn("uptime: could not store a result", "target", t.Name, "err", err)
		} else if s.pub != nil {
			s.pub.PublishResult(res)
		}
		if !sleep(ctx, s.interval(t)) {
			return
		}
	}
}

// sleep waits d or until ctx is done; it reports whether the full wait elapsed.
func sleep(ctx context.Context, d time.Duration) bool {
	if d <= 0 {
		return ctx.Err() == nil
	}
	timer := time.NewTimer(d)
	defer timer.Stop()
	select {
	case <-ctx.Done():
		return false
	case <-timer.C:
		return true
	}
}

func (s *Service) purge(ctx context.Context) {
	if s.opts.Retention <= 0 {
		return
	}
	before := s.now().Add(-s.opts.Retention)
	for ctx.Err() == nil {
		n, err := s.repo.PurgeResults(ctx, before, purgeBatch)
		if err != nil {
			s.log.Error("uptime: purge failed", "err", err)
			return
		}
		if n == 0 {
			return
		}
	}
}
