package citizencli

import (
	"civora/internal/citizen"
	"civora/internal/codec"
	"fmt"
	"github.com/cometbft/cometbft/libs/log"
	"os"
	"path/filepath"
	"strings"
)

func offline(home, command string) error {
	raw, e := os.ReadFile(filepath.Join(home, "network.json"))
	if e != nil {
		return e
	}
	m, e := citizen.ParseManifest(raw)
	if e != nil {
		return e
	}
	lanes := []string{}
	for _, lane := range m.Lanes {
		if _, e = os.Stat(filepath.Join(home, "archives", lane.ChainID)); e == nil {
			lanes = append(lanes, *lane.Lane)
		}
	}
	h, e := citizen.Load(home, strings.Join(lanes, ","), "offline", 28650, log.NewNopLogger())
	if e != nil {
		return e
	}
	defer h.Close()
	if e = h.Refresh(); e != nil {
		return e
	}
	if command == "verify" {
		if e = h.Verify(); e != nil {
			return e
		}
	}
	chains := []any{}
	for _, desc := range append([]citizen.Chain{m.Main}, m.Lanes...) {
		r := h.Readers[desc.ChainID]
		if r == nil {
			continue
		}
		height, root := "0", ""
		if v := r.Latest(); v != nil {
			height, root = v.Height, v.Root
		}
		chains = append(chains, map[string]any{"chain_id": desc.ChainID, "height": height, "app_hash": root, "replayed": command == "verify", "from": "genesis"})
	}
	fmt.Println(string(codec.Must(map[string]any{"offline": true, "stale": true, "network_id": m.NetworkID, "security_status": "development_unqualified", "chains": chains})))
	return nil
}
