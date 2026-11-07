package qualification

import (
	"bytes"
	"civora/internal/codec"
	"civora/internal/partition"
	"civora/internal/partitiondev"
	"crypto/sha256"
	"encoding/json"
	"fmt"
	cmtjson "github.com/cometbft/cometbft/libs/json"
	"github.com/cometbft/cometbft/types"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func TestQuantilesAndSustainedRateIncludeEmptySeconds(t *testing.T) {
	s := accumulator{start: 1000, end: 4000, seconds: make([]uint64, 3), lanes: make([]uint64, 32), seen: map[[32]byte]struct{}{}}
	for i := 0; i < 100; i++ {
		id := sha256.Sum256([]byte(fmt.Sprint(i)))
		if e := s.add(id, 0, 1000, 1000+uint64(i), 8000); e != nil {
			t.Fatal(e)
		}
	}
	id := sha256.Sum256([]byte("0"))
	s.add(id, 0, 1000, 2000, 8000)
	if s.unique != 100 || s.duplicates != 1 || s.percentile(95) != "94" || s.percentile(99) != "98" || s.seconds[1] != 0 {
		t.Fatal(s.unique, s.duplicates, s.percentile(95), s.seconds)
	}
	if e := s.add(id, 32, 1000, 2000, 1); e == nil {
		t.Fatal("out of range lane")
	}
}
func TestActualReceiptVerificationDoesNotCountReplaysOrForgedProofs(t *testing.T) {
	raw, e := os.ReadFile("../../spec/fixtures/v0.5/transport.json")
	if e != nil {
		t.Fatal(e)
	}
	var f partitiondev.Fixture
	if e = json.Unmarshal(raw, &f); e != nil {
		t.Fatal(e)
	}
	for i, run := range f.Lanes {
		b := run.Blocks[1]
		if len(b.Transactions) == 0 {
			continue
		}
		txs := types.Txs{}
		for _, encoded := range b.Transactions {
			tx, _ := codec.Blob(encoded)
			txs = append(txs, types.Tx(tx))
		}
		proof, e := cmtjson.Marshal(txs.Proof(0))
		if e != nil {
			t.Fatal(e)
		}
		var command partition.Command
		json.Unmarshal(txs[0], &command)
		var batch partition.Batch
		json.Unmarshal(command.Value, &batch)
		ref, e := partition.Reference(f.Frozen.Context, fmt.Sprint(i), "0", batch.Envelopes)
		if e != nil {
			t.Fatal(e)
		}
		items := [][]byte{}
		for _, s := range batch.Envelopes {
			b, _ := codec.Blob(s)
			items = append(items, b)
		}
		path, _ := partition.Inclusion(items, 0)
		tm, _ := codec.Uint(b.TimeMS)
		h, _ := codec.Uint(b.Height)
		header, e := partitiondev.SignedHeaderData(f.Frozen.Lanes[i], h, tm, run.Roots[0], 5, txs)
		if e != nil {
			t.Fatal(e)
		}
		receipt := partition.Receipt{SignedHeader: header, TransactionProof: partitiondev.Encode(proof), Reference: ref, Envelope: batch.Envelopes[0], Index: "0", Inclusion: path}
		sample := Sample{"1000", "2000", &receipt}
		line := append(codec.Must(sample), '\n')
		stream := append(append([]byte{}, line...), line...)
		receipt.Index = "999"
		stream = append(stream, append(codec.Must(sample), '\n')...)
		stream = append(stream, append(codec.Must(Sample{"1000", "2000", nil}), '\n')...)
		for name, data := range map[string][]byte{"frozen.TEST_ONLY.json": codec.Must(f.Frozen), "receipts.TEST_ONLY.jsonl": stream} {
			path := filepath.Join("..", "..", "spec", "fixtures", "v0.9", name)
			if os.Getenv("CIVORA_WRITE_QUALIFICATION_FIXTURES") == "1" {
				if e = os.WriteFile(path, data, 0600); e != nil {
					t.Fatal(e)
				}
			}
			got, err := os.ReadFile(path)
			if err != nil || !bytes.Equal(got, data) {
				t.Fatalf("fixture changed %s: %v", name, err)
			}
		}
		r, e := AnalyzeReceipts(f.Frozen, bytes.NewReader(stream), 1000, 4000)
		if e != nil {
			t.Fatal(e)
		}
		if r.Unique != "1" || r.Duplicates != "1" || r.Failures != "2" || r.MinRate != "0" || r.Qualified {
			t.Fatal(r)
		}
		if _, e = AnalyzeReceipts(f.Frozen, strings.NewReader(`{"receipt":null,"received_at_ms":"2000","sent_at_ms":"1000","verified":true}`), 1000, 4000); e == nil {
			t.Fatal("extra field accepted")
		}
		return
	}
	t.Fatal("no populated lane")
}
