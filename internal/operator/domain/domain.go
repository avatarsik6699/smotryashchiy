// Package domain holds the operator console's value types and validation (docs/SPEC.md §4j):
// the closed set of commands the tracked site's host may run, incidents and audit entries.
package domain

import (
	"bytes"
	"encoding/json"
	"regexp"
	"strconv"
	"strings"
	"time"
	"unicode"
	"unicode/utf8"

	"github.com/avatarsik6699/smotryashchiy/internal/platform/apierror"
)

// Kind is one allow-listed operator command. The host's agent and the host's executor keep their
// own copy of this list; the server can only ask.
type Kind string

const (
	KindWarn          Kind = "warn"
	KindSuspend       Kind = "suspend"
	KindTerminate     Kind = "terminate"
	KindLift          Kind = "lift"
	KindRefundExecute Kind = "refund_execute"
	KindAccountShow   Kind = "account_show"
)

// Kinds is the allow-list in a stable order.
var Kinds = []Kind{KindWarn, KindSuspend, KindTerminate, KindLift, KindRefundExecute, KindAccountShow}

// Valid reports whether k is allow-listed.
func (k Kind) Valid() bool {
	for _, known := range Kinds {
		if k == known {
			return true
		}
	}
	return false
}

// NeedsReason reports whether the student sees a reason for this measure.
func (k Kind) NeedsReason() bool { return k == KindWarn || k == KindSuspend || k == KindTerminate }

// NeedsStepUp reports whether queueing this kind needs the admin password again.
func (k Kind) NeedsStepUp() bool { return k == KindTerminate || k == KindRefundExecute }

// Status is where a command is in its life.
type Status string

const (
	StatusQueued  Status = "queued"
	StatusRunning Status = "running"
	StatusDone    Status = "done"
	StatusFailed  Status = "failed"
	StatusExpired Status = "expired"
)

// Limits of the command channel.
const (
	MaxReasonBytes   = 300
	MaxNoteBytes     = 1000
	MaxEmailBytes    = 320
	MaxResultBytes   = 64 << 10
	MaxErrorBytes    = 2 << 10
	MaxResultBody    = 80 << 10
	ClaimBatch       = 5
	QueuedExpiry     = 10 * time.Minute
	RunningExpiry    = 5 * time.Minute
	ResultRetention  = 30 * 24 * time.Hour
	AuditRetention   = 3 * 365 * 24 * time.Hour
	DefaultListLimit = 50
	MaxListLimit     = 200
)

// Command is one queued operator action and, once the host answered, its outcome.
type Command struct {
	ID         string          `json:"id"`
	HostID     string          `json:"host_id"`
	Kind       Kind            `json:"kind"`
	Args       json.RawMessage `json:"args"`
	Reason     string          `json:"reason,omitempty"`
	Status     Status          `json:"status"`
	CreatedAt  time.Time       `json:"created_at"`
	StartedAt  *time.Time      `json:"started_at"`
	FinishedAt *time.Time      `json:"finished_at"`
	Result     json.RawMessage `json:"result"`
	Error      string          `json:"error,omitempty"`
}

// Claimed is what the host's agent receives for one command.
type Claimed struct {
	ID   string          `json:"id"`
	Kind Kind            `json:"kind"`
	Args json.RawMessage `json:"args"`
}

// NewCommand is a command as the operator submitted it, before validation.
type NewCommand struct {
	HostID   string          `json:"host_id"`
	Kind     Kind            `json:"kind"`
	Args     json.RawMessage `json:"args"`
	Reason   string          `json:"reason"`
	Password string          `json:"password"`
}

// commandArgs is the union of every kind's arguments; each kind accepts only its own subset.
type commandArgs struct {
	User       *string `json:"user,omitempty"`
	Email      *string `json:"email,omitempty"`
	Days       *int    `json:"days,omitempty"`
	Compensate *bool   `json:"compensate,omitempty"`
	Refund     *string `json:"refund,omitempty"`
	Note       *string `json:"note,omitempty"`
	Reason     *string `json:"reason,omitempty"`
}

var (
	uuidPattern  = regexp.MustCompile(`^[0-9a-f]{8}-[0-9a-f]{4}-[0-9a-f]{4}-[0-9a-f]{4}-[0-9a-f]{12}$`)
	emailPattern = regexp.MustCompile(`^[^@\s]+@[^@\s]+$`)
)

// Validate checks the kind and its arguments and returns the arguments the host receives: the
// account target, the kind's own fields and, for a measure, the reason the student sees.
func (n NewCommand) Validate() (json.RawMessage, error) {
	if !n.Kind.Valid() {
		return nil, apierror.Invalid("kind must be one of warn, suspend, terminate, lift, refund_execute, account_show")
	}
	if strings.TrimSpace(n.HostID) == "" {
		return nil, apierror.Invalid("host_id is required")
	}
	var args commandArgs
	raw := n.Args
	if len(bytes.TrimSpace(raw)) == 0 {
		raw = json.RawMessage(`{}`)
	}
	dec := json.NewDecoder(bytes.NewReader(raw))
	dec.DisallowUnknownFields()
	if err := dec.Decode(&args); err != nil {
		return nil, apierror.Invalid("args: " + strings.TrimPrefix(err.Error(), "json: "))
	}
	if args.Reason != nil {
		return nil, apierror.Invalid("args.reason: the reason is a top-level field")
	}
	allowed := map[string]bool{}
	switch n.Kind {
	case KindWarn, KindTerminate:
		allowed = map[string]bool{"user": true, "email": true, "note": true}
	case KindSuspend:
		allowed = map[string]bool{"user": true, "email": true, "note": true, "days": true}
	case KindLift:
		allowed = map[string]bool{"user": true, "email": true, "compensate": true}
	case KindAccountShow:
		allowed = map[string]bool{"user": true, "email": true}
	case KindRefundExecute:
		allowed = map[string]bool{"refund": true}
	}
	for name, present := range map[string]bool{
		"user": args.User != nil, "email": args.Email != nil, "days": args.Days != nil,
		"compensate": args.Compensate != nil, "refund": args.Refund != nil, "note": args.Note != nil,
	} {
		if present && !allowed[name] {
			return nil, apierror.Invalid("args." + name + " is not accepted for " + string(n.Kind))
		}
	}
	if n.Kind == KindRefundExecute {
		if args.Refund == nil || !uuidPattern.MatchString(*args.Refund) {
			return nil, apierror.Invalid("args.refund must be a refund UUID")
		}
	} else {
		if (args.User == nil) == (args.Email == nil) {
			return nil, apierror.Invalid("args must name exactly one of user or email")
		}
		if args.User != nil && !uuidPattern.MatchString(*args.User) {
			return nil, apierror.Invalid("args.user must be an account UUID")
		}
		if args.Email != nil && (len(*args.Email) > MaxEmailBytes || !emailPattern.MatchString(*args.Email)) {
			return nil, apierror.Invalid("args.email must be an address of at most 320 bytes")
		}
	}
	if n.Kind == KindSuspend && (args.Days == nil || *args.Days < 1 || *args.Days > 30) {
		return nil, apierror.Invalid("args.days must be 1..30")
	}
	if args.Note != nil {
		if err := checkText("args.note", *args.Note, MaxNoteBytes); err != nil {
			return nil, err
		}
	}
	reason := strings.TrimSpace(n.Reason)
	if n.Kind.NeedsReason() {
		if err := checkText("reason", reason, MaxReasonBytes); err != nil {
			return nil, err
		}
		args.Reason = &reason
	} else if reason != "" {
		return nil, apierror.Invalid("reason is only for warn, suspend and terminate")
	}
	out, err := json.Marshal(args)
	if err != nil {
		return nil, err
	}
	return out, nil
}

// checkText accepts one line of 1..max bytes of valid UTF-8 without control characters.
func checkText(field, value string, max int) error {
	if value == "" || len(value) > max || !utf8.ValidString(value) {
		return apierror.Invalid(field + " must contain 1.." + strconv.Itoa(max) + " bytes")
	}
	for _, r := range value {
		if unicode.IsControl(r) {
			return apierror.Invalid(field + " must not contain control characters")
		}
	}
	return nil
}

// Target names whom a command is about, for the audit log.
func Target(args json.RawMessage) string {
	var a commandArgs
	if json.Unmarshal(args, &a) != nil {
		return ""
	}
	switch {
	case a.User != nil:
		return "user:" + *a.User
	case a.Email != nil:
		return "email:" + *a.Email
	case a.Refund != nil:
		return "refund:" + *a.Refund
	}
	return ""
}

// Report is the host's answer to one command.
type Report struct {
	Status Status          `json:"status"`
	Result json.RawMessage `json:"result"`
	Error  string          `json:"error"`
}

// Validate accepts done or failed, a JSON object or null result of at most MaxResultBytes and an
// error of at most MaxErrorBytes (longer text is cut, never refused).
func (r Report) Validate() (Report, error) {
	if r.Status != StatusDone && r.Status != StatusFailed {
		return Report{}, apierror.Invalid("status must be done or failed")
	}
	trimmed := bytes.TrimSpace(r.Result)
	if len(trimmed) == 0 || bytes.Equal(trimmed, []byte("null")) {
		r.Result = nil
	} else {
		if len(trimmed) > MaxResultBytes || trimmed[0] != '{' || !json.Valid(trimmed) {
			return Report{}, apierror.Invalid("result must be a JSON object of at most 64 KiB")
		}
		r.Result = trimmed
	}
	if len(r.Error) > MaxErrorBytes {
		r.Error = strings.ToValidUTF8(r.Error[len(r.Error)-MaxErrorBytes:], "")
	}
	return r, nil
}

// IncidentKind classifies an incident; a leak has the RKN deadlines.
type IncidentKind string

const (
	IncidentLeak   IncidentKind = "leak"
	IncidentOutage IncidentKind = "outage"
	IncidentOther  IncidentKind = "other"
)

// Incident is one record the operator keeps by hand.
type Incident struct {
	ID              string       `json:"id"`
	Title           string       `json:"title"`
	Kind            IncidentKind `json:"kind"`
	DetectedAt      time.Time    `json:"detected_at"`
	Scope           string       `json:"scope"`
	PeopleAffected  *int         `json:"people_affected"`
	Measures        string       `json:"measures"`
	RKNPrimaryAt    *time.Time   `json:"rkn_primary_at"`
	RKNFinalAt      *time.Time   `json:"rkn_final_at"`
	UsersNotifiedAt *time.Time   `json:"users_notified_at"`
	Notes           string       `json:"notes"`
	ClosedAt        *time.Time   `json:"closed_at"`
	CreatedAt       time.Time    `json:"created_at"`
	UpdatedAt       time.Time    `json:"updated_at"`
}

// IncidentInput is a create or a full update of the editable fields.
type IncidentInput struct {
	Title           string       `json:"title"`
	Kind            IncidentKind `json:"kind"`
	DetectedAt      *time.Time   `json:"detected_at"`
	Scope           string       `json:"scope"`
	PeopleAffected  *int         `json:"people_affected"`
	Measures        string       `json:"measures"`
	RKNPrimaryAt    *time.Time   `json:"rkn_primary_at"`
	RKNFinalAt      *time.Time   `json:"rkn_final_at"`
	UsersNotifiedAt *time.Time   `json:"users_notified_at"`
	Notes           string       `json:"notes"`
	ClosedAt        *time.Time   `json:"closed_at"`
}

// Validate trims and checks the input; detection time defaults to now.
func (in IncidentInput) Validate(now time.Time) (IncidentInput, error) {
	in.Title = strings.TrimSpace(in.Title)
	if in.Title == "" || len(in.Title) > 200 {
		return IncidentInput{}, apierror.Invalid("title must contain 1..200 bytes")
	}
	switch in.Kind {
	case IncidentLeak, IncidentOutage, IncidentOther:
	default:
		return IncidentInput{}, apierror.Invalid("kind must be leak, outage or other")
	}
	if in.DetectedAt == nil {
		t := now.UTC()
		in.DetectedAt = &t
	}
	if in.DetectedAt.After(now.Add(5 * time.Minute)) {
		return IncidentInput{}, apierror.Invalid("detected_at must not be in the future")
	}
	for name, text := range map[string]string{"scope": in.Scope, "measures": in.Measures, "notes": in.Notes} {
		if len(text) > 10000 {
			return IncidentInput{}, apierror.Invalid(name + " must contain at most 10000 bytes")
		}
	}
	if in.PeopleAffected != nil && *in.PeopleAffected < 0 {
		return IncidentInput{}, apierror.Invalid("people_affected must not be negative")
	}
	return in, nil
}

// AuditEntry is one append-only record of an operator action or a host answer.
type AuditEntry struct {
	ID      int64           `json:"id"`
	At      time.Time       `json:"at"`
	Action  string          `json:"action"`
	Target  string          `json:"target"`
	Detail  json.RawMessage `json:"detail"`
	Client  string          `json:"client"`
	Outcome string          `json:"outcome"`
}
