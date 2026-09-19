// Package apierror is the single place that maps domain errors to HTTP status codes and JSON error
// bodies. Domain packages construct sentinel errors with the constructors below (NotFound,
// Conflict, Invalid, Unauthorized) instead of plain errors.New, so any interfaces/http handler can
// hand the error straight to Write and get a consistent response — mirrors an
// AppException-base/subclass-per-error pattern in idiomatic Go form (a shared error type plus a
// Kind, not a class hierarchy).
package apierror

import (
	"encoding/json"
	"errors"
	"net/http"
)

// Kind classifies a domain error into the HTTP status family it maps to.
type Kind int

const (
	KindInternal Kind = iota
	KindNotFound
	KindConflict
	KindInvalid
	KindUnauthorized
)

// Error is the typed/sentinel error every domain package should use for expected failure modes.
type Error struct {
	Kind    Kind
	Message string
}

func (e *Error) Error() string { return e.Message }

func NotFound(message string) *Error     { return &Error{Kind: KindNotFound, Message: message} }
func Conflict(message string) *Error     { return &Error{Kind: KindConflict, Message: message} }
func Invalid(message string) *Error      { return &Error{Kind: KindInvalid, Message: message} }
func Unauthorized(message string) *Error { return &Error{Kind: KindUnauthorized, Message: message} }

func statusFor(kind Kind) int {
	switch kind {
	case KindNotFound:
		return http.StatusNotFound
	case KindConflict:
		return http.StatusConflict
	case KindInvalid:
		return http.StatusBadRequest
	case KindUnauthorized:
		return http.StatusUnauthorized
	default:
		return http.StatusInternalServerError
	}
}

type body struct {
	Error string `json:"error"`
}

// Write maps err to an HTTP status and writes a `{"error": "..."}` JSON body. Errors that aren't
// *Error (or don't wrap one) are treated as internal errors and their detail is not leaked to the
// client.
func Write(w http.ResponseWriter, err error) {
	var apiErr *Error
	status := http.StatusInternalServerError
	message := "internal server error"
	if errors.As(err, &apiErr) {
		status = statusFor(apiErr.Kind)
		message = apiErr.Message
	}
	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(status)
	_ = json.NewEncoder(w).Encode(body{Error: message})
}
