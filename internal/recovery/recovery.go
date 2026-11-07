// Package recovery verifies independently pinned, off-chain safety orders.
// It cannot produce a block, activate a protocol, import state or cast a vote.
package recovery

import (
	"civora/internal/codec"
	"crypto/sha3"
	"encoding/base64"
	"encoding/hex"
	"fmt"
	"strconv"

	"github.com/cloudflare/circl/sign/mldsa/mldsa65"
)

const MaxHoldMS uint64 = 72 * 60 * 60 * 1000
const FreshnessMS uint64 = 10 * 60 * 1000

type Member struct {
	ID        string `json:"member_id"`
	College   string `json:"college,omitempty"`
	PublicKey string `json:"public_key"`
}
type Root struct {
	V         string   `json:"v"`
	Network   string   `json:"network_id"`
	Epoch     string   `json:"recovery_epoch"`
	Mode      string   `json:"mode"`
	NotBefore string   `json:"not_before_ms"`
	Expires   string   `json:"expires_at_ms"`
	Members   []Member `json:"members"`
	Mirrors   []Member `json:"mirrors"`
}
type Order struct {
	V         string `json:"v"`
	Network   string `json:"network_id"`
	Epoch     string `json:"recovery_epoch"`
	Counter   string `json:"counter"`
	Type      string `json:"type"`
	ScopeKind string `json:"scope_kind"`
	ScopeID   string `json:"scope_id"`
	Reference string `json:"reference_sha3_384"`
	Point     string `json:"reference_point"`
	Reason    string `json:"public_reason"`
	Starts    string `json:"starts_at_ms"`
	Expires   string `json:"expires_at_ms"`
}
type Signature struct {
	MemberID  string `json:"member_id"`
	Signature string `json:"signature"`
}
type Statement struct {
	Network  string `json:"network_id"`
	Epoch    string `json:"recovery_epoch"`
	Counter  string `json:"counter"`
	Root     string `json:"root_sha3_384"`
	Order    string `json:"order_sha3_384"`
	Observed string `json:"observed_at_ms"`
	Expires  string `json:"expires_at_ms"`
}
type Mirror struct {
	MemberID  string    `json:"member_id"`
	Statement Statement `json:"statement"`
	Signature string    `json:"signature"`
}
type Bundle struct {
	Order      Order       `json:"order"`
	Signatures []Signature `json:"signatures"`
	Mirrors    []Mirror    `json:"mirrors"`
}
type Rotation struct {
	Next     Root        `json:"next_root"`
	Approval string      `json:"approval_sha3_384"`
	Old      []Signature `json:"old_signatures"`
	New      []Signature `json:"new_signatures"`
}

func HashBytes(b []byte) string { h := sha3.Sum384(b); return hex.EncodeToString(h[:]) }
func RootID(r Root) string      { return HashBytes(codec.Must(r)) }
func Digest(domain string, v any) []byte {
	h := sha3.New384()
	h.Write([]byte(domain))
	h.Write(codec.Must(v))
	return h.Sum(nil)
}
func OrderDigest(o Order) []byte { return Digest("CIVIC/RECOVERY/1/"+o.Type+"\n", o) }
func DecodeRoot(raw []byte, pin string, now uint64, allowTest bool) (Root, error) {
	var r Root
	if len(raw) > 256*1024 || HashBytes(raw) != pin {
		return r, fmt.Errorf("ERR_RECOVERY_ROOT_PIN")
	}
	if err := codec.Decode(raw, "recovery_root", &r); err != nil {
		return r, err
	}
	return r, r.Validate(now, allowTest)
}
func validate(name string, v any) error {
	raw, err := codec.Canonical(v)
	if err != nil {
		return err
	}
	value, err := codec.Parse(raw)
	if err != nil {
		return err
	}
	return codec.Validate(name, value)
}
func (r Root) Validate(now uint64, allowTest bool) error {
	if err := validate("recovery_root", r); err != nil {
		return err
	}
	epoch, _ := codec.Uint(r.Epoch)
	start, _ := codec.Uint(r.NotBefore)
	end, _ := codec.Uint(r.Expires)
	if epoch == 0 || now < start || now >= end || start >= end || (!allowTest && r.Mode != "operator") {
		return fmt.Errorf("ERR_RECOVERY_ROOT_VALIDITY")
	}
	ids, keys := map[string]bool{}, map[string]bool{}
	counts := map[string]int{}
	all := append(append([]Member{}, r.Members...), r.Mirrors...)
	for _, m := range all {
		if ids[m.ID] || keys[m.PublicKey] {
			return fmt.Errorf("ERR_RECOVERY_KEY_SEPARATION")
		}
		ids[m.ID] = true
		keys[m.PublicKey] = true
		if _, err := decode(m.PublicKey, mldsa65.PublicKeySize); err != nil {
			return err
		}
	}
	for _, m := range r.Members {
		counts[m.College]++
	}
	if counts["technical"] != 7 || counts["civic"] != 7 {
		return fmt.Errorf("ERR_RECOVERY_COLLEGES")
	}
	return nil
}
func decode(s string, n int) ([]byte, error) {
	b, e := base64.RawURLEncoding.DecodeString(s)
	if e != nil || len(b) != n || base64.RawURLEncoding.EncodeToString(b) != s {
		return nil, fmt.Errorf("ERR_RECOVERY_ENCODING")
	}
	return b, nil
}
func verify(m Member, s string, digest []byte) bool {
	pk, e := decode(m.PublicKey, mldsa65.PublicKeySize)
	if e != nil {
		return false
	}
	signature, e := decode(s, mldsa65.SignatureSize)
	if e != nil {
		return false
	}
	var key mldsa65.PublicKey
	return key.UnmarshalBinary(pk) == nil && mldsa65.Verify(&key, digest, nil, signature)
}
func quorum(r Root, sigs []Signature, digest []byte) error {
	seen := map[string]bool{}
	counts := map[string]int{}
	for _, s := range sigs {
		if seen[s.MemberID] {
			return fmt.Errorf("ERR_RECOVERY_DUPLICATE")
		}
		seen[s.MemberID] = true
		found := false
		for _, m := range r.Members {
			if m.ID == s.MemberID {
				found = true
				if !verify(m, s.Signature, digest) {
					return fmt.Errorf("ERR_RECOVERY_SIGNATURE")
				}
				counts[m.College]++
				break
			}
		}
		if !found {
			return fmt.Errorf("ERR_RECOVERY_SIGNER")
		}
	}
	if counts["technical"] < 5 || counts["civic"] < 5 {
		return fmt.Errorf("ERR_RECOVERY_QUORUM")
	}
	return nil
}
func verifyOrder(r Root, b Bundle, now, last uint64, allowTest bool) error {
	if err := r.Validate(now, allowTest); err != nil {
		return err
	}
	if err := validate("recovery_bundle", b); err != nil {
		return err
	}
	o := b.Order
	count, _ := codec.Uint(o.Counter)
	start, _ := codec.Uint(o.Starts)
	end, _ := codec.Uint(o.Expires)
	rootEnd, _ := codec.Uint(r.Expires)
	if o.Network != r.Network || o.Epoch != r.Epoch {
		return fmt.Errorf("ERR_RECOVERY_CONTEXT")
	}
	if count <= last {
		return fmt.Errorf("ERR_RECOVERY_REPLAY")
	}
	if start > now || now >= end || end <= start || end-start > MaxHoldMS || end > rootEnd {
		return fmt.Errorf("ERR_RECOVERY_WINDOW")
	}
	switch o.Type {
	case "security.hold":
		if o.ScopeKind == "network" {
			return fmt.Errorf("ERR_RECOVERY_SCOPE")
		}
	case "security.arm_reserve":
		if o.ScopeKind != "module" {
			return fmt.Errorf("ERR_RECOVERY_SCOPE")
		}
	case "recovery.publish":
		if o.ScopeKind != "network" || o.ScopeID == o.Network || len(o.ScopeID) != 64 {
			return fmt.Errorf("ERR_RECOVERY_NEW_NETWORK")
		}
		if raw, err := hex.DecodeString(o.ScopeID); err != nil || hex.EncodeToString(raw) != o.ScopeID {
			return fmt.Errorf("ERR_RECOVERY_NEW_NETWORK")
		}
	}
	d := OrderDigest(o)
	if err := quorum(r, b.Signatures, d); err != nil {
		return err
	}
	return nil
}

// VerifyOnChain uses authenticated block time and genesis configuration. Mirror
// observations belong only to the independent, off-chain freshness path.
func VerifyOnChain(r Root, b Bundle, now, last uint64, allowTest bool) error {
	if len(b.Mirrors) != 0 {
		return fmt.Errorf("ERR_RECOVERY_ONCHAIN_MIRRORS")
	}
	return verifyOrder(r, b, now, last, allowTest)
}
func Verify(r Root, b Bundle, now, last uint64, allowTest bool) error {
	if err := verifyOrder(r, b, now, last, allowTest); err != nil {
		return err
	}
	o := b.Order
	d := OrderDigest(o)
	seen := map[string]bool{}
	agreed := 0
	for _, m := range b.Mirrors {
		if seen[m.MemberID] {
			return fmt.Errorf("ERR_RECOVERY_DUPLICATE")
		}
		seen[m.MemberID] = true
		s := m.Statement
		observed, _ := codec.Uint(s.Observed)
		expires, _ := codec.Uint(s.Expires)
		if s.Network != r.Network || s.Epoch != r.Epoch || s.Counter != o.Counter || s.Root != RootID(r) || s.Order != hex.EncodeToString(d) || observed > now || now >= expires || expires <= observed || expires-observed > FreshnessMS || now-observed > FreshnessMS {
			return fmt.Errorf("ERR_RECOVERY_FRESHNESS")
		}
		found := false
		for _, key := range r.Mirrors {
			if key.ID == m.MemberID {
				found = verify(key, m.Signature, Digest("CIVIC/RECOVERY/MIRROR/1\n", s))
				break
			}
		}
		if !found {
			return fmt.Errorf("ERR_RECOVERY_MIRROR")
		}
		agreed++
	}
	if agreed < 3 {
		return fmt.Errorf("ERR_RECOVERY_MIRRORS")
	}
	return nil
}

// ApprovalVerifier must authenticate an actual constitutional approval. An
// approval hash or signature of the recovery colleges alone is insufficient.
type ApprovalVerifier interface {
	VerifyRecoveryRootApproval(network, oldRoot, nextRoot, approval string) error
}

func VerifyRotation(old Root, rotation Rotation, now uint64, allowTest bool, approval ApprovalVerifier) error {
	if err := validate("recovery_rotation", rotation); err != nil {
		return err
	}
	if err := old.Validate(now, allowTest); err != nil {
		return err
	}
	if err := rotation.Next.Validate(now, allowTest); err != nil {
		return err
	}
	a, _ := codec.Uint(old.Epoch)
	b, _ := codec.Uint(rotation.Next.Epoch)
	if a == ^uint64(0) || b != a+1 || rotation.Next.Network != old.Network || rotation.Next.Mode != old.Mode {
		return fmt.Errorf("ERR_RECOVERY_ROTATION_CONTEXT")
	}
	if approval == nil {
		return fmt.Errorf("ERR_RECOVERY_CONSTITUTIONAL_APPROVAL_REQUIRED")
	}
	if err := approval.VerifyRecoveryRootApproval(old.Network, RootID(old), RootID(rotation.Next), rotation.Approval); err != nil {
		return err
	}
	body := struct {
		Old      string `json:"old_root_sha3_384"`
		Next     Root   `json:"next_root"`
		Approval string `json:"approval_sha3_384"`
	}{RootID(old), rotation.Next, rotation.Approval}
	d := Digest("CIVIC/RECOVERY/ROOT/1\n", body)
	if err := quorum(old, rotation.Old, d); err != nil {
		return err
	}
	return quorum(rotation.Next, rotation.New, d)
}
func Number(n uint64) string { return strconv.FormatUint(n, 10) }
