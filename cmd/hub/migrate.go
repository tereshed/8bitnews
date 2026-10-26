package main

import (
	"fmt"
	"os"

	"github.com/tereshed/8bitnews/internal/config"
	"github.com/tereshed/8bitnews/internal/store"
)

func runMigrate(args []string) {
	if len(args) != 1 || (args[0] != "up" && args[0] != "down") {
		fmt.Fprintln(os.Stderr, "usage: hub migrate up|down")
		os.Exit(2)
	}

	cfg, err := config.Load()
	if err != nil {
		fatal(err)
	}

	switch args[0] {
	case "up":
		err = store.MigrateUp(cfg.DatabaseURL)
	case "down":
		err = store.MigrateDown(cfg.DatabaseURL)
	}
	if err != nil {
		fatal(err)
	}
}
