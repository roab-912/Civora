// civcrypto inspects envelopes and exports/replays public development archives.
package main

import (
	"civora/internal/codec"
	"civora/internal/electionguard"
	"civora/internal/electionguarddev"
	"flag"
	"fmt"
	"io"
	"os"
)

func run(args []string) error {
	if len(args) > 0 && (args[0] == "audit" || args[0] == "export") {
		flags := flag.NewFlagSet(args[0], flag.ContinueOnError)
		input := flags.String("in", "", "public archive path")
		directory := flags.String("store", "", "existing single-writer ledger")
		output := flags.String("out", "", "new public archive path")
		supported := flags.Uint64("supported-epoch", 1, "maximum supported epoch")
		if e := flags.Parse(args[1:]); e != nil {
			return e
		}
		if flags.NArg() != 0 {
			return fmt.Errorf("unexpected arguments")
		}
		if args[0] == "export" {
			if *directory == "" || *output == "" || *input != "" {
				return fmt.Errorf("export requires -store and -out")
			}
			raw, e := electionguarddev.ExportArchive(*directory, *supported)
			if e != nil {
				return e
			}
			f, e := os.OpenFile(*output, os.O_CREATE|os.O_EXCL|os.O_WRONLY, 0600)
			if e != nil {
				return e
			}
			_, e = f.Write(raw)
			if e == nil {
				e = f.Sync()
			}
			closeErr := f.Close()
			if e != nil {
				return e
			}
			return closeErr
		}
		if *input == "" || *directory != "" || *output != "" {
			return fmt.Errorf("audit requires only -in")
		}
		f, e := os.Open(*input)
		if e != nil {
			return e
		}
		defer f.Close()
		raw, e := io.ReadAll(io.LimitReader(f, electionguarddev.MaxArchiveBytes+1))
		if e != nil {
			return e
		}
		report, e := electionguarddev.ReplayArchive(raw, *supported)
		if e != nil {
			return e
		}
		fmt.Println(string(codec.Must(report)))
		return nil
	}
	if len(args) > 0 && args[0] == "fixtures" {
		if len(args) == 1 {
			return electionguarddev.Fixtures(false)
		}
		if len(args) == 2 && args[1] == "-check" {
			return electionguarddev.Fixtures(true)
		}
		return fmt.Errorf("usage: civcrypto fixtures [-check]")
	}
	if len(args) == 0 || args[0] != "inspect" {
		return fmt.Errorf("usage: civcrypto inspect -kind ceremony|ballot|tally -in envelope.json [-network id -poll id -proposal id]; envelope inspection only, no cryptographic verification")
	}
	flags := flag.NewFlagSet("inspect", flag.ContinueOnError)
	kind := flags.String("kind", "", "ceremony, ballot or tally")
	path := flags.String("in", "", "canonical decoded Civic envelope (not base64)")
	network := flags.String("network", "", "expected network for ceremony link")
	poll := flags.String("poll", "", "expected poll for ceremony link")
	proposal := flags.String("proposal", "", "expected immutable proposal for ceremony link")
	if err := flags.Parse(args[1:]); err != nil {
		return err
	}
	if flags.NArg() != 0 || *path == "" {
		return fmt.Errorf("supply -in and no positional arguments")
	}
	if *kind == "ceremony" && (*network == "" || *poll == "" || *proposal == "") {
		return fmt.Errorf("ceremony inspection requires -network, -poll and -proposal")
	}
	f, err := os.Open(*path)
	if err != nil {
		return err
	}
	defer f.Close()
	raw, err := io.ReadAll(io.LimitReader(f, electionguard.MaxBundleBytes+1))
	if err != nil {
		return err
	}
	d, err := electionguard.DecodeBytes(raw, electionguard.Kind(*kind))
	if err != nil {
		return err
	}
	if *kind == "ceremony" {
		if err = d.CheckLink(*network, *poll, *proposal); err != nil {
			return err
		}
	}
	fmt.Println(string(codec.Must(d.Inspect(electionguard.Kind(*kind)))))
	return nil
}

func main() {
	if err := run(os.Args[1:]); err != nil {
		fmt.Fprintln(os.Stderr, err)
		os.Exit(1)
	}
}
