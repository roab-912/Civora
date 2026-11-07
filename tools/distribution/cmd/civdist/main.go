// civdist verifies mirrored software, without installing it or authorizing a protocol upgrade.
package main

import (
	"encoding/json"
	"errors"
	"flag"
	"fmt"
	"os"
	"time"

	"civora/distribution/internal/distribution"
)

func main() {
	if err := run(os.Args[1:]); err != nil {
		fmt.Fprintln(os.Stderr, err)
		os.Exit(1)
	}
}

func run(args []string) error {
	if len(args) == 0 {
		return errors.New("usage: civdist verify|archive|seal-test (use -h for flags)")
	}
	f := flag.NewFlagSet(args[0], flag.ContinueOnError)
	switch args[0] {
	case "verify":
		var o distribution.Options
		f.StringVar(&o.Repository, "repository", "", "untrusted local mirror")
		f.StringVar(&o.RootFile, "root", "", "installed initial root obtained independently")
		f.StringVar(&o.RootSHA256, "root-sha256", "", "independently obtained root pin")
		f.StringVar(&o.StateFile, "state", "", "persistent anti-rollback database; retain across updates")
		f.BoolVar(&o.AllowTestKeys, "allow-test-keys", false, "DANGER: allow PUBLIC TEST KEYS for a local exercise")
		if err := f.Parse(args[1:]); err != nil {
			return err
		}
		if f.NArg() != 0 {
			return errors.New("unexpected arguments")
		}
		r, err := distribution.Verify(o)
		if err != nil {
			return err
		}
		return json.NewEncoder(os.Stdout).Encode(r)
	case "archive":
		source := f.String("source", "", "explicit staging directory")
		out := f.String("output", "", "new deterministic ZIP")
		if err := f.Parse(args[1:]); err != nil {
			return err
		}
		if *source == "" || *out == "" || f.NArg() != 0 {
			return errors.New("source and output required")
		}
		return distribution.Archive(*source, *out)
	case "seal-test":
		payload := f.String("payload", "", "explicit public payload directory")
		repo := f.String("repository", "", "new local repository")
		epoch := f.String("epoch", "", "explicit RFC3339 reference time; timestamp expires after 24h")
		version := f.Int64("version", 1, "monotone metadata version")
		allow := f.Bool("allow-test-keys", false, "acknowledge PUBLIC TEST KEYS")
		if err := f.Parse(args[1:]); err != nil {
			return err
		}
		if !*allow || *payload == "" || *repo == "" || f.NArg() != 0 {
			return errors.New("payload, repository and explicit --allow-test-keys required")
		}
		t, err := time.Parse(time.RFC3339, *epoch)
		if err != nil {
			return err
		}
		return distribution.SealTest(*payload, *repo, t, *version)
	default:
		return errors.New("unknown command")
	}
}
