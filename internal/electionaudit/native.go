package electionaudit

import (
	"civora/internal/codec"
	"encoding/base64"
	"fmt"
	cmtjson "github.com/cometbft/cometbft/libs/json"
	"github.com/cometbft/cometbft/types"
)

func encode(b []byte) string { return base64.RawURLEncoding.EncodeToString(b) }
func nativePosition(encoded string) (uint64, error) {
	raw, e := codec.Blob(encoded)
	if e != nil || len(raw) > 400*1024 {
		return 0, fmt.Errorf("transaction proof bound")
	}
	var p types.TxProof
	if e = cmtjson.Unmarshal(raw, &p); e != nil {
		return 0, e
	}
	if p.Proof.Index < 0 {
		return 0, fmt.Errorf("transaction position")
	}
	return uint64(p.Proof.Index), nil
}
