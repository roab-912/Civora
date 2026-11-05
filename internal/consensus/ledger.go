package consensus

import (
	"civora/internal/codec"
	"civora/internal/core"
	"civora/internal/governance"
	"civora/internal/replica"
	"civora/internal/state"
	"fmt"
	abci "github.com/cometbft/cometbft/abci/types"
)

type Ledger struct {
	Engine *core.Engine
	events []core.Event
}

func (l *Ledger) Events() []abci.Event { return CivicEvents(l.events) }

func (l *Ledger) Entries() []state.Entry { return l.Engine.Snapshot().Entries }

func RotateEngine(engine *core.Engine, guardians []governance.Guardian) (*core.Engine, error) {
	if engine.Governance == nil || engine.Governance.LivePolls() > 0 {
		return nil, fmt.Errorf("guardian rotation unavailable")
	}
	cfg := *engine.Config.Governance
	cfg.Guardians = guardians
	if _, e := governance.New(cfg); e != nil {
		return nil, e
	}
	for _, g := range guardians {
		a, ok := engine.Accounts[g.AccountID]
		if !ok || a.Status != "active" || a.SigningPK != g.SigningPK {
			return nil, fmt.Errorf("guardian must be an active civic account")
		}
	}
	n := engine.Clone()
	epoch, e := codec.Uint(n.Governance.Network.GuardianEpoch)
	if e != nil || epoch == ^uint64(0) {
		return nil, fmt.Errorf("guardian epoch overflow")
	}
	n.Governance.Network.GuardianEpoch = fmt.Sprint(epoch + 1)
	n.Governance.Network.Guardians = append([]governance.Guardian{}, guardians...)
	return n, nil
}
func (l *Ledger) RotateGuardians(g []governance.Guardian) (replica.Machine, error) {
	n, e := RotateEngine(l.Engine, g)
	if e != nil {
		return nil, e
	}
	return &Ledger{Engine: n, events: l.events}, nil
}

func (l *Ledger) Height() uint64 { return l.Engine.Height }
func (l *Ledger) Time() uint64   { return l.Engine.Time }
func (l *Ledger) State() []byte  { return codec.Must(l.Engine.Snapshot()) }
func (l *Ledger) Root() string   { return l.Engine.Root() }
func (l *Ledger) Active() bool {
	return l.Engine.Governance != nil && l.Engine.Governance.LivePolls() > 0
}
func (l *Ledger) Check(tx []byte) error { return l.Engine.CheckTx(tx) }
func (l *Ledger) Apply(b replica.Block) (replica.Machine, error) {
	block := core.Block{Height: b.Height, TimeMS: b.TimeMS, Transactions: []core.Transaction{}}
	for _, encoded := range b.Transactions {
		raw, e := codec.Blob(encoded)
		if e != nil {
			return nil, e
		}
		var tx core.Transaction
		if e = codec.Decode(raw, "transaction", &tx); e != nil {
			return nil, e
		}
		block.Transactions = append(block.Transactions, tx)
	}
	next, events, e := l.Engine.Apply(codec.Must(block))
	if e != nil {
		return nil, e
	}
	return &Ledger{Engine: next, events: events}, nil
}
