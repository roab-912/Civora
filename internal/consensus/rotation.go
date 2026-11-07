package consensus

import (
	"civora/internal/codec"
	"civora/internal/core"
	"civora/internal/governance"
	"civora/internal/replica"
	"civora/internal/state"
	"encoding/hex"
	"fmt"
	abci "github.com/cometbft/cometbft/abci/types"
	"sort"
)

// Rotation is preauthorized by development genesis, never by validator votes.
// A validator update returned at H takes effect in CometBFT at H+2.
type Rotation struct {
	Height        string                `json:"height"`
	Keys          []string              `json:"keys"`
	Guardians     []governance.Guardian `json:"guardians"`
	Authorization string                `json:"authorization"`
}
type Scheduled struct {
	Base      replica.Machine
	Initial   Trust
	Rotations []Rotation
}

func WithRotations(base replica.Machine, initial Trust, rotations []Rotation) (replica.Machine, error) {
	if len(rotations) == 0 {
		return base, nil
	}
	if _, e := initial.Set(); e != nil {
		return nil, e
	}
	if _, ok := base.(interface{ Entries() []state.Entry }); !ok {
		return nil, fmt.Errorf("rotation state port unavailable")
	}
	previous := uint64(0)
	for _, r := range rotations {
		if e := codec.Decode(codec.Must(r), "consensus_rotation", new(Rotation)); e != nil {
			return nil, e
		}
		h, e := codec.Uint(r.Height)
		if e != nil || h == 0 || h > ^uint64(0)-2 || previous > 0 && h <= previous+2 {
			return nil, fmt.Errorf("rotation heights overlap")
		}
		previous = h
		if _, e = (Trust{ChainID: initial.ChainID, Keys: r.Keys}).Set(); e != nil {
			return nil, e
		}
		ids, keys := map[string]bool{}, map[string]bool{}
		for _, g := range r.Guardians {
			if ids[g.AccountID] || keys[g.SigningPK] {
				return nil, fmt.Errorf("duplicate guardian")
			}
			ids[g.AccountID] = true
			keys[g.SigningPK] = true
			for _, k := range r.Keys {
				if k == g.SigningPK {
					return nil, fmt.Errorf("guardian/validator key overlap")
				}
			}
		}
	}
	return &Scheduled{base, initial, rotations}, nil
}
func (s *Scheduled) Height() uint64        { return s.Base.Height() }
func (s *Scheduled) Time() uint64          { return s.Base.Time() }
func (s *Scheduled) Active() bool          { return s.Base.Active() }
func (s *Scheduled) Check(tx []byte) error { return s.Base.Check(tx) }
func (s *Scheduled) TransactionPriority(raw []byte) int {
	if p, ok := s.Base.(interface{ TransactionPriority([]byte) int }); ok {
		return p.TransactionPriority(raw)
	}
	return 1
}
func (s *Scheduled) Events() []abci.Event {
	if p, ok := s.Base.(interface{ Events() []abci.Event }); ok {
		return p.Events()
	}
	return nil
}
func (s *Scheduled) Entries() []state.Entry {
	entries := append([]state.Entry{}, s.Base.(interface{ Entries() []state.Entry }).Entries()...)
	entries = append(entries, state.Entry{Table: "consensus_schedule", Key: "development", Value: map[string]any{"initial": s.Initial, "rotations": s.Rotations}})
	sort.Slice(entries, func(i, j int) bool {
		return string(codec.Must([]any{entries[i].Table, entries[i].Key})) < string(codec.Must([]any{entries[j].Table, entries[j].Key}))
	})
	return entries
}
func (s *Scheduled) State() []byte {
	return codec.Must(core.State{Height: fmt.Sprint(s.Height()), TimeMS: fmt.Sprint(s.Time()), Entries: s.Entries()})
}
func (s *Scheduled) Root() string { return state.Root(s.Entries()) }
func (s *Scheduled) Apply(b replica.Block) (replica.Machine, error) {
	height, e := codec.Uint(b.Height)
	if e != nil {
		return nil, e
	}
	barrier := false
	for _, r := range s.Rotations {
		h, _ := codec.Uint(r.Height)
		if height >= h && height <= h+2 {
			barrier = true
		}
	}
	if barrier && s.Base.Active() {
		return nil, fmt.Errorf("ERR_ROTATION_ACTIVE_POLL")
	}
	next, e := s.Base.Apply(b)
	if e != nil {
		return nil, e
	}
	if barrier && next.Active() {
		return nil, fmt.Errorf("ERR_ROTATION_ACTIVE_POLL")
	}
	for _, r := range s.Rotations {
		if r.Height != b.Height || len(r.Guardians) == 0 {
			continue
		}
		port, ok := next.(interface {
			RotateGuardians([]governance.Guardian) (replica.Machine, error)
		})
		if !ok {
			return nil, fmt.Errorf("guardian rotation unsupported")
		}
		next, e = port.RotateGuardians(r.Guardians)
		if e != nil {
			return nil, e
		}
	}
	return &Scheduled{next, s.Initial, s.Rotations}, nil
}
func (s *Scheduled) ValidatorUpdates() []abci.ValidatorUpdate {
	old := s.Initial.Keys
	for _, r := range s.Rotations {
		h, _ := codec.Uint(r.Height)
		if h < s.Height() {
			old = r.Keys
			continue
		}
		if h != s.Height() {
			break
		}
		newKeys := map[string]bool{}
		for _, k := range r.Keys {
			newKeys[k] = true
		}
		out := []abci.ValidatorUpdate{}
		for _, k := range old {
			if !newKeys[k] {
				raw, _ := hex.DecodeString(k)
				out = append(out, abci.UpdateValidator(raw, 0, "ed25519"))
			}
		}
		for _, k := range r.Keys {
			raw, _ := hex.DecodeString(k)
			out = append(out, abci.UpdateValidator(raw, 1, "ed25519"))
		}
		return out
	}
	return nil
}
func (s *Scheduled) TrustAt(height uint64) Trust {
	keys := s.Initial.Keys
	for _, r := range s.Rotations {
		h, _ := codec.Uint(r.Height)
		if height >= h+2 {
			keys = r.Keys
		}
	}
	return Trust{ChainID: s.Initial.ChainID, Keys: append([]string{}, keys...)}
}
