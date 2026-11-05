package citizencli

import (
	"civora/internal/archive"
	"civora/internal/citizen"
	"civora/internal/codec"
	"civora/internal/electionaudit"
	"civora/internal/partition"
	"crypto/ed25519"
	"encoding/hex"
	"flag"
	"fmt"
	"os"
	"path/filepath"
)

func election(args []string) error {
	if len(args) == 0 {
		return fmt.Errorf("election export | verify")
	}
	f := flag.NewFlagSet("election "+args[0], flag.ContinueOnError)
	home := f.String("home", ".dev-host", "initialized host")
	manifest := f.String("manifest", "", "authenticated network manifest; defaults to home/network.json")
	input := f.String("input", "", "public selective record directory")
	output := f.String("output", "election-record", "output directory")
	poll := f.String("poll-id", "", "poll ID")
	key := f.String("attest-key", "", "explicit TEST_ONLY auditor key, after successful replay")
	if e := f.Parse(args[1:]); e != nil {
		return e
	}
	path := *manifest
	if path == "" {
		path = filepath.Join(*home, "network.json")
	}
	raw, e := os.ReadFile(path)
	if e != nil {
		return e
	}
	m, e := citizen.ParseManifest(raw)
	if e != nil {
		return e
	}
	switch args[0] {
	case "export":
		store := archive.Store{Directory: *output}
		record, e := citizen.DownloadRecord(m, *poll, store)
		if e != nil {
			return e
		}
		if e = citizen.ExportRecord(*output, record, store); e != nil {
			return e
		}
		fmt.Println("Public record exported:", *output)
		return nil
	case "verify":
		raw, e := os.ReadFile(filepath.Join(*input, "record.json"))
		if e != nil {
			return e
		}
		var record electionaudit.Record
		if e = codec.Decode(raw, "citizen_election_record", &record); e != nil {
			return e
		}
		report, e := electionaudit.Verify(record, archive.Store{Directory: *input}, m.Main.Genesis.SHA256)
		if e != nil {
			return e
		}
		if *key != "" {
			if report.State != "provisional" && report.State != "certified" {
				return fmt.Errorf("cannot attest a contested or expired dossier")
			}
			raw, e := os.ReadFile(*key)
			if e != nil {
				return e
			}
			var k struct {
				Label  string `json:"label"`
				Seed   string `json:"seed"`
				Public string `json:"public_key"`
			}
			if e = codec.Decode(raw, "citizen_test_key", &k); e != nil {
				return e
			}
			seed, _ := hex.DecodeString(k.Seed)
			private := ed25519.NewKeyFromSeed(seed)
			if hex.EncodeToString(private.Public().(ed25519.PublicKey)) != k.Public {
				return fmt.Errorf("key identity mismatch")
			}
			a := partition.Attestation{PollID: report.PollID, Dossier: report.Dossier, Implementation: partition.GoAudit, Signer: k.Public}
			a.Signature = hex.EncodeToString(ed25519.Sign(private, partition.AttestationMessage(record.Frozen, a)))
			if e = partition.VerifyAttestation(record.Frozen, a); e != nil {
				return e
			}
			fmt.Println(string(codec.Must(a)))
			return nil
		}
		fmt.Println(string(codec.Must(report)))
		return nil
	}
	return fmt.Errorf("unknown election command")
}
