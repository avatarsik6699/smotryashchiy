// Package infrastructure persists the operator console in SQLite (docs/SPEC.md §4j). Times are
// unix milliseconds in UTC.
package infrastructure

import (
	"context"
	"crypto/rand"
	"database/sql"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"strings"
	"time"

	"github.com/avatarsik6699/smotryashchiy/internal/operator/application"
	"github.com/avatarsik6699/smotryashchiy/internal/operator/domain"
	"github.com/avatarsik6699/smotryashchiy/internal/platform/apierror"
)

// NewID returns a random 24-hex-digit identifier for a command or an incident.
func NewID() (string, error) {
	buf := make([]byte, 12)
	if _, err := rand.Read(buf); err != nil {
		return "", err
	}
	return hex.EncodeToString(buf), nil
}

// Store implements application.Repository.
type Store struct{ db *sql.DB }

// NewStore returns a Store over db (migrations must have run).
func NewStore(db *sql.DB) *Store { return &Store{db: db} }

var _ application.Repository = (*Store)(nil)

const commandColumns = `id, host_id, kind, args_json, reason, status, created_at, started_at, finished_at, result_json, error`

// InsertCommand stores a queued command; an unknown host is a field error, not a 500.
func (s *Store) InsertCommand(ctx context.Context, c domain.Command) error {
	_, err := s.db.ExecContext(ctx,
		`INSERT INTO operator_commands (id, host_id, kind, args_json, reason, status, created_at) VALUES (?, ?, ?, ?, ?, ?, ?)`,
		c.ID, c.HostID, string(c.Kind), string(c.Args), nullString(c.Reason), string(c.Status), c.CreatedAt.UnixMilli())
	if err != nil {
		if strings.Contains(err.Error(), "FOREIGN KEY") {
			return apierror.Invalid("host_id is not an enrolled host")
		}
		return fmt.Errorf("operator: insert command: %w", err)
	}
	return nil
}

// Claim marks up to limit of the host's queued commands running, oldest first, in one transaction,
// so two polls can never hand out the same command.
func (s *Store) Claim(ctx context.Context, hostID string, now time.Time, limit int) ([]domain.Claimed, error) {
	tx, err := s.db.BeginTx(ctx, nil)
	if err != nil {
		return nil, fmt.Errorf("operator: begin claim: %w", err)
	}
	defer func() { _ = tx.Rollback() }()
	rows, err := tx.QueryContext(ctx,
		`SELECT id, kind, args_json FROM operator_commands WHERE host_id = ? AND status = 'queued' ORDER BY created_at, id LIMIT ?`,
		hostID, limit)
	if err != nil {
		return nil, fmt.Errorf("operator: select queued: %w", err)
	}
	claimed := []domain.Claimed{}
	for rows.Next() {
		var (
			c    domain.Claimed
			kind string
			args string
		)
		if err := rows.Scan(&c.ID, &kind, &args); err != nil {
			rows.Close()
			return nil, fmt.Errorf("operator: scan queued: %w", err)
		}
		c.Kind, c.Args = domain.Kind(kind), json.RawMessage(args)
		claimed = append(claimed, c)
	}
	rows.Close()
	if err := rows.Err(); err != nil {
		return nil, fmt.Errorf("operator: queued rows: %w", err)
	}
	for _, c := range claimed {
		if _, err := tx.ExecContext(ctx,
			`UPDATE operator_commands SET status = 'running', started_at = ? WHERE id = ? AND status = 'queued'`,
			now.UnixMilli(), c.ID); err != nil {
			return nil, fmt.Errorf("operator: mark running: %w", err)
		}
	}
	if err := tx.Commit(); err != nil {
		return nil, fmt.Errorf("operator: commit claim: %w", err)
	}
	return claimed, nil
}

// Complete closes a running command of the same host; anything else is not found.
func (s *Store) Complete(ctx context.Context, hostID, id string, r domain.Report, now time.Time) (domain.Command, error) {
	var result any
	if r.Result != nil {
		result = string(r.Result)
	}
	res, err := s.db.ExecContext(ctx,
		`UPDATE operator_commands SET status = ?, finished_at = ?, result_json = ?, error = ?
		 WHERE id = ? AND host_id = ? AND status = 'running'`,
		string(r.Status), now.UnixMilli(), result, nullString(r.Error), id, hostID)
	if err != nil {
		return domain.Command{}, fmt.Errorf("operator: complete command: %w", err)
	}
	if n, _ := res.RowsAffected(); n == 0 {
		return domain.Command{}, apierror.NotFound("no running command with this id for this host")
	}
	return s.Command(ctx, id)
}

// Command returns one command.
func (s *Store) Command(ctx context.Context, id string) (domain.Command, error) {
	row := s.db.QueryRowContext(ctx, `SELECT `+commandColumns+` FROM operator_commands WHERE id = ?`, id)
	c, err := scanCommand(row)
	if errors.Is(err, sql.ErrNoRows) {
		return domain.Command{}, apierror.NotFound("command not found")
	}
	return c, err
}

// Commands returns the newest commands first.
func (s *Store) Commands(ctx context.Context, limit int) ([]domain.Command, error) {
	rows, err := s.db.QueryContext(ctx, `SELECT `+commandColumns+` FROM operator_commands ORDER BY created_at DESC, id DESC LIMIT ?`, limit)
	if err != nil {
		return nil, fmt.Errorf("operator: list commands: %w", err)
	}
	defer rows.Close()
	out := []domain.Command{}
	for rows.Next() {
		c, err := scanCommand(rows)
		if err != nil {
			return nil, err
		}
		out = append(out, c)
	}
	return out, rows.Err()
}

// Expire ends commands nobody claimed within QueuedExpiry and those running without an answer for
// RunningExpiry.
func (s *Store) Expire(ctx context.Context, now time.Time) (int, error) {
	queued, err := s.db.ExecContext(ctx,
		`UPDATE operator_commands SET status = 'expired', finished_at = ? WHERE status = 'queued' AND created_at < ?`,
		now.UnixMilli(), now.Add(-domain.QueuedExpiry).UnixMilli())
	if err != nil {
		return 0, fmt.Errorf("operator: expire queued: %w", err)
	}
	running, err := s.db.ExecContext(ctx,
		`UPDATE operator_commands SET status = 'failed', finished_at = ?, error = 'no result' WHERE status = 'running' AND started_at < ?`,
		now.UnixMilli(), now.Add(-domain.RunningExpiry).UnixMilli())
	if err != nil {
		return 0, fmt.Errorf("operator: fail stale running: %w", err)
	}
	a, _ := queued.RowsAffected()
	b, _ := running.RowsAffected()
	return int(a + b), nil
}

// ClearResults drops the answers (they may name people) of commands finished before the cutoff;
// the rows stay as history.
func (s *Store) ClearResults(ctx context.Context, before time.Time) (int, error) {
	res, err := s.db.ExecContext(ctx,
		`UPDATE operator_commands SET result_json = NULL, error = NULL
		 WHERE finished_at < ? AND (result_json IS NOT NULL OR error IS NOT NULL)`, before.UnixMilli())
	if err != nil {
		return 0, fmt.Errorf("operator: clear results: %w", err)
	}
	n, _ := res.RowsAffected()
	return int(n), nil
}

type scanner interface{ Scan(dest ...any) error }

func scanCommand(row scanner) (domain.Command, error) {
	var (
		c                  domain.Command
		kind, args, status string
		reason, result, e  sql.NullString
		created            int64
		started, finished  sql.NullInt64
	)
	if err := row.Scan(&c.ID, &c.HostID, &kind, &args, &reason, &status, &created, &started, &finished, &result, &e); err != nil {
		if errors.Is(err, sql.ErrNoRows) {
			return domain.Command{}, err
		}
		return domain.Command{}, fmt.Errorf("operator: scan command: %w", err)
	}
	c.Kind, c.Args, c.Status = domain.Kind(kind), json.RawMessage(args), domain.Status(status)
	c.Reason, c.Error = reason.String, e.String
	c.CreatedAt = time.UnixMilli(created).UTC()
	c.StartedAt, c.FinishedAt = millis(started), millis(finished)
	if result.Valid {
		c.Result = json.RawMessage(result.String)
	}
	return c, nil
}

const incidentColumns = `id, title, kind, detected_at, scope, people_affected, measures, rkn_primary_at, rkn_final_at, users_notified_at, notes, closed_at, created_at, updated_at`

// InsertIncident stores a validated incident.
func (s *Store) InsertIncident(ctx context.Context, in domain.IncidentInput, id string, now time.Time) (domain.Incident, error) {
	_, err := s.db.ExecContext(ctx,
		`INSERT INTO incidents (`+incidentColumns+`) VALUES (?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?)`,
		id, in.Title, string(in.Kind), in.DetectedAt.UnixMilli(), in.Scope, in.PeopleAffected, in.Measures,
		unix(in.RKNPrimaryAt), unix(in.RKNFinalAt), unix(in.UsersNotifiedAt), in.Notes, unix(in.ClosedAt),
		now.UnixMilli(), now.UnixMilli())
	if err != nil {
		return domain.Incident{}, fmt.Errorf("operator: insert incident: %w", err)
	}
	return s.incident(ctx, id)
}

// UpdateIncident replaces the editable fields of an incident.
func (s *Store) UpdateIncident(ctx context.Context, id string, in domain.IncidentInput, now time.Time) (domain.Incident, error) {
	res, err := s.db.ExecContext(ctx,
		`UPDATE incidents SET title = ?, kind = ?, detected_at = ?, scope = ?, people_affected = ?, measures = ?,
		 rkn_primary_at = ?, rkn_final_at = ?, users_notified_at = ?, notes = ?, closed_at = ?, updated_at = ? WHERE id = ?`,
		in.Title, string(in.Kind), in.DetectedAt.UnixMilli(), in.Scope, in.PeopleAffected, in.Measures,
		unix(in.RKNPrimaryAt), unix(in.RKNFinalAt), unix(in.UsersNotifiedAt), in.Notes, unix(in.ClosedAt),
		now.UnixMilli(), id)
	if err != nil {
		return domain.Incident{}, fmt.Errorf("operator: update incident: %w", err)
	}
	if n, _ := res.RowsAffected(); n == 0 {
		return domain.Incident{}, apierror.NotFound("incident not found")
	}
	return s.incident(ctx, id)
}

// DeleteIncident removes an incident.
func (s *Store) DeleteIncident(ctx context.Context, id string) error {
	res, err := s.db.ExecContext(ctx, `DELETE FROM incidents WHERE id = ?`, id)
	if err != nil {
		return fmt.Errorf("operator: delete incident: %w", err)
	}
	if n, _ := res.RowsAffected(); n == 0 {
		return apierror.NotFound("incident not found")
	}
	return nil
}

// Incidents lists every incident, newest detection first.
func (s *Store) Incidents(ctx context.Context) ([]domain.Incident, error) {
	rows, err := s.db.QueryContext(ctx, `SELECT `+incidentColumns+` FROM incidents ORDER BY detected_at DESC, id`)
	if err != nil {
		return nil, fmt.Errorf("operator: list incidents: %w", err)
	}
	defer rows.Close()
	out := []domain.Incident{}
	for rows.Next() {
		inc, err := scanIncident(rows)
		if err != nil {
			return nil, err
		}
		out = append(out, inc)
	}
	return out, rows.Err()
}

func (s *Store) incident(ctx context.Context, id string) (domain.Incident, error) {
	inc, err := scanIncident(s.db.QueryRowContext(ctx, `SELECT `+incidentColumns+` FROM incidents WHERE id = ?`, id))
	if errors.Is(err, sql.ErrNoRows) {
		return domain.Incident{}, apierror.NotFound("incident not found")
	}
	return inc, err
}

func scanIncident(row scanner) (domain.Incident, error) {
	var (
		inc                                    domain.Incident
		kind                                   string
		detected, created, updated             int64
		people                                 sql.NullInt64
		primary, final, notified, closedMillis sql.NullInt64
	)
	if err := row.Scan(&inc.ID, &inc.Title, &kind, &detected, &inc.Scope, &people, &inc.Measures,
		&primary, &final, &notified, &inc.Notes, &closedMillis, &created, &updated); err != nil {
		if errors.Is(err, sql.ErrNoRows) {
			return domain.Incident{}, err
		}
		return domain.Incident{}, fmt.Errorf("operator: scan incident: %w", err)
	}
	inc.Kind = domain.IncidentKind(kind)
	inc.DetectedAt = time.UnixMilli(detected).UTC()
	if people.Valid {
		n := int(people.Int64)
		inc.PeopleAffected = &n
	}
	inc.RKNPrimaryAt, inc.RKNFinalAt, inc.UsersNotifiedAt, inc.ClosedAt = millis(primary), millis(final), millis(notified), millis(closedMillis)
	inc.CreatedAt, inc.UpdatedAt = time.UnixMilli(created).UTC(), time.UnixMilli(updated).UTC()
	return inc, nil
}

// AppendAudit adds one entry; the table refuses updates and early deletes (triggers).
func (s *Store) AppendAudit(ctx context.Context, e domain.AuditEntry) error {
	detail := e.Detail
	if len(detail) == 0 {
		detail = json.RawMessage(`{}`)
	}
	_, err := s.db.ExecContext(ctx,
		`INSERT INTO audit_log (at, action, target, detail_json, client, outcome) VALUES (?, ?, ?, ?, ?, ?)`,
		e.At.UnixMilli(), e.Action, e.Target, string(detail), e.Client, e.Outcome)
	if err != nil {
		return fmt.Errorf("operator: append audit: %w", err)
	}
	return nil
}

// Audit pages the log newest first; before is an entry id (0 = from the newest).
func (s *Store) Audit(ctx context.Context, before int64, limit int) ([]domain.AuditEntry, error) {
	query := `SELECT id, at, action, target, detail_json, client, outcome FROM audit_log`
	args := []any{}
	if before > 0 {
		query += ` WHERE id < ?`
		args = append(args, before)
	}
	query += ` ORDER BY id DESC LIMIT ?`
	args = append(args, limit)
	rows, err := s.db.QueryContext(ctx, query, args...)
	if err != nil {
		return nil, fmt.Errorf("operator: list audit: %w", err)
	}
	defer rows.Close()
	out := []domain.AuditEntry{}
	for rows.Next() {
		var (
			e      domain.AuditEntry
			at     int64
			detail string
		)
		if err := rows.Scan(&e.ID, &at, &e.Action, &e.Target, &detail, &e.Client, &e.Outcome); err != nil {
			return nil, fmt.Errorf("operator: scan audit: %w", err)
		}
		e.At, e.Detail = time.UnixMilli(at).UTC(), json.RawMessage(detail)
		out = append(out, e)
	}
	return out, rows.Err()
}

// PurgeAudit deletes entries older than the cutoff (the trigger refuses anything younger than
// three years, so a wrong cutoff cannot erase recent evidence).
func (s *Store) PurgeAudit(ctx context.Context, before time.Time) (int, error) {
	res, err := s.db.ExecContext(ctx, `DELETE FROM audit_log WHERE at < ?`, before.UnixMilli())
	if err != nil {
		return 0, fmt.Errorf("operator: purge audit: %w", err)
	}
	n, _ := res.RowsAffected()
	return int(n), nil
}

func nullString(s string) any {
	if s == "" {
		return nil
	}
	return s
}

func unix(t *time.Time) any {
	if t == nil {
		return nil
	}
	return t.UnixMilli()
}

func millis(v sql.NullInt64) *time.Time {
	if !v.Valid {
		return nil
	}
	t := time.UnixMilli(v.Int64).UTC()
	return &t
}
