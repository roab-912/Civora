// Package electionguard implements Civic envelopes and public ElectionGuard 2.0
// verification. Decoding an envelope alone proves neither ballot correctness nor
// a tally. See spec/decisions/0010-electionguard-v0.4.md.
package electionguard

import (
	"civora/internal/codec"
	"crypto/sha256"
	"encoding/hex"
	"fmt"
)

const Suite = "eg-2.0-civic-1"

// These development limits bound allocation before JSON/base64 decoding. The
// complete transaction still has to satisfy core.MaxTxBytes independently.
const MaxBundleBytes = 128 * 1024
const MaxFileBytes = 64 * 1024
const MaxTotalFileBytes = 90 * 1024

type Kind string

const (
	Ceremony Kind = "ceremony"
	Ballot   Kind = "ballot"
	Tally    Kind = "tally"
)

type File struct {
	Path  string `json:"path"`
	Bytes string `json:"bytes"`
}

type Bundle struct {
	Suite string `json:"suite"`
	Files []File `json:"files"`
}

type Link struct {
	NetworkID      string `json:"network_id"`
	PollID         string `json:"poll_id"`
	ProposalID     string `json:"proposal_id"`
	QuestionDigest string `json:"question_digest"`
}

// Decoded keeps external file bytes intact. ElectionGuard has its own numeric
// types and canonical order; the Civic codec must never rewrite those files.
type Decoded struct {
	Bundle Bundle
	Raw    []byte
	Files  map[string][]byte
}

func expected(kind Kind) ([]string, error) {
	switch kind {
	case Ceremony:
		return []string{"context.json", "guardians/01.json", "guardians/02.json", "guardians/03.json", "guardians/04.json", "guardians/05.json", "link.json", "manifest.json"}, nil
	case Ballot:
		return []string{"ballot.json"}, nil
	case Tally:
		return []string{"tally.json"}, nil
	default:
		return nil, fmt.Errorf("ERR_EG_KIND")
	}
}

// Decode accepts strict unpadded base64url around the canonical Civic envelope.
// Success means only that the envelope is well formed, never a valid vote.
func Decode(encoded string, kind Kind) (*Decoded, error) {
	if len(encoded) > (MaxBundleBytes*8+5)/6 {
		return nil, fmt.Errorf("ERR_EG_SIZE")
	}
	raw, err := codec.Blob(encoded)
	if err != nil {
		return nil, fmt.Errorf("ERR_EG_BASE64URL")
	}
	return DecodeBytes(raw, kind)
}

// DecodeBytes reads already decoded envelope bytes, e.g. from an archive. It
// never resolves paths, extracts files, opens a URL or reads a local file.
func DecodeBytes(raw []byte, kind Kind) (*Decoded, error) {
	paths, err := expected(kind)
	if err != nil {
		return nil, err
	}
	if len(raw) > MaxBundleBytes {
		return nil, fmt.Errorf("ERR_EG_SIZE")
	}
	var b Bundle
	if err = codec.Decode(raw, "eg_bundle", &b); err != nil {
		return nil, fmt.Errorf("ERR_EG_BUNDLE")
	}
	if len(b.Files) != len(paths) {
		return nil, fmt.Errorf("ERR_EG_FILES")
	}
	files := make(map[string][]byte, len(paths))
	total := 0
	for i, file := range b.Files {
		// An exact allowlist also rejects traversal, absolute paths, alternate
		// separators, case aliases, duplicate paths and non-ASCII names.
		if file.Path != paths[i] {
			return nil, fmt.Errorf("ERR_EG_FILES")
		}
		if len(file.Bytes) > (MaxFileBytes*8+5)/6 {
			return nil, fmt.Errorf("ERR_EG_SIZE")
		}
		data, err := codec.Blob(file.Bytes)
		if err != nil || len(data) == 0 {
			return nil, fmt.Errorf("ERR_EG_BASE64URL")
		}
		total += len(data)
		if len(data) > MaxFileBytes || total > MaxTotalFileBytes {
			return nil, fmt.Errorf("ERR_EG_SIZE")
		}
		files[file.Path] = data
	}
	return &Decoded{Bundle: b, Raw: append([]byte(nil), raw...), Files: files}, nil
}

// CheckLink compares the ceremony's Civic link against caller-supplied context.
// Authenticating that context and checking the ElectionGuard manifest, keys and
// proofs are separate obligations, still blocked on the interoperability gate.
func (d *Decoded) CheckLink(network, poll, proposal string) error {
	var link Link
	if err := codec.Decode(d.Files["link.json"], "eg_link", &link); err != nil {
		return fmt.Errorf("ERR_EG_LINK")
	}
	if link.NetworkID != network || link.PollID != poll || link.ProposalID != proposal ||
		link.QuestionDigest != codec.Hash("poll/question", []any{network, poll, proposal}) {
		return fmt.Errorf("ERR_EG_LINK")
	}
	return nil
}

type FileInfo struct {
	Path   string `json:"path"`
	Size   string `json:"size"`
	SHA256 string `json:"sha256"`
}

type Inspection struct {
	Suite                string     `json:"suite"`
	Kind                 Kind       `json:"kind"`
	EnvelopeSHA256       string     `json:"envelope_sha256"`
	Files                []FileInfo `json:"files"`
	CryptographyVerified bool       `json:"cryptography_verified"`
	SuiteQualified       bool       `json:"suite_qualified"`
}

// Inspect returns archive metadata, explicitly marked as not crypto-verified.
func (d *Decoded) Inspect(kind Kind) Inspection {
	h := sha256.Sum256(d.Raw)
	r := Inspection{Suite: d.Bundle.Suite, Kind: kind, EnvelopeSHA256: hex.EncodeToString(h[:]), Files: []FileInfo{}}
	for _, f := range d.Bundle.Files {
		data := d.Files[f.Path]
		digest := sha256.Sum256(data)
		r.Files = append(r.Files, FileInfo{Path: f.Path, Size: fmt.Sprint(len(data)), SHA256: hex.EncodeToString(digest[:])})
	}
	return r
}
