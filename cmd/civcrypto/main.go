// civcrypto inspects transport envelopes offline. It is not a ballot verifier.
package main

import (
	"civora/internal/codec"
	"civora/internal/electionguard"
	"flag"
	"fmt"
	"io"
	"os"
)

func run(args []string) error {
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
