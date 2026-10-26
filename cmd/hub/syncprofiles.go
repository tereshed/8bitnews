package main

import (
	"context"
	"flag"
	"fmt"

	"github.com/tereshed/8bitnews/internal/config"
	"github.com/tereshed/8bitnews/internal/profilesync"
	"github.com/tereshed/8bitnews/internal/store"
)

func runSyncProfiles(args []string) {
	fs := flag.NewFlagSet("sync-profiles", flag.ExitOnError)
	root := fs.String("root", "", "repository root containing channels/ and countries/ (defaults to HUB_REPO_ROOT or .)")
	if err := fs.Parse(args); err != nil {
		fatal(err)
	}

	cfg, err := config.Load()
	if err != nil {
		fatal(err)
	}
	repoRoot := cfg.RepoRoot
	if *root != "" {
		repoRoot = *root
	}

	ctx := context.Background()
	st, err := store.Open(ctx, cfg.DatabaseURL)
	if err != nil {
		fatal(err)
	}
	defer st.Close()

	res, err := profilesync.Sync(ctx, st, repoRoot)
	if err != nil {
		fatal(err)
	}
	fmt.Printf("synced %d countries, %d channels\n", res.Countries, res.Channels)
}
