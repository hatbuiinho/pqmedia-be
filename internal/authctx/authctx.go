// Package authctx carries the authenticated Principal through request context.
// Kept separate from httpserver and handlers so both can import it without cycles.
package authctx

import (
	"context"

	"pqmedia/be/internal/service"
)

type ctxKey struct{ name string }

var principalKey = ctxKey{name: "principal"}

// WithPrincipal returns a child context that carries p.
func WithPrincipal(ctx context.Context, p service.Principal) context.Context {
	return context.WithValue(ctx, principalKey, p)
}

// FromContext extracts the Principal previously attached by WithPrincipal.
func FromContext(ctx context.Context) (service.Principal, bool) {
	p, ok := ctx.Value(principalKey).(service.Principal)
	return p, ok
}

// MustPrincipal panics when called outside a protected route. Use only in handlers
// mounted under the Authentication middleware.
func MustPrincipal(ctx context.Context) service.Principal {
	p, ok := FromContext(ctx)
	if !ok {
		panic("authctx: principal missing from context")
	}
	return p
}
