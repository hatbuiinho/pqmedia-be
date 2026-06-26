// seeddata creates an initial admin user in dev/staging. Idempotent: if an account
// with the same email exists, it logs and exits 0.
package main

import (
	"context"
	"errors"
	"flag"
	"fmt"
	"os"
	"strings"
	"time"

	"pqmedia/be/internal/auth"
	"pqmedia/be/internal/config"
	"pqmedia/be/internal/database"
	"pqmedia/be/internal/repository"
)

func main() {
	email := flag.String("email", "admin@pqmedia.local", "admin email")
	password := flag.String("password", "admin12345", "admin password (min 8 chars)")
	fullName := flag.String("name", "Quản trị", "admin full name")
	envFile := flag.String("env", ".env", "path to dotenv file")
	flag.Parse()

	if len(*password) < 8 {
		exitf("password must be at least 8 characters")
	}

	cfg, err := config.Load(*envFile)
	if err != nil {
		exitf("load config: %v", err)
	}

	ctx, cancel := context.WithTimeout(context.Background(), 30*time.Second)
	defer cancel()

	db, err := database.NewPool(ctx, cfg.DatabaseURL)
	if err != nil {
		exitf("connect db: %v", err)
	}
	defer db.Close()

	repo := repository.New(db)
	emailNorm := strings.ToLower(strings.TrimSpace(*email))

	if _, err := repo.GetUserByEmail(ctx, emailNorm); err == nil {
		fmt.Printf("admin %s already exists, nothing to do\n", emailNorm)
		return
	} else if !errors.Is(err, repository.ErrNotFound) {
		exitf("lookup admin: %v", err)
	}

	hash, err := auth.HashPassword(*password)
	if err != nil {
		exitf("hash password: %v", err)
	}
	created, err := repo.CreateUserWithProfile(ctx, repository.CreateUserParams{
		Email:        emailNorm,
		PasswordHash: hash,
		IsAdmin:      true,
		FullName:     *fullName,
	})
	if err != nil {
		exitf("create admin: %v", err)
	}
	fmt.Printf("admin created: id=%s email=%s\n", created.User.ID, created.User.Email)
}

func exitf(format string, args ...any) {
	fmt.Fprintln(os.Stderr, "seeddata:", fmt.Sprintf(format, args...))
	os.Exit(1)
}
