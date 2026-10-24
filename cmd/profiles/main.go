// Command profiles validates the data-only channel and country profiles
// (channels/*/channel.yaml, countries/*/sources.yaml) against their JSON
// schemas and the cross-checks described in docs/architecture.md §10.
package main

import (
	"flag"
	"fmt"
	"os"

	"github.com/tereshed/8bitnews/internal/profile"
)

func main() {
	if len(os.Args) < 2 || os.Args[1] != "validate" {
		fmt.Fprintln(os.Stderr, "usage: profiles validate [--strict] [--root <path>]")
		os.Exit(2)
	}

	fs := flag.NewFlagSet("validate", flag.ExitOnError)
	strict := fs.Bool("strict", false, `also reject any "tbd" placeholder value`)
	root := fs.String("root", ".", "repository root containing channels/ and countries/")
	if err := fs.Parse(os.Args[2:]); err != nil {
		os.Exit(2)
	}

	result, err := profile.Validate(*root, *strict)
	if err != nil {
		fmt.Fprintln(os.Stderr, err)
		os.Exit(1)
	}

	for _, issue := range result.Issues {
		fmt.Println(issue.String())
	}

	if !result.OK() {
		os.Exit(1)
	}
}
