// Package service contains the business logic layer.
// Services depend on repository.Repo and return structured errors that handlers
// map directly to HTTP status codes via ErrorStatus().
package service

import "net/http"

// Error is the canonical error returned by service methods. Handlers translate it
// into an HTTP response via WriteServiceError.
type Error struct {
	Status  int
	Code    string
	Message string
}

func (e Error) Error() string { return e.Message }

func NewError(status int, code, message string) Error {
	return Error{Status: status, Code: code, Message: message}
}

var (
	ErrUnauthorized = NewError(http.StatusUnauthorized, "unauthorized", "authentication required")
	ErrForbidden    = NewError(http.StatusForbidden, "forbidden", "operation not allowed")
	ErrNotFound     = NewError(http.StatusNotFound, "not_found", "resource not found")
	ErrConflict     = NewError(http.StatusConflict, "conflict", "resource already exists")
)

func ValidationError(message string) Error {
	return NewError(http.StatusBadRequest, "validation_failed", message)
}
