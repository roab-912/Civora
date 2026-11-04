package core

import (
	"civora/internal/codec"
	"civora/internal/governance"
	"crypto/ed25519"
	"encoding/hex"
	"encoding/json"
	"fmt"
	"strings"
)

func isProof(kind string) bool {
	return kind == "poll.configure" || kind == "vote.cast" || kind == "poll.tally"
}

// Null actor and nonce are part of the proof-authorized body's signed bytes.
// Keep string fields for compatibility with the original account API.
func (b Body) MarshalJSON() ([]byte, error) {
	type plain Body
	if !isProof(b.Type) {
		return json.Marshal(plain(b))
	}
	v := map[string]any{"v": b.V, "network_id": b.NetworkID, "type": b.Type, "actor_id": nil, "nonce": nil, "payload": b.Payload}
	if b.Epoch != "" {
		v["protocol_epoch"] = b.Epoch
	}
	return json.Marshal(v)
}

func (e *Engine) execution(previous *Engine, emit func(string, string)) governance.Execution {
	view := func(accounts map[string]Account) map[string]governance.AccountView {
		out := map[string]governance.AccountView{}
		for id, a := range accounts {
			out[id] = governance.AccountView{Active: a.Status == "active", SigningPK: a.SigningPK, Commitment: a.Commitment}
		}
		return out
	}
	x := governance.Execution{NetworkID: e.Config.NetworkID, Epoch: e.Config.Registry.Epoch, Height: fmt.Sprint(e.Height), Time: e.Time, Accounts: view(e.Accounts), PreviousAccounts: view(previous.Accounts), PreviousHeight: fmt.Sprint(previous.Height), Eligibility: e.adapters.Eligibility, Crypto: e.adapters.Voting, Effects: e.adapters.Effects, Authorize: e.Authorize, Emit: emit}
	if e.Config.Governance != nil {
		x.Config = *e.Config.Governance
	}
	return x
}
func (e *Engine) applyTransaction(tx Transaction, emit func(string, string), position string) error {
	b := tx.Body
	if len(codec.Must(tx)) > MaxTxBytes {
		return fmt.Errorf("ERR_SIZE")
	}
	if b.NetworkID != e.Config.NetworkID {
		return fmt.Errorf("ERR_NETWORK")
	}
	if b.V != e.Config.Registry.TransactionVersion || b.V == "1" && e.Config.Registry.Epoch != "1" || b.V == "2" && b.Epoch != e.Config.Registry.Epoch {
		return fmt.Errorf("ERR_EPOCH")
	}
	if strings.HasPrefix(b.Type, "account.") {
		return e.applyAccount(tx)
	}
	if e.Governance == nil {
		return fmt.Errorf("ERR_GOVERNANCE_DISABLED")
	}
	x := e.execution(e, emit)
	if isProof(b.Type) {
		switch b.Type {
		case "poll.configure":
			return e.Governance.Configure(x, b.Payload)
		case "vote.cast":
			return e.Governance.Cast(x, b.Payload, TxID(b), position)
		case "poll.tally":
			return e.Governance.Tally(x, b.Payload)
		}
	}
	if _, err := e.Config.Registry.AccountSuite(); err != nil {
		return err
	}
	a, ok := e.Accounts[b.ActorID]
	if !ok || a.Status != "active" {
		return fmt.Errorf("ERR_ACCOUNT")
	}
	nonce, err := codec.Uint(b.Nonce)
	if err != nil {
		return err
	}
	if nonce == ^uint64(0) {
		return fmt.Errorf("ERR_NONCE_OVERFLOW")
	}
	if b.Nonce != a.NextNonce {
		return fmt.Errorf("ERR_NONCE")
	}
	key, _ := hex.DecodeString(a.SigningPK)
	sig, _ := hex.DecodeString(tx.Auth.Signature)
	digest, _ := hex.DecodeString(TxID(b))
	if !ed25519.Verify(key, digest, sig) {
		return fmt.Errorf("ERR_SIGNATURE")
	}
	switch b.Type {
	case "proposal.submit":
		err = e.Governance.Submit(x, TxID(b), b.ActorID, b.Payload)
	case "proposal.endorse":
		var p struct {
			ProposalID string `json:"proposal_id"`
		}
		_ = json.Unmarshal(b.Payload, &p)
		err = e.Governance.Endorse(x, p.ProposalID, b.ActorID)
	case "role.publish":
		err = e.Governance.Publish(x, b.ActorID, b.Payload)
	default:
		err = fmt.Errorf("ERR_TYPE")
	}
	if err != nil {
		return err
	}
	a.NextNonce = fmt.Sprint(nonce + 1)
	e.Accounts[b.ActorID] = a
	return nil
}
