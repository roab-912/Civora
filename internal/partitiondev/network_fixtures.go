package partitiondev

import (
	"civora/internal/codec"
	"civora/internal/consensus"
	"civora/internal/core"
	"civora/internal/governance"
	"civora/internal/network"
	"encoding/hex"
	"encoding/json"
	"os"
)

func NetworkFixtures() (map[string][]byte, error) {
	raw, e := os.ReadFile("spec/fixtures/accounts.json")
	if e != nil {
		return nil, e
	}
	var account struct {
		Genesis core.Config `json:"genesis"`
	}
	if e = json.Unmarshal(raw, &account); e != nil {
		return nil, e
	}
	keys := network.Trust(account.Genesis.ChainID).Keys
	keys[6] = hex.EncodeToString(network.TestKey(account.Genesis.ChainID, "consensus-next", 6).PubKey().Bytes())
	rotation := network.Genesis{Kind: "ledger", Data: codec.Must(account.Genesis), Rotations: []consensus.Rotation{{Height: "4", Keys: keys, Guardians: []governance.Guardian{}, Authorization: "development_genesis_fixture"}}}
	raw, e = os.ReadFile("spec/fixtures/activation.json")
	if e != nil {
		return nil, e
	}
	var activation struct {
		Genesis core.Config `json:"genesis"`
	}
	if e = json.Unmarshal(raw, &activation); e != nil {
		return nil, e
	}
	a := &activation.Genesis.Activations[0]
	a.Plan.Activation.NotBeforeMS = "0"
	a.Plan.Activation.ExpiresAtMS = "253402300799999"
	a.Plan.Activation.NoticeMS = "0"
	a.Instance.ArmWindowStartMS = "0"
	a.Instance.ArmWindowEndMS = "0"
	a.Instance.ExpiresAtMS = "253402300799999"
	a.PlanID = codec.Hash("upgrade/plan", a.Plan)
	a.Instance.PlanID = a.PlanID
	a.InstanceID = codec.Hash("upgrade/instance", a.Instance)
	// This intentionally broad development window allows a native BFT clock.
	// It is a genesis fixture, not an approval or arming protocol.
	version := network.Genesis{Kind: "ledger", Data: codec.Must(activation.Genesis)}
	zip, e := ZIP215Fixture(account.Genesis.ChainID)
	if e != nil {
		return nil, e
	}
	return map[string][]byte{"rotation.app.json": codec.Must(rotation), "activation.app.json": codec.Must(version), "zip215.json": zip}, nil
}
