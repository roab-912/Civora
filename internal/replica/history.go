package replica

import (
	"bytes"
	"civora/internal/codec"
	"fmt"
	"os"
	"path/filepath"
)

func (j *Journal) Read(height uint64) (Record, error) {
	raw, e := os.ReadFile(filepath.Join(j.directory, fmt.Sprintf("%020d.record.json", height)))
	if e != nil {
		return Record{}, e
	}
	var r Record
	e = codec.Decode(raw, "replica_record", &r)
	if e == nil && r.Block.Height != fmt.Sprint(height) {
		e = fmt.Errorf("record height mismatch")
	}
	return r, e
}
func (j *Journal) Replay(height uint64) (Machine, error) {
	if height > j.Machine.Height() {
		return nil, fmt.Errorf("height unavailable")
	}
	m, e := j.factory()
	if e != nil {
		return nil, e
	}
	for h := uint64(1); h <= height; h++ {
		r, e := j.Read(h)
		if e != nil {
			return nil, e
		}
		m, e = m.Apply(r.Block)
		if e != nil {
			return nil, e
		}
		if m.Root() != r.AppHash || !bytes.Equal(m.State(), r.State) {
			return nil, fmt.Errorf("historical replay mismatch")
		}
	}
	return m, nil
}
func (j *Journal) Directory() string { return j.directory }
