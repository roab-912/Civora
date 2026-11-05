package consensus

import (
	"bytes"
	"civora/internal/codec"
	"civora/internal/state"
	"encoding/hex"
	"fmt"
	"github.com/cometbft/cometbft/crypto/ed25519"
	cmtjson "github.com/cometbft/cometbft/libs/json"
	"github.com/cometbft/cometbft/types"
)

// Trust is supplied by authenticated genesis or a frozen, authenticated poll.
// A proof cannot supply its own validators.
type Trust struct {
	ChainID string   `json:"chain_id"`
	Keys    []string `json:"keys"`
}
type StateProof struct {
	Height       string      `json:"height"`
	SignedHeader string      `json:"signed_header"`
	Path         state.Proof `json:"path"`
}

func (t Trust) Set() (*types.ValidatorSet, error) {
	if len(t.Keys) != 7 || len(t.ChainID) == 0 || len(t.ChainID) > 50 {
		return nil, fmt.Errorf("seven unit validators required")
	}
	vals := []*types.Validator{}
	seen := map[string]bool{}
	for _, s := range t.Keys {
		k, e := hex.DecodeString(s)
		if e != nil || len(k) != 32 || hex.EncodeToString(k) != s || seen[s] {
			return nil, fmt.Errorf("validator key")
		}
		seen[s] = true
		vals = append(vals, types.NewValidator(ed25519.PubKey(k), 1))
	}
	return types.NewValidatorSet(vals), nil
}
func VerifyHeader(t Trust, encoded string) (*types.SignedHeader, error) {
	raw, e := codec.Blob(encoded)
	if e != nil || len(raw) > 48*1024 {
		return nil, fmt.Errorf("signed header bound/encoding")
	}
	var sh types.SignedHeader
	if e = cmtjson.Unmarshal(raw, &sh); e != nil {
		return nil, e
	}
	if e = sh.ValidateBasic(t.ChainID); e != nil {
		return nil, e
	}
	vs, e := t.Set()
	if e != nil {
		return nil, e
	}
	if !bytes.Equal(sh.ValidatorsHash, vs.Hash()) {
		return nil, fmt.Errorf("frozen validator set mismatch")
	}
	if e = vs.VerifyCommit(t.ChainID, sh.Commit.BlockID, sh.Height, sh.Commit); e != nil {
		return nil, e
	}
	return &sh, nil
}
func VerifyState(t Trust, p StateProof) (*types.SignedHeader, error) {
	sh, e := VerifyHeader(t, p.SignedHeader)
	if e != nil {
		return nil, e
	}
	h, e := codec.Uint(p.Height)
	if e != nil || h == 0 || sh.Height <= 1 || uint64(sh.Height)-1 != h {
		return nil, fmt.Errorf("AppHash requires state height plus one")
	}
	if e = state.Verify(hex.EncodeToString(sh.AppHash), p.Path); e != nil {
		return nil, e
	}
	return sh, nil
}
