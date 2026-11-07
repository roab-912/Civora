package codec_test

import (
	"bytes"
	"civora/internal/codec"
	"testing"
)

func FuzzCanonical(f *testing.F) {
	for _, s := range []string{`{}`, `{"v":"1","x":[true,null,"é"]}`, `{"a":"1","a":"2"}`, `{"x":1}`, `{"x":"\ud800"}`} {
		f.Add([]byte(s))
	}
	f.Fuzz(func(t *testing.T, b []byte) {
		if len(b) > 65536 {
			return
		}
		v, e := codec.Parse(b)
		if e != nil {
			return
		}
		raw, e := codec.Canonical(v)
		if e != nil || !bytes.Equal(raw, b) {
			t.Fatal("canonical round trip changed accepted bytes", e)
		}
	})
}
