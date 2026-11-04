package codec_test

import (
	"bytes"
	"civora/internal/codec"
	"civora/internal/core"
	"civora/internal/state"
	"civora/internal/testutil"
	"crypto/ed25519"
	"encoding/hex"
	"encoding/json"
	"strings"
	"testing"
)

func TestSection13(t *testing.T) {
	var v struct {
		NetworkID string    `json:"network_id"`
		ChainID   string    `json:"chain_id"`
		Seed      string    `json:"seed"`
		AccountID string    `json:"account_id"`
		Body      core.Body `json:"body"`
		TxID      string    `json:"tx_id"`
		Signature string    `json:"signature"`
		EmptyRoot string    `json:"empty_root"`
	}
	if e := json.Unmarshal(testutil.Read(t, "vector13"), &v); e != nil {
		t.Fatal(e)
	}
	chain, e := codec.ChainID(v.NetworkID)
	if e != nil || chain != v.ChainID {
		t.Fatal("chain_id", chain, e)
	}
	seed, _ := hex.DecodeString(v.Seed)
	key := ed25519.NewKeyFromSeed(seed)
	pk := hex.EncodeToString(key.Public().(ed25519.PublicKey))
	if id := codec.Hash("account", []any{v.NetworkID, pk}); id != v.AccountID {
		t.Fatal("account", id)
	}
	if id := core.TxID(v.Body); id != v.TxID {
		t.Fatal("tx_id", id)
	}
	digest, _ := hex.DecodeString(v.TxID)
	sig := ed25519.Sign(key, digest)
	if hex.EncodeToString(sig) != v.Signature || !ed25519.Verify(key.Public().(ed25519.PublicKey), digest, sig) {
		t.Fatal("signature")
	}
	if root := state.Root(nil); root != v.EmptyRoot {
		t.Fatal("empty_root", root)
	}
}
func TestEncodingCorpus(t *testing.T) {
	var cases []struct {
		Name   string `json:"name"`
		Raw    string `json:"raw_base64url"`
		Schema string `json:"schema"`
	}
	if e := json.Unmarshal(testutil.Read(t, "encoding-negative"), &cases); e != nil {
		t.Fatal(e)
	}
	for _, c := range cases {
		t.Run(c.Name, func(t *testing.T) {
			raw, e := codec.Blob(c.Raw)
			if e != nil {
				t.Fatal(e)
			}
			v, e := codec.Parse(raw)
			if e == nil && c.Schema != "" {
				e = codec.Validate(c.Schema, v)
			}
			if e == nil {
				t.Fatal("accepted invalid encoding")
			}
		})
	}
}
func TestJCSUnicodeAndEscapes(t *testing.T) {
	v := map[string]any{"\ue000": "<>&\u2028", "😀": "é\n\t\x01", "1": "one", "10": "ten", "2": "two"}
	want := "{\"1\":\"one\",\"10\":\"ten\",\"2\":\"two\",\"😀\":\"é\\n\\t\\u0001\",\"\ue000\":\"<>&\u2028\"}"
	if got := string(codec.Must(v)); got != want {
		t.Fatalf("got %q want %q", got, want)
	}
	parsed, e := codec.Parse([]byte(want))
	if e != nil || !bytes.Equal(codec.Must(parsed), []byte(want)) {
		t.Fatal(e)
	}
}
func TestStrictBase64AndIntegers(t *testing.T) {
	for _, s := range []string{"Zg=", "Zh", "Zg==", "+w", "/w", "Zg\n"} {
		if _, e := codec.Blob(s); e == nil {
			t.Fatal("accepted blob", s)
		}
	}
	if b, e := codec.Blob("Zg"); e != nil || string(b) != "f" {
		t.Fatal(e)
	}
	for _, s := range []string{"00", "-1", "18446744073709551616", "1.0", "+1"} {
		if _, e := codec.Uint(s); e == nil {
			t.Fatal("accepted integer", s)
		}
	}
	if _, e := codec.Uint("18446744073709551615"); e != nil {
		t.Fatal(e)
	}
}
func FuzzCanonicalRoundTrip(f *testing.F) {
	for _, s := range []string{`{}`, `{"é":"😀"}`, `["1",null,true]`, `{"x":"\\n"}`} {
		f.Add(s)
	}
	f.Fuzz(func(t *testing.T, s string) {
		v, e := codec.Parse([]byte(s))
		if e == nil {
			out, e := codec.Canonical(v)
			if e != nil || !strings.EqualFold(hex.EncodeToString(out), hex.EncodeToString([]byte(s))) {
				t.Fatal("round trip")
			}
		}
	})
}
