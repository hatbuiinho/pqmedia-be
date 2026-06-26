// Package auth handles JWT issuing/parsing and password hashing.
package auth

import (
	"errors"
	"fmt"
	"time"

	"github.com/golang-jwt/jwt/v5"
)

// TokenType differentiates access vs refresh tokens so a refresh token cannot
// be replayed as an access token.
type TokenType string

const (
	AccessToken  TokenType = "access"
	RefreshToken TokenType = "refresh"
)

type Claims struct {
	UserID string    `json:"sub"`
	Type   TokenType `json:"typ"`
	jwt.RegisteredClaims
}

// Issue creates a signed HS256 token for the given user and type.
func Issue(secret, userID string, typ TokenType, ttl time.Duration, now time.Time) (string, time.Time, error) {
	expiresAt := now.Add(ttl)
	claims := Claims{
		UserID: userID,
		Type:   typ,
		RegisteredClaims: jwt.RegisteredClaims{
			IssuedAt:  jwt.NewNumericDate(now),
			ExpiresAt: jwt.NewNumericDate(expiresAt),
			NotBefore: jwt.NewNumericDate(now),
		},
	}
	token := jwt.NewWithClaims(jwt.SigningMethodHS256, claims)
	signed, err := token.SignedString([]byte(secret))
	if err != nil {
		return "", time.Time{}, fmt.Errorf("sign token: %w", err)
	}
	return signed, expiresAt, nil
}

// Parse validates the signature, expiry, and token type. Returns ErrInvalidToken
// on any failure to keep the surface small for callers.
func Parse(secret, raw string, expectedType TokenType) (Claims, error) {
	claims := Claims{}
	parsed, err := jwt.ParseWithClaims(raw, &claims, func(t *jwt.Token) (interface{}, error) {
		if _, ok := t.Method.(*jwt.SigningMethodHMAC); !ok {
			return nil, fmt.Errorf("unexpected signing method: %v", t.Method.Alg())
		}
		return []byte(secret), nil
	})
	if err != nil || !parsed.Valid {
		return Claims{}, ErrInvalidToken
	}
	if claims.Type != expectedType {
		return Claims{}, ErrInvalidToken
	}
	return claims, nil
}

// ErrInvalidToken is returned for any signature, expiry, or type mismatch.
var ErrInvalidToken = errors.New("invalid token")
