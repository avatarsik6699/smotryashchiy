// Package application holds the operator console's use-cases (docs/SPEC.md §4j): queueing a
// command, the host claiming and answering it, incidents and the append-only audit log.
package application

import (
	"context"
	"encoding/json"
	"fmt"
	"log/slog"
	"time"

	"github.com/avatarsik6699/smotryashchiy/internal/operator/domain"
)

// Repository persists commands, incidents and audit entries.
type Repository interface {
	InsertCommand(ctx context.Context, c domain.Command) error
	Claim(ctx context.Context, hostID string, now time.Time, limit int) ([]domain.Claimed, error)
	Complete(ctx context.Context, hostID, id string, r domain.Report, now time.Time) (domain.Command, error)
	Command(ctx context.Context, id string) (domain.Command, error)
	Commands(ctx context.Context, limit int) ([]domain.Command, error)
	Expire(ctx context.Context, now time.Time) (int, error)
	ClearResults(ctx context.Context, before time.Time) (int, error)

	InsertIncident(ctx context.Context, in domain.IncidentInput, id string, now time.Time) (domain.Incident, error)
	UpdateIncident(ctx context.Context, id string, in domain.IncidentInput, now time.Time) (domain.Incident, error)
	DeleteIncident(ctx context.Context, id string) error
	Incidents(ctx context.Context) ([]domain.Incident, error)

	AppendAudit(ctx context.Context, e domain.AuditEntry) error
	Audit(ctx context.Context, before int64, limit int) ([]domain.AuditEntry, error)
	PurgeAudit(ctx context.Context, before time.Time) (int, error)
}

// Service implements the console. IDs come from newID so tests stay deterministic.
type Service struct {
	repo  Repository
	now   func() time.Time
	newID func() (string, error)
}

// NewService wires a Service.
func NewService(repo Repository, now func() time.Time, newID func() (string, error)) *Service {
	return &Service{repo: repo, now: now, newID: newID}
}

// Queue validates and stores one command for a host and records who asked for it. The step-up
// password, when the kind needs one, is checked by the caller before Queue runs.
func (s *Service) Queue(ctx context.Context, n domain.NewCommand, client string) (domain.Command, error) {
	args, err := n.Validate()
	if err != nil {
		return domain.Command{}, err
	}
	id, err := s.newID()
	if err != nil {
		return domain.Command{}, fmt.Errorf("operator: command id: %w", err)
	}
	c := domain.Command{
		ID: id, HostID: n.HostID, Kind: n.Kind, Args: args, Status: domain.StatusQueued,
		CreatedAt: s.now().UTC().Truncate(time.Millisecond),
	}
	if n.Kind.NeedsReason() {
		c.Reason = n.Reason
	}
	if err := s.repo.InsertCommand(ctx, c); err != nil {
		return domain.Command{}, err
	}
	s.audit(ctx, "command.queue", domain.Target(args), map[string]any{"id": id, "host_id": n.HostID, "kind": n.Kind, "args": args}, client, string(domain.StatusQueued))
	return c, nil
}

// Refused records a queue attempt the step-up check turned away.
func (s *Service) Refused(ctx context.Context, n domain.NewCommand, client, why string) {
	s.audit(ctx, "command.refused", domain.Target(n.Args), map[string]any{"host_id": n.HostID, "kind": n.Kind}, client, why)
}

// Claim hands the host up to ClaimBatch of its queued commands and marks them running.
func (s *Service) Claim(ctx context.Context, hostID string) ([]domain.Claimed, error) {
	now := s.now()
	if _, err := s.repo.Expire(ctx, now); err != nil {
		return nil, err
	}
	return s.repo.Claim(ctx, hostID, now, domain.ClaimBatch)
}

// Complete stores the host's answer to one of its running commands.
func (s *Service) Complete(ctx context.Context, hostID, id string, r domain.Report) (domain.Command, error) {
	r, err := r.Validate()
	if err != nil {
		return domain.Command{}, err
	}
	c, err := s.repo.Complete(ctx, hostID, id, r, s.now())
	if err != nil {
		return domain.Command{}, err
	}
	s.audit(ctx, "command.result", domain.Target(c.Args), map[string]any{"id": id, "kind": c.Kind, "error": c.Error}, "host:"+hostID, string(c.Status))
	return c, nil
}

// Command returns one command, expiring stale ones first so the UI never waits forever.
func (s *Service) Command(ctx context.Context, id string) (domain.Command, error) {
	if _, err := s.repo.Expire(ctx, s.now()); err != nil {
		return domain.Command{}, err
	}
	return s.repo.Command(ctx, id)
}

// Commands returns the newest commands.
func (s *Service) Commands(ctx context.Context, limit int) ([]domain.Command, error) {
	if _, err := s.repo.Expire(ctx, s.now()); err != nil {
		return nil, err
	}
	return s.repo.Commands(ctx, clampLimit(limit))
}

// Incidents lists every incident, newest detection first.
func (s *Service) Incidents(ctx context.Context) ([]domain.Incident, error) {
	return s.repo.Incidents(ctx)
}

// CreateIncident validates and stores an incident.
func (s *Service) CreateIncident(ctx context.Context, in domain.IncidentInput, client string) (domain.Incident, error) {
	now := s.now()
	in, err := in.Validate(now)
	if err != nil {
		return domain.Incident{}, err
	}
	id, err := s.newID()
	if err != nil {
		return domain.Incident{}, fmt.Errorf("operator: incident id: %w", err)
	}
	inc, err := s.repo.InsertIncident(ctx, in, id, now)
	if err != nil {
		return domain.Incident{}, err
	}
	s.audit(ctx, "incident.create", "incident:"+id, map[string]any{"title": in.Title, "kind": in.Kind}, client, "created")
	return inc, nil
}

// UpdateIncident replaces an incident's editable fields.
func (s *Service) UpdateIncident(ctx context.Context, id string, in domain.IncidentInput, client string) (domain.Incident, error) {
	now := s.now()
	in, err := in.Validate(now)
	if err != nil {
		return domain.Incident{}, err
	}
	inc, err := s.repo.UpdateIncident(ctx, id, in, now)
	if err != nil {
		return domain.Incident{}, err
	}
	s.audit(ctx, "incident.update", "incident:"+id, map[string]any{"title": in.Title, "kind": in.Kind, "closed": in.ClosedAt != nil}, client, "updated")
	return inc, nil
}

// DeleteIncident removes an incident; the audit log keeps that it existed.
func (s *Service) DeleteIncident(ctx context.Context, id, client string) error {
	if err := s.repo.DeleteIncident(ctx, id); err != nil {
		return err
	}
	s.audit(ctx, "incident.delete", "incident:"+id, map[string]any{}, client, "deleted")
	return nil
}

// Audit pages the log newest first; before is an entry id (0 = from the newest).
func (s *Service) Audit(ctx context.Context, before int64, limit int) ([]domain.AuditEntry, error) {
	return s.repo.Audit(ctx, before, clampLimit(limit))
}

// Run expires stale commands every minute and, daily, clears old results and audit rows.
func (s *Service) Run(ctx context.Context) {
	s.maintain(ctx)
	minute := time.NewTicker(time.Minute)
	day := time.NewTicker(24 * time.Hour)
	defer minute.Stop()
	defer day.Stop()
	for {
		select {
		case <-ctx.Done():
			return
		case <-minute.C:
			if _, err := s.repo.Expire(ctx, s.now()); err != nil && ctx.Err() == nil {
				slog.Error("operator: expire commands", "err", err)
			}
		case <-day.C:
			s.maintain(ctx)
		}
	}
}

// Maintain runs the retention jobs once (exported for tests).
func (s *Service) Maintain(ctx context.Context) { s.maintain(ctx) }

func (s *Service) maintain(ctx context.Context) {
	now := s.now()
	if _, err := s.repo.Expire(ctx, now); err != nil && ctx.Err() == nil {
		slog.Error("operator: expire commands", "err", err)
	}
	if _, err := s.repo.ClearResults(ctx, now.Add(-domain.ResultRetention)); err != nil && ctx.Err() == nil {
		slog.Error("operator: clear old results", "err", err)
	}
	if _, err := s.repo.PurgeAudit(ctx, now.Add(-domain.AuditRetention)); err != nil && ctx.Err() == nil {
		slog.Error("operator: purge old audit rows", "err", err)
	}
}

// audit appends one entry; a failure is logged and never undoes the action it describes.
func (s *Service) audit(ctx context.Context, action, target string, detail map[string]any, client, outcome string) {
	raw, err := json.Marshal(detail)
	if err != nil {
		raw = []byte(`{}`)
	}
	entry := domain.AuditEntry{At: s.now().UTC(), Action: action, Target: target, Detail: raw, Client: client, Outcome: outcome}
	if err := s.repo.AppendAudit(ctx, entry); err != nil {
		slog.Error("operator: append audit entry", "action", action, "err", err)
	}
}

func clampLimit(limit int) int {
	if limit <= 0 {
		return domain.DefaultListLimit
	}
	if limit > domain.MaxListLimit {
		return domain.MaxListLimit
	}
	return limit
}
