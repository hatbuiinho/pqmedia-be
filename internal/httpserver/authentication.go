package httpserver

import (
	"errors"
	"net/http"
	"strings"

	"pqmedia/be/internal/authctx"
	"pqmedia/be/internal/httpx"
	"pqmedia/be/internal/service"
)

// Authentication validates the Bearer access token and stores the Principal in context.
func Authentication(userSvc *service.UserService) func(http.Handler) http.Handler {
	return func(next http.Handler) http.Handler {
		return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			token := bearerToken(r)
			if token == "" {
				httpx.WriteError(w, http.StatusUnauthorized, "unauthorized", "missing bearer token")
				return
			}
			principal, err := userSvc.PrincipalFromAccessToken(r.Context(), token)
			if err != nil {
				var se service.Error
				if errors.As(err, &se) {
					httpx.WriteError(w, se.Status, se.Code, se.Message)
					return
				}
				httpx.WriteError(w, http.StatusUnauthorized, "unauthorized", "invalid token")
				return
			}
			next.ServeHTTP(w, r.WithContext(authctx.WithPrincipal(r.Context(), principal)))
		})
	}
}

func bearerToken(r *http.Request) string {
	header := r.Header.Get("Authorization")
	const prefix = "Bearer "
	if !strings.HasPrefix(header, prefix) {
		return ""
	}
	return strings.TrimSpace(header[len(prefix):])
}
