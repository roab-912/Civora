package partition

import (
	"civora/internal/codec"
	"civora/internal/consensus"
	"civora/internal/electionguard"
	"encoding/json"
	"fmt"
	cmtjson "github.com/cometbft/cometbft/libs/json"
	"github.com/cometbft/cometbft/types"
)

type Receipt struct {
	SignedHeader     string   `json:"signed_header"`
	TransactionProof string   `json:"transaction_proof"`
	Reference        BatchRef `json:"reference"`
	Envelope         string   `json:"envelope"`
	Index            string   `json:"index"`
	Inclusion        []string `json:"inclusion"`
}

// VerifyReceipt establishes a replicated deposit, not a counted vote. The
// transaction is authenticated by header H, unlike state after H (header H+1).
func VerifyReceipt(f Frozen, r Receipt) error {
	if e := codec.Decode(codec.Must(r), "partition_receipt", new(Receipt)); e != nil {
		return e
	}
	lane, e := codec.Uint(r.Reference.Lane)
	if e != nil || lane >= uint64(len(f.Lanes)) {
		return fmt.Errorf("receipt lane")
	}
	header, e := consensus.VerifyHeader(f.Lanes[lane], r.SignedHeader)
	if e != nil {
		return e
	}
	opens, _ := codec.Uint(f.Opens)
	closes, _ := codec.Uint(f.Closes)
	if header.Time.UnixMilli() < 0 || uint64(header.Time.UnixMilli()) < opens || uint64(header.Time.UnixMilli()) >= closes {
		return fmt.Errorf("receipt outside admission window")
	}
	raw, e := codec.Blob(r.TransactionProof)
	if e != nil || len(raw) > 400*1024 {
		return fmt.Errorf("transaction proof bound")
	}
	var tx types.TxProof
	if e = cmtjson.Unmarshal(raw, &tx); e != nil {
		return e
	}
	if len(tx.Data) > 256*1024 || tx.Proof.Total < 1 || tx.Proof.Total > 8192 {
		return fmt.Errorf("transaction inclusion bound")
	}
	if e = tx.Validate(header.DataHash); e != nil {
		return e
	}
	var command Command
	if e = codec.Decode(tx.Data, "partition_tx", &command); e != nil {
		return e
	}
	if command.Type != "partition.admit" {
		return fmt.Errorf("receipt transaction type")
	}
	var batch Batch
	if e = json.Unmarshal(command.Value, &batch); e != nil {
		return e
	}
	if batch.PollID != f.Context.PollID {
		return fmt.Errorf("receipt poll")
	}
	ref, e := Reference(f.Context, fmt.Sprint(lane), r.Reference.Start, batch.Envelopes)
	if e != nil {
		return e
	}
	if string(codec.Must(ref)) != string(codec.Must(r.Reference)) {
		return fmt.Errorf("receipt batch reference")
	}
	if e = VerifyCertificate(ref, batch.Certificate, f.ArchiveKeys); e != nil {
		return e
	}
	index, e := codec.Uint(r.Index)
	if e != nil || index >= uint64(len(batch.Envelopes)) || batch.Envelopes[index] != r.Envelope {
		return fmt.Errorf("receipt envelope index")
	}
	envelope, e := codec.Blob(r.Envelope)
	if e != nil {
		return e
	}
	if e = VerifyInclusion(ref.Root, envelope, index, uint64(len(batch.Envelopes)), r.Inclusion); e != nil {
		return e
	}
	var env electionguard.ScaleEnvelope
	if e = codec.Decode(envelope, "scale_envelope", &env); e != nil {
		return e
	}
	if env.Checkpoint != f.Context.Checkpoint || env.NetworkID != f.Context.NetworkID || env.PollID != f.Context.PollID || env.Epoch != f.Context.Epoch || env.Lane != fmt.Sprint(lane) {
		return fmt.Errorf("receipt context")
	}
	return nil
}
