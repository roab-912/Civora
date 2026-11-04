package electionguarddev

import (
	"bytes"
	"civora/internal/codec"
	"civora/internal/core"
	"civora/internal/state/store"
	"fmt"
	"os"
	"path/filepath"
	"sort"
)

// Development archive: bounded and fully replayed; scalable archive transport
// and BFT authentication remain separate milestones.
const MaxArchiveBytes = 64 * 1024 * 1024

type Archive struct {
	Format  string         `json:"format"`
	Genesis core.Config    `json:"genesis"`
	Records []store.Record `json:"records"`
}
type Audit struct {
	NetworkID       string   `json:"network_id"`
	Height          string   `json:"height"`
	AppHash         string   `json:"app_hash"`
	Replayed        bool     `json:"replayed"`
	VerifiedResults []string `json:"verified_results"`
}

func ReplayArchive(raw []byte, supported uint64) (Audit, error) {
	if len(raw) > MaxArchiveBytes {
		return Audit{}, fmt.Errorf("archive too large")
	}
	var archive Archive
	if e := codec.Decode(raw, "eg_archive", &archive); e != nil {
		return Audit{}, e
	}
	engine, e := core.New(codec.Must(archive.Genesis), supported)
	if e != nil {
		return Audit{}, e
	}
	for _, record := range archive.Records {
		next, events, e := engine.Apply(codec.Must(record.Block))
		if e != nil {
			return Audit{}, e
		}
		if record.AppHash != next.Root() || !bytes.Equal(codec.Must(record.State), codec.Must(next.Snapshot())) || !bytes.Equal(codec.Must(record.Events), codec.Must(events)) {
			return Audit{}, fmt.Errorf("archive state/events/hash mismatch at %s", record.Block.Height)
		}
		engine = next
	}
	out := Audit{NetworkID: engine.Config.NetworkID, Height: fmt.Sprint(engine.Height), AppHash: engine.Root(), Replayed: true, VerifiedResults: []string{}}
	if engine.Governance != nil {
		for id, poll := range engine.Governance.Polls {
			if poll.Result != nil {
				out.VerifiedResults = append(out.VerifiedResults, id)
			}
		}
	}
	sort.Strings(out.VerifiedResults)
	return out, nil
}

// ExportArchive only reads an existing ledger and never imports fixture secrets.
// The caller must own the single-writer store while this operation executes.
func ExportArchive(directory string, supported uint64) ([]byte, error) {
	genesis, e := os.ReadFile(filepath.Join(directory, "genesis.json"))
	if e != nil {
		return nil, e
	}
	ledger, e := store.Open(directory, genesis, supported)
	if e != nil {
		return nil, e
	}
	records, e := ledger.Export()
	if e != nil {
		return nil, e
	}
	var config core.Config
	if e = codec.Decode(genesis, "config", &config); e != nil {
		return nil, e
	}
	out := Archive{Format: "civic-audit-1", Genesis: config, Records: []store.Record{}}
	for _, raw := range records {
		var record store.Record
		if e = codec.Decode(raw, "commit", &record); e != nil {
			return nil, e
		}
		out.Records = append(out.Records, record)
	}
	raw := codec.Must(out)
	if len(raw) > MaxArchiveBytes {
		return nil, fmt.Errorf("archive too large")
	}
	return raw, nil
}
