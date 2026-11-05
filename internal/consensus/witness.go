package consensus

import (
	"civora/internal/codec"
	"civora/internal/state"
	"encoding/hex"
	"fmt"
)

type WitnessProof struct {
	Height       string        `json:"height"`
	SignedHeader string        `json:"signed_header"`
	Path         state.Witness `json:"path"`
}

func VerifyWitness(t Trust, p WitnessProof) error {
	h, e := codec.Uint(p.Height)
	if e != nil || h == 0 {
		return fmt.Errorf("state witness height")
	}
	sh, e := VerifyHeader(t, p.SignedHeader)
	if e != nil {
		return e
	}
	if sh.Height != int64(h+1) {
		return fmt.Errorf("state witness H+1")
	}
	return state.VerifyWitness(hex.EncodeToString(sh.AppHash), p.Path)
}
