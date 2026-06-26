package main

import (
	"context"
	"flag"
	"fmt"
	"os"
	"time"

	"pqmedia/be/internal/config"
	"pqmedia/be/internal/database"
)

func main() {
	dir := flag.String("dir", "db/migrations", "path to migration directory")
	envFile := flag.String("env", ".env", "path to dotenv file")
	cmd := flag.String("cmd", "up", "command: up | status")
	flag.Parse()

	cfg, err := config.Load(*envFile)
	if err != nil {
		fatal(err)
	}

	ctx, cancel := context.WithTimeout(context.Background(), 60*time.Second)
	defer cancel()

	db, err := database.NewPool(ctx, cfg.DatabaseURL)
	if err != nil {
		fatal(err)
	}
	defer db.Close()

	switch *cmd {
	case "up":
		if err := database.ApplyMigrations(ctx, db, *dir); err != nil {
			fatal(err)
		}
		fmt.Println("migrations applied")
	case "status":
		states, err := database.Status(ctx, db, *dir)
		if err != nil {
			fatal(err)
		}
		for _, s := range states {
			mark := "pending"
			if s.Applied {
				mark = "applied"
			}
			fmt.Printf("%s\t%s\n", mark, s.Version)
		}
	default:
		fatal(fmt.Errorf("unknown cmd %q", *cmd))
	}
}

func fatal(err error) {
	fmt.Fprintln(os.Stderr, "migrate:", err)
	os.Exit(1)
}
