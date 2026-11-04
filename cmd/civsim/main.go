//go:build civora_sim

// civsim is a local public-test-only governance harness, never a voting node.
package main

import (
	"civora/internal/codec"
	"civora/internal/devcli"
	"civora/internal/simulation"
	"fmt"
	"os"
)

func run() error {
	if len(os.Args) > 1 && os.Args[1] == "fixtures" {
		if len(os.Args) > 3 || len(os.Args) == 3 && os.Args[2] != "--check" {
			return fmt.Errorf("usage: civsim fixtures [--check]")
		}
		return simulation.GenerateFixtures("spec/fixtures/v0.2", len(os.Args) > 2)
	}
	if len(os.Args) == 2 && os.Args[1] == "demo" {
		b, e := simulation.GovernanceScenario()
		if e != nil {
			return e
		}
		laws, roles, activeGrants := 0, 0, 0
		for _, obj := range b.Engine.Governance.Objects {
			if obj.Kind == "law" {
				laws++
			} else {
				roles++
			}
		}
		for _, g := range b.Engine.Governance.Grants {
			if g.Status == "active" {
				activeGrants++
			}
		}
		fmt.Println(string(codec.Must(map[string]any{"profile": simulation.Profile, "warning": "PUBLIC TEST ONLY: choices and commitments are visible; not a secret ballot", "height": fmt.Sprint(b.Engine.Height), "app_hash": b.Engine.Root(), "laws": fmt.Sprint(laws), "roles": fmt.Sprint(roles), "active_grants": fmt.Sprint(activeGrants), "policy_revision": b.Engine.Governance.Network.PolicyRevision})))
		return nil
	}
	return devcli.Run(os.Args[1:], simulation.New)
}
func main() {
	if e := run(); e != nil {
		fmt.Fprintln(os.Stderr, e)
		os.Exit(1)
	}
}
