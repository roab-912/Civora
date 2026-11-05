package consensus

import (
	"civora/internal/core"
	abci "github.com/cometbft/cometbft/abci/types"
)

func CivicEvents(events []core.Event) []abci.Event {
	out := []abci.Event{}
	for _, e := range events {
		out = append(out, abci.Event{Type: e.Type, Attributes: []abci.EventAttribute{{Key: "id", Value: e.ID, Index: true}, {Key: "subject", Value: e.Subject, Index: true}}})
	}
	return out
}
