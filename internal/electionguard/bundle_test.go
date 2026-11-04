package electionguard_test

import (
	"bytes"
	"civora/internal/codec"
	"civora/internal/electionguard"
	"encoding/base64"
	"encoding/json"
	"os"
	"strings"
	"testing"
)

func TestCrossLanguageEnvelopes(t *testing.T) {
	raw, err := os.ReadFile("../../spec/fixtures/v0.4/envelopes.json")
	if err != nil {
		t.Fatal(err)
	}
	var fixture struct {
		Context struct {
			NetworkID  string `json:"network_id"`
			PollID     string `json:"poll_id"`
			ProposalID string `json:"proposal_id"`
		} `json:"context"`
		Vectors []struct {
			Name       string             `json:"name"`
			Kind       electionguard.Kind `json:"kind"`
			Encoded    string             `json:"encoded"`
			Accept     bool               `json:"accept"`
			CheckLink  bool               `json:"check_link"`
			Inspection json.RawMessage    `json:"inspection"`
		} `json:"vectors"`
	}
	if err = json.Unmarshal(raw, &fixture); err != nil {
		t.Fatal(err)
	}
	for _, v := range fixture.Vectors {
		t.Run(v.Name, func(t *testing.T) {
			d, err := electionguard.Decode(v.Encoded, v.Kind)
			if err == nil && v.CheckLink {
				err = d.CheckLink(fixture.Context.NetworkID, fixture.Context.PollID, fixture.Context.ProposalID)
			}
			if !v.Accept {
				if err == nil {
					t.Fatal("accepted malformed envelope/context")
				}
				return
			}
			if err != nil {
				t.Fatal(err)
			}
			if got := codec.Must(d.Inspect(v.Kind)); !bytes.Equal(got, v.Inspection) {
				t.Fatalf("inspection differs: %s", got)
			}
			if v.Kind == electionguard.Ceremony && string(d.Files["context.json"]) != "{\"n\":5, \"k\":3}\n" {
				t.Fatal("external bytes were rewritten")
			}
		})
	}
}

func TestResourceBounds(t *testing.T) {
	if _, err := electionguard.Decode(strings.Repeat("A", (electionguard.MaxBundleBytes*8+5)/6+1), electionguard.Ballot); err == nil {
		t.Fatal("oversize encoded envelope")
	}
	if _, err := electionguard.DecodeBytes(bytes.Repeat([]byte(" "), electionguard.MaxBundleBytes+1), electionguard.Ballot); err == nil {
		t.Fatal("oversize decoded envelope")
	}
	for _, n := range []int{electionguard.MaxFileBytes, electionguard.MaxFileBytes + 1} {
		b := electionguard.Bundle{Suite: electionguard.Suite, Files: []electionguard.File{{Path: "ballot.json", Bytes: base64.RawURLEncoding.EncodeToString(bytes.Repeat([]byte("x"), n))}}}
		_, err := electionguard.DecodeBytes(codec.Must(b), electionguard.Ballot)
		if (err == nil) != (n == electionguard.MaxFileBytes) {
			t.Fatalf("file boundary %d: %v", n, err)
		}
	}
	paths := []string{"context.json", "guardians/01.json", "guardians/02.json", "guardians/03.json", "guardians/04.json", "guardians/05.json", "link.json", "manifest.json"}
	for _, total := range []int{electionguard.MaxTotalFileBytes, electionguard.MaxTotalFileBytes + 1} {
		b := electionguard.Bundle{Suite: electionguard.Suite, Files: []electionguard.File{}}
		// Base64 is aligned at the accepted limit so the rejected case still
		// fits the overall envelope bound and exercises cumulative file size.
		for i, path := range paths {
			n := 12000
			if i == len(paths)-1 {
				n = total - 7*12000
			}
			b.Files = append(b.Files, electionguard.File{Path: path, Bytes: base64.RawURLEncoding.EncodeToString(bytes.Repeat([]byte("x"), n))})
		}
		raw := codec.Must(b)
		if len(raw) > electionguard.MaxBundleBytes {
			t.Fatal("test cannot reach cumulative bound")
		}
		_, err := electionguard.DecodeBytes(raw, electionguard.Ceremony)
		if (err == nil) != (total == electionguard.MaxTotalFileBytes) {
			t.Fatalf("total boundary %d: %v", total, err)
		}
	}
}
