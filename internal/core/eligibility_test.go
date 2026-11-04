package core_test

import (
	"bytes"
	"civora/internal/codec"
	"civora/internal/core"
	"civora/internal/eligibility"
	"civora/internal/testutil"
	"encoding/json"
	"strings"
	"testing"
)

func TestAccountEligibilityLifecycleAndAtomicity(t *testing.T) {
	for _, depth := range []string{"16", "26"} {
		t.Run(depth, func(t *testing.T) {
			f := testutil.WithEligibility(testutil.Load(t, "accounts"), depth)
			engine, e := core.New(codec.Must(f.Genesis), 1)
			if e != nil {
				t.Fatal(e)
			}
			original := engine
			engine, _, e = engine.Apply(codec.Must(f.Blocks[0]))
			if e != nil {
				t.Fatal(e)
			}
			if original.Eligibility.Size != 0 || engine.Eligibility.Size != 1 || *engine.Eligibility.Root() != "5" {
				t.Fatal("register or copy-on-write root")
			}
			before := codec.Must(engine.Snapshot())
			bad := f.Blocks[1]
			bad.Transactions = append(bad.Transactions, bad.Transactions[0])
			if _, _, e = engine.Apply(codec.Must(bad)); e == nil {
				t.Fatal("concurrent nonce accepted")
			}
			if !bytes.Equal(before, codec.Must(engine.Snapshot())) {
				t.Fatal("failed block modified eligibility")
			}
			engine, _, e = engine.Apply(codec.Must(f.Blocks[1]))
			if e != nil {
				t.Fatal(e)
			}
			zero, _ := eligibility.ParseField("0", false)
			six, _ := eligibility.ParseField("6", true)
			want := eligibility.Hash(zero, six).String()
			if engine.Eligibility.Size != 2 || engine.Eligibility.Active != 1 || *engine.Eligibility.Root() != want {
				t.Fatal("rotation did not zero old leaf and append new leaf")
			}
			for _, a := range engine.Accounts {
				if a.EligibilityIndex == nil || *a.EligibilityIndex != "1" {
					t.Fatal("current index")
				}
			}
			for _, b := range f.Blocks[2:] {
				engine, _, e = engine.Apply(codec.Must(b))
				if e != nil {
					t.Fatal(e)
				}
			}
			if engine.Eligibility.Active != 0 || engine.Eligibility.Size != 2 {
				t.Fatal("retirement")
			}
			if e = codec.Validate("state", mustParse(t, codec.Must(engine.Snapshot()))); e != nil {
				t.Fatal(e)
			}
		})
	}
}
func TestKeyOnlyRotationPreservesEligibilityPosition(t *testing.T) {
	f := testutil.WithEligibility(testutil.Load(t, "accounts"), "16")
	engine, e := core.New(codec.Must(f.Genesis), 1)
	if e != nil {
		t.Fatal(e)
	}
	engine, _, e = engine.Apply(codec.Must(f.Blocks[0]))
	if e != nil {
		t.Fatal(e)
	}
	tx := f.Blocks[1].Transactions[0]
	var payload core.Payload
	_ = json.Unmarshal(tx.Body.Payload, &payload)
	payload.Commitment = "5"
	tx.Body.Payload = codec.Must(payload)
	tx = resign(t, tx, "4ccd089b28ff96da9db6c346ec114e0f5b8a319f35aba624da8cf6ed4fb8a6fb")
	block := f.Blocks[1]
	block.Transactions = []core.Transaction{tx}
	engine, _, e = engine.Apply(codec.Must(block))
	if e != nil {
		t.Fatal(e)
	}
	if engine.Eligibility.Size != 1 || engine.Eligibility.Active != 1 || *engine.Eligibility.Root() != "5" {
		t.Fatal("key rotation consumed a new position")
	}
	for i, m := range engine.Config.Registry.Modules {
		if m.ID == f.Genesis.Eligibility.SuiteID {
			engine.Config.Registry.Modules[i].Status = "revoked"
		}
	}
	if e = engine.CheckTx(codec.Must(f.Blocks[3].Transactions[0])); e == nil || !strings.Contains(e.Error(), "ERR_SUITE") {
		t.Fatal("revoked eligibility suite used", e)
	}
}
