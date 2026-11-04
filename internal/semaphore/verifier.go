// Package semaphore verifies the pinned Semaphore v4.14.5 Groth16 profile.
package semaphore

import (
	"civora/internal/codec"
	"civora/internal/eligibility"
	"civora/internal/ports"
	"civora/spec"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"fmt"
	"github.com/iden3/go-rapidsnark/types"
	"github.com/iden3/go-rapidsnark/verifier"
	"golang.org/x/crypto/sha3"
	"math/big"
	"strconv"
	"strings"
)

const CoordinateModulus = "21888242871839275222246405745257275088696311157297823662689037894645226208583"

var coordinateModulus, _ = new(big.Int).SetString(CoordinateModulus, 10)

type Membership struct {
	Depth     string   `json:"merkleTreeDepth"`
	Root      string   `json:"merkleTreeRoot"`
	Nullifier string   `json:"nullifier"`
	Message   string   `json:"message"`
	Scope     string   `json:"scope"`
	Points    []string `json:"points"`
}
type Context struct {
	NetworkID string `json:"network_id"`
	PollID    string `json:"poll_id"`
	Epoch     string `json:"protocol_epoch"`
	SuiteID   string `json:"suite_id"`
	Depth     string `json:"circuit_depth"`
	Root      string `json:"group_root"`
}
type Verifier struct{ keys map[string][]byte }

func (v *Verifier) VerifyMembership(c ports.BulletinContext, root, depth string, ballot, raw []byte) error {
	var p Membership
	if e := codec.Decode(raw, "membership_v03", &p); e != nil {
		return e
	}
	return v.Verify(Context{NetworkID: c.NetworkID, PollID: c.PollID, Epoch: c.Epoch, SuiteID: c.EligibilitySuite, Depth: depth, Root: root}, ballot, p)
}

var _ ports.MembershipVerifier = (*Verifier)(nil)

type artifact struct {
	File   string `json:"file"`
	SHA256 string `json:"sha256"`
	Size   string `json:"size"`
}

func New() (*Verifier, error) {
	raw, e := spec.Crypto.ReadFile("crypto/semaphore-v4.lock.json")
	if e != nil {
		return nil, e
	}
	var lock struct {
		SDKVersion     string `json:"sdk_version"`
		CircuitVersion string `json:"circuit_version"`
		Depths         []struct {
			Depth string   `json:"depth"`
			VK    artifact `json:"verification_key"`
		} `json:"depths"`
	}
	if e = json.Unmarshal(raw, &lock); e != nil {
		return nil, e
	}
	if lock.SDKVersion != "4.14.5" || lock.CircuitVersion != "4.13.0" {
		return nil, fmt.Errorf("ERR_CRYPTO_LOCK")
	}
	v := &Verifier{keys: map[string][]byte{}}
	for _, d := range lock.Depths {
		if d.Depth != "16" && d.Depth != "26" {
			return nil, fmt.Errorf("ERR_CIRCUIT_DEPTH")
		}
		data, e := spec.Crypto.ReadFile("crypto/semaphore-" + d.Depth + ".verification-key.json")
		if e != nil {
			return nil, e
		}
		sum := sha256.Sum256(data)
		if hex.EncodeToString(sum[:]) != d.VK.SHA256 || strconv.Itoa(len(data)) != d.VK.Size {
			return nil, fmt.Errorf("ERR_ARTIFACT_HASH")
		}
		v.keys[d.Depth] = data
	}
	if len(v.keys) != 2 {
		return nil, fmt.Errorf("ERR_CRYPTO_LOCK")
	}
	return v, nil
}
func Binding(network, poll string, ballot []byte) (scope, message string) {
	digest := sha256.Sum256(ballot)
	read := func(tag string, values []any) string {
		b, _ := hex.DecodeString(codec.Hash(tag, values))
		return new(big.Int).SetBytes(b[:31]).String()
	}
	return read("vote/scope", []any{network, poll}), read("vote/message", []any{network, poll, hex.EncodeToString(digest[:])})
}

// SDKHash is keccak256(uint256(value)) >> 8, not SHA-256 and not Poseidon.
func SDKHash(value string) (string, error) {
	n, ok := new(big.Int).SetString(value, 10)
	if !ok || n.Sign() < 0 || n.BitLen() > 256 || n.String() != value {
		return "", fmt.Errorf("ERR_PUBLIC_INPUT")
	}
	var padded [32]byte
	n.FillBytes(padded[:])
	h := sha3.NewLegacyKeccak256()
	h.Write(padded[:])
	return new(big.Int).Rsh(new(big.Int).SetBytes(h.Sum(nil)), 8).String(), nil
}
func (v *Verifier) Verify(c Context, ballot []byte, p Membership) error {
	if c.Depth != "16" && c.Depth != "26" || p.Depth != c.Depth {
		return fmt.Errorf("ERR_CIRCUIT_DEPTH")
	}
	if _, e := codec.ChainID(c.NetworkID); e != nil {
		return e
	}
	if len(c.PollID) != 64 || strings.ToLower(c.PollID) != c.PollID {
		return fmt.Errorf("ERR_POLL")
	}
	if _, e := hex.DecodeString(c.PollID); e != nil {
		return fmt.Errorf("ERR_POLL")
	}
	epoch, e := codec.Uint(c.Epoch)
	if e != nil || epoch == 0 {
		return fmt.Errorf("ERR_EPOCH")
	}
	if c.SuiteID != "semaphore-v4.14.5-depth-"+c.Depth {
		return fmt.Errorf("ERR_SUITE")
	}
	if p.Root != c.Root {
		return fmt.Errorf("ERR_GROUP_ROOT")
	}
	for _, s := range []string{p.Root, p.Nullifier} {
		if _, e = eligibility.ParseField(s, false); e != nil {
			return e
		}
	}
	scope, message := Binding(c.NetworkID, c.PollID, ballot)
	if p.Scope != scope || p.Message != message {
		return fmt.Errorf("ERR_PROOF_CONTEXT")
	}
	if len(p.Points) != 8 {
		return fmt.Errorf("ERR_PROOF_POINTS")
	}
	for _, s := range p.Points {
		if s == "" || len(s) > 77 || len(s) > 1 && s[0] == '0' {
			return fmt.Errorf("ERR_PROOF_POINTS")
		}
		for _, r := range s {
			if r < '0' || r > '9' {
				return fmt.Errorf("ERR_PROOF_POINTS")
			}
		}
		n, ok := new(big.Int).SetString(s, 10)
		if !ok || n.Sign() < 0 || n.Cmp(coordinateModulus) >= 0 {
			return fmt.Errorf("ERR_PROOF_POINTS")
		}
	}
	if p.Points[0] == "0" && p.Points[1] == "0" || p.Points[6] == "0" && p.Points[7] == "0" || p.Points[2] == "0" && p.Points[3] == "0" && p.Points[4] == "0" && p.Points[5] == "0" {
		return fmt.Errorf("ERR_PROOF_INFINITY")
	}
	messageHash, e := SDKHash(p.Message)
	if e != nil {
		return e
	}
	scopeHash, e := SDKHash(p.Scope)
	if e != nil {
		return e
	}
	points := p.Points
	proof := types.ZKProof{Proof: &types.ProofData{A: []string{points[0], points[1], "1"}, B: [][]string{{points[3], points[2]}, {points[5], points[4]}, {"1", "0"}}, C: []string{points[6], points[7], "1"}, Protocol: "groth16"}, PubSignals: []string{p.Root, p.Nullifier, messageHash, scopeHash}}
	key, ok := v.keys[p.Depth]
	if !ok {
		return fmt.Errorf("ERR_CIRCUIT_DEPTH")
	}
	if e = verifier.VerifyGroth16(proof, key); e != nil {
		return fmt.Errorf("ERR_MEMBERSHIP_PROOF: %w", e)
	}
	return nil
}
