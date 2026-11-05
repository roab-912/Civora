package citizendev

import (
	"civora/internal/chain"
	"civora/internal/codec"
	"civora/internal/electionaudit"
	"civora/internal/network"
	"civora/internal/partition"
	"civora/internal/partitiondev"
	"encoding/hex"
	"fmt"
	"os"
)

func Complete(f Fixture, work string) (Fixture, error) {
	host, e := Serve(f, work)
	if e != nil {
		return f, e
	}
	defer host.Close()
	var poll string
	for _, entry := range host.Readers[f.Manifest.Main.ChainID].Latest().Entries {
		if entry.Table == "transport_polls" {
			poll = fmt.Sprint(entry.Key)
			break
		}
	}
	record, e := host.Election(poll)
	if e != nil {
		return f, e
	}
	report, e := electionaudit.Verify(record, host.API.Objects, f.Manifest.Main.Genesis.SHA256)
	if e != nil {
		return f, e
	}
	raw, e := os.ReadFile("spec/fixtures/v0.6/audit-ts.json")
	if e != nil {
		return f, fmt.Errorf("run independent TypeScript audit before completing fixture: %w", e)
	}
	var ts partition.Attestation
	if e = codec.Decode(raw, "audit_attestation", &ts); e != nil {
		return f, e
	}
	if ts.Implementation != partition.TSAudit || ts.Dossier != report.Dossier {
		return f, fmt.Errorf("TypeScript audit dossier mismatch")
	}
	if e = partition.VerifyAttestation(record.Frozen, ts); e != nil {
		return f, e
	}
	raw, e = codec.Blob(f.Runs[0].Genesis)
	if e != nil {
		return f, e
	}
	g, e := chain.ParseGenesis(raw)
	if e != nil {
		return f, e
	}
	builder, e := New(g.Application)
	if e != nil {
		return f, e
	}
	for _, bundle := range f.Runs[0].Blocks {
		block, e := chain.Decode(bundle)
		if e != nil {
			return f, e
		}
		txs := [][]byte{}
		for _, tx := range block.Data.Txs {
			txs = append(txs, []byte(tx))
		}
		if e = builder.Add(uint64(block.Time.UnixMilli()), txs...); e != nil {
			return f, e
		}
	}
	attestations := [][]byte{}
	for i := 0; i < 2; i++ {
		key := network.TestKey(g.Doc.ChainID, "audit", i)
		a := partition.Attestation{PollID: poll, Dossier: report.Dossier, Implementation: partition.GoAudit, Signer: hex.EncodeToString(key.PubKey().Bytes())}
		sig, _ := key.Sign(partition.AttestationMessage(record.Frozen, a))
		a.Signature = hex.EncodeToString(sig)
		attestations = append(attestations, partitiondev.Command("audit.attest", a))
	}
	attestations = append(attestations, partitiondev.Command("audit.attest", ts))
	closes, _ := codec.Uint(record.Frozen.Closes)
	if e = builder.Add(closes+13, attestations...); e != nil {
		return f, e
	}
	result := builder.Machine.(*partition.Main).Results[poll]
	published, _ := codec.Uint(result.PublishedMS)
	certAt := published + partition.ChallengeMS
	if e = builder.Add(certAt); e != nil {
		return f, e
	}
	if e = builder.Add(certAt + 1); e != nil {
		return f, e
	}
	proposal := builder.Machine.(*partition.Main).Engine.Governance.Proposals[record.Frozen.ProposalID]
	if proposal.EffectiveAtMS == nil {
		return f, fmt.Errorf("audit did not schedule effect")
	}
	effect, _ := codec.Uint(*proposal.EffectiveAtMS)
	if e = builder.Add(effect); e != nil {
		return f, e
	}
	if e = builder.Add(effect + 1); e != nil {
		return f, e
	}
	if builder.Machine.(*partition.Main).Engine.Governance.Proposals[record.Frozen.ProposalID].State != "applied" {
		return f, fmt.Errorf("certified decision not applied")
	}
	f.Runs[0] = builder.Run
	return f, nil
}
