package main

import (
	"civora/internal/codec"
	"testing"
)

func TestDevelopmentSessionNetwork(t *testing.T) {
	first, err := developmentNetworkID("00000000000000000000000000000001")
	if err != nil {
		t.Fatal(err)
	}
	again, err := developmentNetworkID("00000000000000000000000000000001")
	if err != nil || again != first {
		t.Fatalf("fixed TEST_ONLY session must be reproducible: %s, %v", again, err)
	}
	second, err := developmentNetworkID("00000000000000000000000000000002")
	if err != nil || first == second {
		t.Fatalf("new session reused a network: %s, %v", second, err)
	}
	legacy := codec.Hash("development/pwa", "PUBLIC_TEST_ONLY_V0_7")
	if first == legacy || second == legacy {
		t.Fatal("new session reused the historical resettable network")
	}
	if _, err := codec.ChainID(first); err != nil {
		t.Fatal(err)
	}
	for _, invalid := range []string{"01", "not-a-session", "AAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAA", "0000000000000000000000000000000000\n"} {
		if _, err := developmentNetworkID(invalid); err == nil {
			t.Fatalf("accepted invalid session %q", invalid)
		}
	}
	generated, err := developmentNetworkID("")
	if err != nil {
		t.Fatal(err)
	}
	next, err := developmentNetworkID("")
	if err != nil || generated == next {
		t.Fatalf("implicit sessions must be distinct: %v", err)
	}
}
