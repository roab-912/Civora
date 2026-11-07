package recovery

import (
	"civora/internal/codec"
	"civora/internal/state"
	"encoding/json"
	"fmt"
)

const HoldBudget = 4

// Config is opted into by genesis, never learned from an incoming order.
// Profile names the application profile protected by this root.
type Config struct {
	Root    Root   `json:"root"`
	Pin     string `json:"root_sha3_384"`
	Profile string `json:"protected_profile"`
}

func (c Config) ValidateGenesis(network, profile string) error {
	if err := validate("recovery_config", c); err != nil {
		return err
	}
	// This first integration is explicitly confined to development genesis.
	if c.Root.Network != network || c.Root.Mode != "development_TEST_ONLY" || c.Profile != profile || c.Pin != RootID(c.Root) {
		return fmt.Errorf("ERR_RECOVERY_GENESIS")
	}
	start, err := codec.Uint(c.Root.NotBefore)
	if err != nil {
		return err
	}
	return c.Root.Validate(start, true)
}

type LedgerState struct {
	Counter string   `json:"last_counter"`
	Orders  []Bundle `json:"orders"`
}

func NewLedger() *LedgerState { return &LedgerState{Counter: "0", Orders: []Bundle{}} }
func (s *LedgerState) Entries() []state.Entry {
	if s == nil {
		return nil
	}
	out := []state.Entry{{Table: "security", Key: "recovery", Value: map[string]any{"last_counter": s.Counter}}}
	for _, b := range s.Orders {
		out = append(out, state.Entry{Table: "security_orders", Key: b.Order.Counter, Value: b})
	}
	return out
}
func (s *LedgerState) Clone() *LedgerState {
	if s == nil {
		return nil
	}
	var n LedgerState
	_ = json.Unmarshal(codec.Must(s), &n)
	return &n
}
func (s *LedgerState) Accept(c *Config, b Bundle, now uint64) error {
	if c == nil || s == nil {
		return fmt.Errorf("ERR_RECOVERY_NOT_CONFIGURED")
	}
	if b.Order.Type != "security.hold" {
		return fmt.Errorf("ERR_RECOVERY_ACTION_UNAVAILABLE")
	}
	if len(s.Orders) >= 10000 {
		return fmt.Errorf("ERR_RECOVERY_CAPACITY")
	}
	last, err := codec.Uint(s.Counter)
	if err != nil {
		return err
	}
	if err = VerifyOnChain(c.Root, b, now, last, true); err != nil {
		return err
	}
	s.Counter = b.Order.Counter
	s.Orders = append(s.Orders, b)
	return nil
}

// A module put under security review cannot authorize new operations merely
// because the pause expired. New, safely approved module identities are needed.
func (s *LedgerState) ModuleBlocked(id string) bool {
	if s == nil {
		return false
	}
	for _, b := range s.Orders {
		if b.Order.ScopeKind == "module" && b.Order.ScopeID == id {
			return true
		}
	}
	return false
}
func (s *LedgerState) Blocked(kind, id string, now uint64) bool {
	if s == nil {
		return false
	}
	for _, b := range s.Orders {
		o := b.Order
		start, _ := codec.Uint(o.Starts)
		end, _ := codec.Uint(o.Expires)
		if o.ScopeKind == kind && o.ScopeID == id && start <= now && now < end {
			return true
		}
	}
	return false
}

// IsCommand only selects priority. Strict parsing and quorum verification must
// still succeed before the transaction can affect a candidate block.
func IsCommand(raw []byte) bool {
	var v struct {
		Type string `json:"type"`
	}
	return len(raw) <= 256*1024 && json.Unmarshal(raw, &v) == nil && v.Type == "security.hold"
}
func DecodeCommand(raw []byte) (Bundle, error) {
	var c struct {
		Type  string `json:"type"`
		Value Bundle `json:"value"`
	}
	err := codec.Decode(raw, "security_hold_tx", &c)
	if err == nil && c.Value.Order.Type != c.Type {
		err = fmt.Errorf("ERR_RECOVERY_TYPE")
	}
	return c.Value, err
}
func Priority(raw []byte) int {
	if IsCommand(raw) {
		return 0
	}
	return 1
}
