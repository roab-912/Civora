package electionaudit

import (
	"civora/internal/chain"
	"civora/internal/codec"
	"civora/internal/consensus"
	"civora/internal/partition"
	"fmt"
)

func verifyCertificate(g *chain.Genesis, r Record, result partition.Provisional, dossier string) error {
	if r.Certificate == nil {
		return nil
	}
	f := r.Frozen
	if f.Audit == nil {
		return fmt.Errorf("certificate has no authorized auditors")
	}
	p := *r.Certificate
	h, e := codec.Uint(p.Height)
	if e != nil {
		return e
	}
	if e = consensus.VerifyWitness(g.TrustAt(h+1), p); e != nil {
		return e
	}
	if !p.Path.Exists || p.Path.Entry.Table != "poll_certificates" || string(codec.Must(p.Path.Entry.Key)) != string(codec.Must(f.Context.PollID)) {
		return fmt.Errorf("certificate state path")
	}
	var certificate partition.Certification
	if e = codec.Decode(codec.Must(p.Path.Entry.Value), "audit_certification", &certificate); e != nil {
		return e
	}
	published, _ := codec.Uint(result.PublishedMS)
	certified, _ := codec.Uint(certificate.Time)
	closes, _ := codec.Uint(f.Closes)
	if certificate.PollID != f.Context.PollID || certificate.Dossier != dossier || certificate.PublishedMS != result.PublishedMS || certificate.Height != p.Height || certified < published+partition.ChallengeMS || certified >= closes+48*60*60*1000 {
		return fmt.Errorf("certificate deadline/dossier")
	}
	known := map[string]bool{}
	implementations := map[string]bool{}
	for _, a := range certificate.Attestations {
		if known[a.Signer] || a.Dossier != dossier {
			return fmt.Errorf("auditor duplicate/dossier")
		}
		if e = partition.VerifyAttestation(f, a); e != nil {
			return e
		}
		known[a.Signer] = true
		implementations[a.Implementation] = true
	}
	if len(known) < 3 || len(implementations) < 2 {
		return fmt.Errorf("independent audit quorum")
	}
	return nil
}
