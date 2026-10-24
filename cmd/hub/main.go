// Command hub is the 8bitnews hub binary: migrate (apply/roll back the
// embedded Postgres schema), sync-profiles (mirror channels/ and
// countries/ into Postgres) and serve (REST + MCP API for PolyMaths
// agents) — docs/architecture.md §6.
package main

import (
	"fmt"
	"os"
)

func main() {
	if len(os.Args) < 2 {
		usage()
		os.Exit(2)
	}

	switch os.Args[1] {
	case "migrate":
		runMigrate(os.Args[2:])
	case "sync-profiles":
		runSyncProfiles(os.Args[2:])
	case "serve":
		runServe(os.Args[2:])
	default:
		usage()
		os.Exit(2)
	}
}

func usage() {
	fmt.Fprintln(os.Stderr, "usage: hub migrate up|down")
	fmt.Fprintln(os.Stderr, "       hub sync-profiles [--root <path>]")
	fmt.Fprintln(os.Stderr, "       hub serve")
}

func fatal(err error) {
	fmt.Fprintln(os.Stderr, "hub:", err)
	os.Exit(1)
}
