package eligibilityindex

import (
	"bytes"
	"civora/internal/codec"
	"civora/internal/core"
	"civora/internal/devcli"
	"civora/internal/eligibility"
	"civora/internal/upgrade"
	"crypto/ed25519"
	"encoding/hex"
	"encoding/json"
	"fmt"
	"os"
)

// GenerateFixture uses only committed, explicitly public test secrets.
func GenerateFixture(check bool) error {
	read := func(path string, out any) error {
		raw, e := os.ReadFile(path)
		if e != nil {
			return e
		}
		return json.Unmarshal(raw, out)
	}
	var f devcli.Fixture
	if e := read("spec/fixtures/accounts.json", &f); e != nil {
		return e
	}
	var keys struct {
		Signing struct {
			Seed string `json:"seed"`
		} `json:"signing"`
		Recovery struct {
			Seed string `json:"seed"`
		} `json:"recovery"`
		Recovery2 struct {
			Seed string `json:"seed"`
		} `json:"recovery2"`
	}
	if e := read("spec/fixtures/keys.TEST_ONLY.json", &keys); e != nil {
		return e
	}
	var identities struct {
		Identities []struct {
			Commitment string `json:"commitment"`
		} `json:"identities"`
	}
	if e := read("spec/fixtures/v0.3/identities.TEST_ONLY.json", &identities); e != nil {
		return e
	}
	if len(identities.Identities) < 2 {
		return fmt.Errorf("test identities missing")
	}
	f.Name = "accounts-eligibility-v0.3"
	network := codec.Hash("eligibility/test-network", "PUBLIC TEST ONLY v0.3")
	f.Genesis.NetworkID = network
	f.Genesis.ChainID, _ = codec.ChainID(network)
	id := "semaphore-v4.14.5-depth-16"
	f.Genesis.Eligibility = &eligibility.Config{CircuitDepth: "16", SuiteID: id}
	module := upgrade.Module{ID: id, Status: "active", Operations: []string{"eligibility.prove"}, ImplementationSHA256: codec.Hash("development/module", id), Artifacts: []string{}, Dependencies: []string{}, RetirementPolicy: "never_reactivate_revoked"}
	module.Parameters.Format = id
	f.Genesis.Registry.Modules = append(f.Genesis.Registry.Modules, module)
	var first core.Payload
	_ = json.Unmarshal(f.Blocks[0].Transactions[0].Body.Payload, &first)
	actor := codec.Hash("account", []any{network, first.SigningPK})
	for i := range f.Blocks {
		for j := range f.Blocks[i].Transactions {
			tx := &f.Blocks[i].Transactions[j]
			tx.Body.NetworkID = network
			tx.Body.ActorID = actor
			seed := keys.Recovery2.Seed
			if tx.Body.Type != "account.retire" {
				var payload core.Payload
				_ = json.Unmarshal(tx.Body.Payload, &payload)
				which := 1
				if tx.Body.Type == "account.register" {
					which = 0
					seed = keys.Signing.Seed
				} else {
					seed = keys.Recovery.Seed
				}
				payload.Commitment = identities.Identities[which].Commitment
				tx.Body.Payload = codec.Must(payload)
			}
			decoded, e := hex.DecodeString(seed)
			if e != nil || len(decoded) != 32 {
				return fmt.Errorf("invalid test seed")
			}
			digest, _ := hex.DecodeString(core.TxID(tx.Body))
			tx.Auth.Signature = hex.EncodeToString(ed25519.Sign(ed25519.NewKeyFromSeed(decoded), digest))
		}
	}
	engine, e := core.New(codec.Must(f.Genesis), 1)
	if e != nil {
		return e
	}
	f.ExpectedRoots = []string{}
	f.ExpectedEvents = [][]core.Event{}
	for _, block := range f.Blocks {
		next, events, e := engine.Apply(codec.Must(block))
		if e != nil {
			return e
		}
		engine = next
		f.ExpectedRoots = append(f.ExpectedRoots, engine.Root())
		f.ExpectedEvents = append(f.ExpectedEvents, events)
	}
	state := engine.Snapshot()
	f.ExpectedState = &state
	path := "spec/fixtures/v0.3/accounts.json"
	output := codec.Must(f)
	if check {
		old, e := os.ReadFile(path)
		if e != nil {
			return e
		}
		if !bytes.Equal(old, output) {
			return fmt.Errorf("stale eligibility account fixture")
		}
		return nil
	}
	return os.WriteFile(path, output, 0600)
}
