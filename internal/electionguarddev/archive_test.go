package electionguarddev_test

import (
	"civora/internal/codec"
	"civora/internal/electionguarddev"
	"civora/internal/state/store"
	"civora/internal/testutil"
	"encoding/json"
	"testing"
)

func TestIndependentPublicArchiveAndTampering(t *testing.T) {
	f := testutil.Load(t, "v0.4/replay-mixed")
	dir := t.TempDir()
	ledger, e := store.Open(dir, codec.Must(f.Genesis), 1)
	if e != nil {
		t.Fatal(e)
	}
	for _, b := range f.Blocks {
		if e = ledger.Commit(codec.Must(b)); e != nil {
			t.Fatal(e)
		}
	}
	raw, e := electionguarddev.ExportArchive(dir, 1)
	if e != nil {
		t.Fatal(e)
	}
	report, e := electionguarddev.ReplayArchive(raw, 1)
	if e != nil {
		t.Fatal(e)
	}
	if report.AppHash != ledger.AppHash() || !report.Replayed || len(report.VerifiedResults) != 1 {
		t.Fatal("independent archive result differs")
	}
	var archive electionguarddev.Archive
	if e = json.Unmarshal(raw, &archive); e != nil {
		t.Fatal(e)
	}
	archive.Records[5].Block.Transactions[0].Body.NetworkID = "0000000000000000000000000000000000000000000000000000000000000000"
	if _, e = electionguarddev.ReplayArchive(codec.Must(archive), 1); e == nil {
		t.Fatal("altered encrypted archive accepted")
	}
}
