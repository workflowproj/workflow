// Package apperr defines errors that carry an HTTP status and a machine-readable code.
// Handlers return them; the HTTP layer renders RFC 9457 problem details.
package apperr

import (
	"errors"
	"fmt"
	"net/http"
)

type FieldError struct {
	Field   string `json:"field,omitempty"`
	Message string `json:"message"`
}

type Error struct {
	Status   int
	Code     string
	Message  string
	Fields   []FieldError
	Position *int
}

func (e *Error) Error() string { return e.Message }

func New(status int, code, format string, args ...any) *Error {
	return &Error{Status: status, Code: code, Message: fmt.Sprintf(format, args...)}
}

func (e *Error) WithFields(fields ...FieldError) *Error {
	e.Fields = append(e.Fields, fields...)
	return e
}

func NotFound(what string) *Error {
	return New(http.StatusNotFound, "not_found", "%s not found", what)
}

func BadRequest(format string, args ...any) *Error {
	return New(http.StatusBadRequest, "bad_request", format, args...)
}

func Invalid(field, format string, args ...any) *Error {
	msg := fmt.Sprintf(format, args...)
	return &Error{Status: http.StatusBadRequest, Code: "invalid", Message: msg, Fields: []FieldError{{Field: field, Message: msg}}}
}

func TooLarge(format string, args ...any) *Error {
	return New(http.StatusRequestEntityTooLarge, "too_large", format, args...)
}

func Unauthorized() *Error {
	return New(http.StatusUnauthorized, "unauthorized", "authentication required")
}

func Forbidden(format string, args ...any) *Error {
	return New(http.StatusForbidden, "forbidden", format, args...)
}

func Conflict(code, format string, args ...any) *Error {
	return New(http.StatusConflict, code, format, args...)
}

// Unprocessable is used for well-formed requests that fail domain rules (e.g. workflow validators).
func Unprocessable(code, format string, args ...any) *Error {
	return New(http.StatusUnprocessableEntity, code, format, args...)
}

// As extracts an *Error from err.
func As(err error) (*Error, bool) {
	var e *Error
	if errors.As(err, &e) {
		return e, true
	}
	return nil, false
}

func IsNotFound(err error) bool {
	e, ok := As(err)
	return ok && e.Status == http.StatusNotFound
}
