package citizen

import (
	"civora/internal/archive"
	"civora/internal/chain"
	"civora/internal/codec"
	"encoding/json"
	"fmt"
	"net/url"
)

type Origin struct {
	API string `json:"api"`
	RPC string `json:"rpc"`
}
type Chain struct {
	ChainID string      `json:"chain_id"`
	Lane    *string     `json:"lane_id"`
	Genesis archive.Ref `json:"genesis"`
	Origins []Origin    `json:"origins"`
	Peers   []string    `json:"peers"`
}
type Manifest struct {
	Format    string  `json:"format"`
	Name      string  `json:"name"`
	Mode      string  `json:"mode"`
	NetworkID string  `json:"network_id"`
	Profile   string  `json:"voting_profile"`
	Main      Chain   `json:"main"`
	Lanes     []Chain `json:"lanes"`
}

func ValidURL(s string) bool {
	u, e := url.Parse(s)
	return e == nil && (u.Scheme == "http" || u.Scheme == "https") && u.Host != "" && u.User == nil && u.RawQuery == "" && u.Fragment == ""
}
func ParseManifest(raw []byte) (Manifest, error) {
	var m Manifest
	if len(raw) > 1024*1024 {
		return m, fmt.Errorf("manifest capacity")
	}
	if e := codec.Decode(raw, "citizen_manifest", &m); e != nil {
		return m, e
	}
	main, e := codec.ChainID(m.NetworkID)
	if e != nil || m.Main.ChainID != main || m.Main.Lane != nil {
		return m, fmt.Errorf("manifest network/chain mismatch")
	}
	all := append([]Chain{m.Main}, m.Lanes...)
	for i, c := range all {
		if i > 0 && (c.Lane == nil || *c.Lane != fmt.Sprint(i-1) || c.ChainID != fmt.Sprintf("%s-v%02x", main, i-1)) {
			return m, fmt.Errorf("ordered lane manifest")
		}
		seen := map[string]bool{}
		for _, o := range c.Origins {
			if !ValidURL(o.API) || !ValidURL(o.RPC) || seen[o.API] {
				return m, fmt.Errorf("independent origin URLs required")
			}
			seen[o.API] = true
		}
		size, e := codec.Uint(c.Genesis.Size)
		if e != nil || size > 8*1024*1024 {
			return m, fmt.Errorf("genesis bound")
		}
	}
	return m, nil
}
func (m Manifest) CheckGenesis(c Chain, raw []byte) (*chain.Genesis, error) {
	if archive.Digest(raw) != c.Genesis.SHA256 || fmt.Sprint(len(raw)) != c.Genesis.Size {
		return nil, fmt.Errorf("genesis fingerprint")
	}
	g, e := chain.ParseGenesis(raw)
	if e != nil {
		return nil, e
	}
	if g.Doc.ChainID != c.ChainID {
		return nil, fmt.Errorf("genesis chain")
	}
	var config struct {
		Network string `json:"network_id"`
		Profile string `json:"voting_profile"`
		Ledger  struct {
			Network string `json:"network_id"`
			Profile string `json:"voting_profile"`
		} `json:"ledger"`
	}
	if e = json.Unmarshal(g.Application.Data, &config); e != nil {
		return nil, e
	}
	if g.Application.Kind == "coordinator" {
		config.Network = config.Ledger.Network
		config.Profile = config.Ledger.Profile
	}
	if config.Network != m.NetworkID || c.Lane == nil && config.Profile != m.Profile {
		return nil, fmt.Errorf("normative manifest/genesis divergence")
	}
	return g, nil
}
