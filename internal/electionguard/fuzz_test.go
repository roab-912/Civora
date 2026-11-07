package electionguard

import (
	"os"
	"testing"
)

func FuzzCompactBundle(f *testing.F) {
	f.Add([]byte{})
	f.Add([]byte("CIVIC"))
	f.Add(make([]byte, 32))
	if b, e := os.ReadFile("../../spec/fixtures/v0.4/ballot.envelope.json"); e == nil {
		f.Add(b)
	} else {
		f.Fatal(e)
	}
	f.Fuzz(func(t *testing.T, b []byte) {
		if len(b) > 1024*1024 {
			return
		}
		_, _ = DecodeBytes(b, Kind("ballot"))
	})
}
