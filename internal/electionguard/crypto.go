// Public ElectionGuard 2.0 verification. This code handles public operands only;
// encryption, key shares and interactive guardian responses come from the
// pinned SDK oracle, never from a simulated vote or a reconstructed joint key.
package electionguard

import (
	"bytes"
	"civora/internal/codec"
	"civora/internal/ports"
	"civora/spec"
	"crypto/hmac"
	"crypto/sha256"
	"encoding/base64"
	"encoding/binary"
	"encoding/hex"
	"encoding/json"
	"fmt"
	"math/big"
)

const Encoding = "eg-civic-compact-1"

// PinnedModule identifies the verifier and its exact embedded parameter and
// manifest artifacts. Suite authorization is still performed by the registry.
func PinnedModule() (string, []string, error) {
	raw, e := spec.Crypto.ReadFile("crypto/electionguard-v2.lock.json")
	if e != nil {
		return "", nil, e
	}
	var lock struct {
		Suite          string   `json:"suite"`
		Implementation string   `json:"implementation_sha256"`
		Artifacts      []string `json:"artifacts"`
	}
	if e = json.Unmarshal(raw, &lock); e != nil {
		return "", nil, e
	}
	if lock.Suite != Suite || len(lock.Implementation) != 64 || len(lock.Artifacts) != 2 {
		return "", nil, fmt.Errorf("ERR_EG_LOCK")
	}
	for i, name := range []string{"crypto/electionguard-v2.parameters.json", "crypto/electionguard-v2.manifest-template.json"} {
		data, e := spec.Crypto.ReadFile(name)
		if e != nil {
			return "", nil, e
		}
		h := sha256.Sum256(data)
		if hex.EncodeToString(h[:]) != lock.Artifacts[i] {
			return "", nil, fmt.Errorf("ERR_EG_ARTIFACT_PIN")
		}
	}
	return lock.Implementation, lock.Artifacts, nil
}

type Proof struct {
	Challenge string `json:"challenge"`
	Response  string `json:"response"`
}
type Ciphertext struct {
	Alpha string `json:"alpha"`
	Beta  string `json:"beta"`
}
type PublicContext struct {
	Encoding string `json:"encoding"`
	N        string `json:"number_of_guardians"`
	Quorum   string `json:"quorum"`
	HP       string `json:"parameter_base_hash"`
	HM       string `json:"manifest_hash"`
	HB       string `json:"election_base_hash"`
	HE       string `json:"extended_base_hash"`
	Key      string `json:"joint_public_key"`
}
type Coefficient struct {
	PublicKey string `json:"public_key"`
	Challenge string `json:"challenge"`
	Response  string `json:"response"`
}
type Guardian struct {
	AccountID string        `json:"account_id"`
	Sequence  string        `json:"sequence_order"`
	Proofs    []Coefficient `json:"coefficient_proofs"`
}
type Selection struct {
	ID       string     `json:"selection_id"`
	Sequence string     `json:"sequence_order"`
	Cipher   Ciphertext `json:"encrypted_vote"`
	Proof    []Proof    `json:"proof"`
}
type ContestData struct {
	C0       string `json:"c0"`
	C1       string `json:"c1"`
	C2       string `json:"c2"`
	NumBytes string `json:"num_bytes"`
}
type Contest struct {
	ID         string      `json:"contest_id"`
	Sequence   string      `json:"sequence_order"`
	Allowed    string      `json:"votes_allowed"`
	Hash       string      `json:"contest_hash"`
	Selections []Selection `json:"selections"`
	Proof      []Proof     `json:"proof"`
	Data       ContestData `json:"encrypted_contest_data"`
}
type EncryptedBallot struct {
	Encoding   string    `json:"encoding"`
	ID         string    `json:"ballot_id"`
	Style      string    `json:"ballot_style_id"`
	Device     string    `json:"encrypting_device"`
	Timestamp  string    `json:"timestamp"`
	Baux       string    `json:"code_baux"`
	Code       string    `json:"confirmation_code"`
	ElectionID string    `json:"election_id"`
	State      string    `json:"state"`
	Contests   []Contest `json:"contests"`
}
type Share struct {
	Index    string `json:"guardian_index"`
	M        string `json:"share"`
	A        string `json:"a"`
	B        string `json:"b"`
	Response string `json:"response"`
}
type Decryption struct {
	Encoding   string     `json:"encoding"`
	ID         string     `json:"id"`
	ElectionID string     `json:"election_id"`
	Cipher     Ciphertext `json:"ciphertext"`
	Tally      string     `json:"tally"`
	BOverM     string     `json:"b_over_m"`
	Proof      Proof      `json:"proof"`
	Shares     []Share    `json:"shares"`
}
type Verifier struct {
	p, q, g *big.Int
	hp      []byte
}
type Election struct {
	NetworkID, PollID, Epoch string
	Context                  PublicContext
	Guardians                []Guardian
	key                      *big.Int
	he                       []byte
}

func NewVerifier() (*Verifier, error) {
	if _, _, e := PinnedModule(); e != nil {
		return nil, e
	}
	raw, err := spec.Crypto.ReadFile("crypto/electionguard-v2.parameters.json")
	if err != nil {
		return nil, err
	}
	var params struct {
		P, Q, G string
		HP      string `json:"parameter_base_hash"`
	}
	if err = json.Unmarshal(raw, &params); err != nil {
		return nil, err
	}
	pb, e := codec.Blob(params.P)
	if e != nil || len(pb) != 512 {
		return nil, fmt.Errorf("ERR_EG_PARAMETERS")
	}
	qb, e := codec.Blob(params.Q)
	if e != nil || len(qb) != 32 {
		return nil, fmt.Errorf("ERR_EG_PARAMETERS")
	}
	gb, e := codec.Blob(params.G)
	if e != nil || len(gb) != 512 {
		return nil, fmt.Errorf("ERR_EG_PARAMETERS")
	}
	v := &Verifier{p: new(big.Int).SetBytes(pb), q: new(big.Int).SetBytes(qb), g: new(big.Int).SetBytes(gb)}
	version := make([]byte, 32)
	copy(version, []byte("v2.0.0"))
	v.hp = egHash(version, 0, pb, qb, gb)
	if blob(v.hp) != params.HP || v.p.BitLen() != 4096 || v.q.BitLen() != 256 || v.exp(v.g, v.q).Cmp(big.NewInt(1)) != 0 {
		return nil, fmt.Errorf("ERR_EG_PARAMETERS")
	}
	return v, nil
}
func blob(b []byte) string                     { return base64.RawURLEncoding.EncodeToString(b) }
func fixed(n *big.Int, size int) []byte        { return n.FillBytes(make([]byte, size)) }
func (v *Verifier) exp(a, b *big.Int) *big.Int { return new(big.Int).Exp(a, b, v.p) }
func (v *Verifier) mul(a, b *big.Int) *big.Int { return new(big.Int).Mod(new(big.Int).Mul(a, b), v.p) }
func (v *Verifier) modq(a *big.Int) *big.Int   { return new(big.Int).Mod(a, v.q) }
func egHash(key []byte, tag byte, parts ...[]byte) []byte {
	h := hmac.New(sha256.New, key)
	h.Write([]byte{tag})
	for _, p := range parts {
		h.Write(p)
	}
	return h.Sum(nil)
}
func index(n uint32) []byte { b := make([]byte, 4); binary.BigEndian.PutUint32(b, n); return b }
func (v *Verifier) scalar(s string) (*big.Int, error) {
	b, e := codec.Blob(s)
	if e != nil || len(b) != 32 {
		return nil, fmt.Errorf("ERR_EG_SCALAR")
	}
	n := new(big.Int).SetBytes(b)
	if n.Cmp(v.q) >= 0 {
		return nil, fmt.Errorf("ERR_EG_SCALAR")
	}
	return n, nil
}
func hashBytes(s string) ([]byte, error) {
	b, e := codec.Blob(s)
	if e != nil || len(b) != 32 {
		return nil, fmt.Errorf("ERR_EG_HASH")
	}
	return b, nil
}
func (v *Verifier) group(s string, identity bool) (*big.Int, error) {
	b, e := codec.Blob(s)
	if e != nil || len(b) != 512 {
		return nil, fmt.Errorf("ERR_EG_GROUP")
	}
	n := new(big.Int).SetBytes(b)
	if n.Sign() == 0 || n.Cmp(v.p) >= 0 || !identity && n.Cmp(big.NewInt(1)) == 0 || v.exp(n, v.q).Cmp(big.NewInt(1)) != 0 {
		return nil, fmt.Errorf("ERR_EG_GROUP")
	}
	return n, nil
}
func (v *Verifier) pair(c Ciphertext, identity bool) (*big.Int, *big.Int, error) {
	a, e := v.group(c.Alpha, identity)
	if e != nil {
		return nil, nil, e
	}
	b, e := v.group(c.Beta, true)
	return a, b, e
}

func (v *Verifier) Ceremony(c ports.PollContext, raw []byte) (*Election, error) {
	if epoch, e := codec.Uint(c.Epoch); e != nil || epoch == 0 {
		return nil, fmt.Errorf("ERR_EG_EPOCH")
	}
	d, e := DecodeBytes(raw, CeremonyKind())
	if e != nil {
		return nil, e
	}
	if e = d.CheckLink(c.NetworkID, c.PollID, c.ProposalID); e != nil {
		return nil, e
	}
	if len(c.GuardianIDs) != 5 {
		return nil, fmt.Errorf("ERR_EG_GUARDIANS")
	}
	template, e := spec.Crypto.ReadFile("crypto/electionguard-v2.manifest-template.json")
	if e != nil {
		return nil, e
	}
	manifest := bytes.ReplaceAll(template, []byte("POLL_ID"), []byte(c.PollID))
	if !bytes.Equal(d.Files["manifest.json"], manifest) {
		return nil, fmt.Errorf("ERR_EG_MANIFEST")
	}
	var ctx PublicContext
	if e = codec.Decode(d.Files["context.json"], "eg_context", &ctx); e != nil {
		return nil, e
	}
	hm := egHash(v.hp, 1, index(uint32(len(manifest))), manifest)
	hb := egHash(v.hp, 2, hm, index(5), index(3))
	if ctx.HP != blob(v.hp) || ctx.HM != blob(hm) || ctx.HB != blob(hb) {
		return nil, fmt.Errorf("ERR_EG_CONTEXT")
	}
	out := &Election{Context: ctx, Guardians: []Guardian{}, key: big.NewInt(1), NetworkID: c.NetworkID, PollID: c.PollID, Epoch: c.Epoch}
	for i := 0; i < 5; i++ {
		var guardian Guardian
		if e = codec.Decode(d.Files[fmt.Sprintf("guardians/%02d.json", i+1)], "eg_guardian", &guardian); e != nil {
			return nil, e
		}
		if guardian.AccountID != c.GuardianIDs[i] || i > 0 && c.GuardianIDs[i] <= c.GuardianIDs[i-1] || guardian.Sequence != fmt.Sprint(i+1) {
			return nil, fmt.Errorf("ERR_EG_GUARDIANS")
		}
		for j, p := range guardian.Proofs {
			k, e := v.group(p.PublicKey, false)
			if e != nil {
				return nil, e
			}
			challenge, e := v.scalar(p.Challenge)
			if e != nil {
				return nil, e
			}
			r, e := v.scalar(p.Response)
			if e != nil {
				return nil, e
			}
			h := v.mul(v.exp(v.g, r), v.exp(k, challenge))
			want := v.modq(new(big.Int).SetBytes(egHash(v.hp, 0x10, index(uint32(i+1)), index(uint32(j)), fixed(k, 512), fixed(h, 512))))
			if challenge.Cmp(want) != 0 {
				return nil, fmt.Errorf("ERR_EG_GUARDIAN_PROOF")
			}
			if j == 0 {
				out.key = v.mul(out.key, k)
			}
		}
		out.Guardians = append(out.Guardians, guardian)
	}
	out.he = egHash(hb, 0x12, fixed(out.key, 512))
	if ctx.Key != blob(fixed(out.key, 512)) || ctx.HE != blob(out.he) || out.key.Cmp(big.NewInt(1)) == 0 {
		return nil, fmt.Errorf("ERR_EG_CONTEXT")
	}
	return out, nil
}

// Avoid a name collision between the public ceremony record and envelope kind.
func CeremonyKind() Kind { return "ceremony" }

func (v *Verifier) rangeProof(c *Election, a, b *big.Int, proofs []Proof) error {
	if len(proofs) != 2 {
		return fmt.Errorf("ERR_EG_RANGE")
	}
	parts := [][]byte{fixed(c.key, 512), fixed(a, 512), fixed(b, 512)}
	sum := big.NewInt(0)
	for j, p := range proofs {
		challenge, e := v.scalar(p.Challenge)
		if e != nil {
			return e
		}
		r, e := v.scalar(p.Response)
		if e != nil {
			return e
		}
		aj := v.mul(v.exp(v.g, r), v.exp(a, challenge))
		w := v.modq(new(big.Int).Sub(r, new(big.Int).Mul(big.NewInt(int64(j)), challenge)))
		bj := v.mul(v.exp(c.key, w), v.exp(b, challenge))
		parts = append(parts, fixed(aj, 512), fixed(bj, 512))
		sum.Add(sum, challenge)
	}
	want := v.modq(new(big.Int).SetBytes(egHash(c.he, 0x21, parts...)))
	if v.modq(sum).Cmp(want) != 0 {
		return fmt.Errorf("ERR_EG_RANGE")
	}
	return nil
}

func (v *Verifier) Ballot(c *Election, poll string, raw []byte) (*EncryptedBallot, error) {
	if len(raw) > MaxFileBytes || poll != c.PollID {
		return nil, fmt.Errorf("ERR_EG_BALLOT_CONTEXT")
	}
	var b EncryptedBallot
	if e := codec.Decode(raw, "eg_ballot", &b); e != nil {
		return nil, e
	}
	if b.Device != "civic:"+poll || b.ElectionID != c.Context.HE {
		return nil, fmt.Errorf("ERR_EG_BALLOT_CONTEXT")
	}
	contest := b.Contests[0]
	s := contest.Selections[0]
	a, beta, e := v.pair(s.Cipher, false)
	if e != nil {
		return nil, e
	}
	if e = v.rangeProof(c, a, beta, s.Proof); e != nil {
		return nil, e
	}
	if e = v.rangeProof(c, a, beta, contest.Proof); e != nil {
		return nil, e
	}
	ch := egHash(c.he, 0x23, index(0), fixed(c.key, 512), fixed(a, 512), fixed(beta, 512))
	code := egHash(c.he, 0x24, ch)
	if contest.Hash != blob(ch) || b.Code != blob(code) {
		return nil, fmt.Errorf("ERR_EG_CONFIRMATION")
	}
	if _, e = v.group(contest.Data.C0, false); e != nil {
		return nil, e
	}
	if _, e = hashBytes(contest.Data.C2); e != nil {
		return nil, e
	}
	data, e := codec.Blob(contest.Data.C1)
	if e != nil {
		return nil, e
	}
	// Fixed padded contest data size, qualified against both SDK choices.
	if len(data) != 64 || contest.Data.NumBytes != "62" {
		return nil, fmt.Errorf("ERR_EG_CONTEST_DATA")
	}
	return &b, nil
}

func (v *Verifier) Zero() string {
	return string(codec.Must(Ciphertext{blob(fixed(big.NewInt(1), 512)), blob(fixed(big.NewInt(1), 512))}))
}
func (v *Verifier) Add(aggregate string, ballot *EncryptedBallot) (string, error) {
	var current Ciphertext
	if e := codec.Decode([]byte(aggregate), "eg_ciphertext", &current); e != nil {
		return "", e
	}
	a, b, e := v.pair(current, true)
	if e != nil {
		return "", e
	}
	x, y, e := v.pair(ballot.Contests[0].Selections[0].Cipher, false)
	if e != nil {
		return "", e
	}
	return string(codec.Must(Ciphertext{blob(fixed(v.mul(a, x), 512)), blob(fixed(v.mul(b, y), 512))})), nil
}

func (v *Verifier) Tally(c *Election, poll, aggregate, count string, raw []byte) (string, string, error) {
	if len(raw) > MaxFileBytes || poll != c.PollID {
		return "", "", fmt.Errorf("ERR_EG_TALLY_CONTEXT")
	}
	var t Decryption
	if e := codec.Decode(raw, "eg_tally", &t); e != nil {
		return "", "", e
	}
	if t.ID != poll || t.ElectionID != c.Context.HE || string(codec.Must(t.Cipher)) != aggregate {
		return "", "", fmt.Errorf("ERR_EG_TALLY_CONTEXT")
	}
	y, e := codec.Uint(t.Tally)
	if e != nil {
		return "", "", e
	}
	n, e := codec.Uint(count)
	if e != nil || y > n {
		return "", "", fmt.Errorf("ERR_EG_TALLY_COUNT")
	}
	a, b, e := v.pair(t.Cipher, true)
	if e != nil {
		return "", "", e
	}
	bom, e := v.group(t.BOverM, true)
	if e != nil {
		return "", "", e
	}
	if v.exp(c.key, new(big.Int).SetUint64(y)).Cmp(bom) != 0 {
		return "", "", fmt.Errorf("ERR_EG_TALLY_COUNT")
	}
	m := v.mul(b, new(big.Int).ModInverse(bom, v.p))
	challenge, e := v.scalar(t.Proof.Challenge)
	if e != nil {
		return "", "", e
	}
	r, e := v.scalar(t.Proof.Response)
	if e != nil {
		return "", "", e
	}
	ga := v.mul(v.exp(v.g, r), v.exp(c.key, challenge))
	gb := v.mul(v.exp(a, r), v.exp(m, challenge))
	want := v.modq(new(big.Int).SetBytes(egHash(c.he, 0x30, fixed(c.key, 512), fixed(a, 512), fixed(b, 512), fixed(ga, 512), fixed(gb, 512), fixed(m, 512))))
	if want.Cmp(challenge) != 0 {
		return "", "", fmt.Errorf("ERR_EG_DECRYPTION_PROOF")
	}
	prodM, prodA, prodB, sum := big.NewInt(1), big.NewInt(1), big.NewInt(1), big.NewInt(0)
	last := uint64(0)
	for _, share := range t.Shares {
		i, e := codec.Uint(share.Index)
		if e != nil || i <= last || i > 5 {
			return "", "", fmt.Errorf("ERR_EG_QUORUM")
		}
		last = i
		weight := big.NewInt(1)
		for _, other := range t.Shares {
			j, _ := codec.Uint(other.Index)
			if j == i {
				continue
			}
			den := v.modq(new(big.Int).Sub(new(big.Int).SetUint64(j), new(big.Int).SetUint64(i)))
			inv := new(big.Int).ModInverse(den, v.q)
			if inv == nil {
				return "", "", fmt.Errorf("ERR_EG_QUORUM")
			}
			weight = v.modq(new(big.Int).Mul(weight, new(big.Int).Mul(new(big.Int).SetUint64(j), inv)))
		}
		verificationKey := big.NewInt(1)
		for _, guardian := range c.Guardians {
			power := big.NewInt(1)
			for _, coeff := range guardian.Proofs {
				k, _ := v.group(coeff.PublicKey, false)
				verificationKey = v.mul(verificationKey, v.exp(k, power))
				power.Mul(power, new(big.Int).SetUint64(i))
			}
		}
		mi, e := v.group(share.M, true)
		if e != nil {
			return "", "", e
		}
		ai, e := v.group(share.A, true)
		if e != nil {
			return "", "", e
		}
		bi, e := v.group(share.B, true)
		if e != nil {
			return "", "", e
		}
		ri, e := v.scalar(share.Response)
		if e != nil {
			return "", "", e
		}
		ci := v.modq(new(big.Int).Mul(challenge, weight))
		if v.mul(v.exp(v.g, ri), v.exp(verificationKey, ci)).Cmp(ai) != 0 || v.mul(v.exp(a, ri), v.exp(mi, ci)).Cmp(bi) != 0 {
			return "", "", fmt.Errorf("ERR_EG_SHARE_PROOF")
		}
		prodM = v.mul(prodM, v.exp(mi, weight))
		prodA = v.mul(prodA, ai)
		prodB = v.mul(prodB, bi)
		sum.Add(sum, ri)
	}
	if prodM.Cmp(m) != 0 || prodA.Cmp(ga) != 0 || prodB.Cmp(gb) != 0 || v.modq(sum).Cmp(r) != 0 {
		return "", "", fmt.Errorf("ERR_EG_SHARE_AGGREGATE")
	}
	return fmt.Sprint(y), fmt.Sprint(n - y), nil
}

// Adapter is pure. Its full public ceremony is supplied by committed state on
// every call; there is no external-process, file, clock or mutable-key cache.
type Adapter struct{ v *Verifier }

var _ ports.VotingCrypto = (*Adapter)(nil)

func NewAdapter() (*Adapter, error) { v, e := NewVerifier(); return &Adapter{v}, e }
func (a *Adapter) Configure(c ports.PollContext, raw []byte) (string, error) {
	if _, e := a.v.Ceremony(c, raw); e != nil {
		return "", e
	}
	return a.v.Zero(), nil
}
func (a *Adapter) context(c ports.PollContext) (*Election, error) {
	raw, e := codec.Blob(c.CeremonyBundle)
	if e != nil {
		return nil, e
	}
	digest := sha256.Sum256(raw)
	if hex.EncodeToString(digest[:]) != c.CeremonyDigest {
		return nil, fmt.Errorf("ERR_EG_CEREMONY_DIGEST")
	}
	return a.v.Ceremony(c, raw)
}
func (a *Adapter) Accept(c ports.PollContext, aggregate string, raw []byte) (ports.AcceptedBallot, error) {
	var req struct {
		PollID     string `json:"poll_id"`
		Ballot     string `json:"encrypted_ballot"`
		Membership struct {
			Nullifier string `json:"nullifier"`
		} `json:"membership"`
	}
	if e := json.Unmarshal(raw, &req); e != nil {
		return ports.AcceptedBallot{}, e
	}
	ceremony, e := a.context(c)
	if e != nil {
		return ports.AcceptedBallot{}, e
	}
	d, e := Decode(req.Ballot, Ballot)
	if e != nil {
		return ports.AcceptedBallot{}, e
	}
	b, e := a.v.Ballot(ceremony, c.PollID, d.Files["ballot.json"])
	if e != nil {
		return ports.AcceptedBallot{}, e
	}
	next, e := a.v.Add(aggregate, b)
	if e != nil {
		return ports.AcceptedBallot{}, e
	}
	code, _ := hashBytes(b.Code)
	return ports.AcceptedBallot{Nullifier: req.Membership.Nullifier, BallotID: b.ID, CiphertextDigest: codec.Hash("vote/ciphertext", b.Contests[0].Selections[0].Cipher), ConfirmationCode: hex.EncodeToString(code), Aggregate: next}, nil
}
func (a *Adapter) Tally(c ports.PollContext, aggregate, count string, raw []byte) (ports.TallyResult, error) {
	var req struct {
		Record string `json:"decryption_record"`
	}
	if e := json.Unmarshal(raw, &req); e != nil {
		return ports.TallyResult{}, e
	}
	ceremony, e := a.context(c)
	if e != nil {
		return ports.TallyResult{}, e
	}
	d, e := Decode(req.Record, Tally)
	if e != nil {
		return ports.TallyResult{}, e
	}
	yes, no, e := a.v.Tally(ceremony, c.PollID, aggregate, count, d.Files["tally.json"])
	return ports.TallyResult{Yes: yes, No: no}, e
}
