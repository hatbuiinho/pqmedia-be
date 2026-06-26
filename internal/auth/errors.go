package auth

import "errors"

// ErrInvalidCredentials is returned when an email/password pair does not match.
// Kept generic to avoid leaking which side was wrong.
var ErrInvalidCredentials = errors.New("invalid credentials")
