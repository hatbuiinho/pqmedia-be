// Package httpx provides small HTTP helpers shared by handlers and middleware.
// Kept separate from internal/httpserver to avoid import cycles when handlers
// need response helpers and httpserver needs handlers.
package httpx

import (
	"encoding/json"
	"errors"
	"io"
	"net/http"
)

type errorDetail struct {
	Code    string `json:"code"`
	Message string `json:"message"`
}

type errorEnvelope struct {
	Error errorDetail `json:"error"`
}

// WriteJSON encodes v as JSON with the given status code.
func WriteJSON(w http.ResponseWriter, status int, v any) {
	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(status)
	if v == nil {
		return
	}
	_ = json.NewEncoder(w).Encode(v)
}

// WriteError writes a structured error envelope {error:{code,message}}.
func WriteError(w http.ResponseWriter, status int, code, message string) {
	WriteJSON(w, status, errorEnvelope{Error: errorDetail{Code: code, Message: message}})
}

// ReadJSON decodes the request body into dst with a 1 MiB cap, rejecting unknown fields.
// Returns io.EOF when the body is empty so callers can distinguish "missing body".
func ReadJSON(r *http.Request, dst any) error {
	r.Body = http.MaxBytesReader(nil, r.Body, 1<<20)
	dec := json.NewDecoder(r.Body)
	dec.DisallowUnknownFields()
	if err := dec.Decode(dst); err != nil {
		if errors.Is(err, io.EOF) {
			return err
		}
		return err
	}
	return nil
}
