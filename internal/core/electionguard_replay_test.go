package core_test

import (
	"bytes"
	"civora/internal/codec"
	"civora/internal/core"
	"civora/internal/testutil"
	"testing"
)

func TestRealEncryptedReplayAndAtomicDuplicate(t *testing.T) {
	for _, label := range []string{"mixed", "all-no"} {
		t.Run(label, func(t *testing.T) {
			f := testutil.Load(t, "v0.4/replay-"+label)
			first := replay(t, f, 1)
			second := replay(t, f, 1)
			if first.Root() != second.Root() || !bytes.Equal(codec.Must(first.Snapshot()), codec.Must(second.Snapshot())) {
				t.Fatal("encrypted replay diverged")
			}
			if (len(first.Governance.Objects) > 0) != (label == "mixed") {
				t.Fatal("incorrect all-No/mixed effect")
			}
			engine, e := core.New(codec.Must(f.Genesis), 1)
			if e != nil {
				t.Fatal(e)
			}
			for _, b := range f.Blocks[:5] {
				engine, _, e = engine.Apply(codec.Must(b))
				if e != nil {
					t.Fatal(e)
				}
			}
			before := codec.Must(engine.Snapshot())
			bad := f.Blocks[5]
			bad.Transactions = append(append([]core.Transaction{}, bad.Transactions...), bad.Transactions[0])
			if _, _, e = engine.Apply(codec.Must(bad)); e == nil {
				t.Fatal("duplicate real ballot counted")
			}
			if !bytes.Equal(before, codec.Must(engine.Snapshot())) {
				t.Fatal("failed encrypted block changed state")
			}
			if len(engine.Governance.Spent) != 0 {
				t.Fatal("nullifier reserved by rejected block")
			}
		})
	}
}
