package citizendev

import (
	"civora/internal/codec"
	"civora/internal/network"
	"encoding/hex"
	"fmt"
)

func Keys(chain string) map[string][]byte {
	out := map[string][]byte{}
	for _, role := range []string{"archive", "audit"} {
		for i := 0; i < 5; i++ {
			key := network.TestKey(chain, role, i)
			out[fmt.Sprintf("%s-%d.TEST_ONLY.json", role, i)] = codec.Must(map[string]any{"label": "PUBLIC_TEST_ONLY", "seed": hex.EncodeToString(key[:32]), "public_key": hex.EncodeToString(key.PubKey().Bytes())})
		}
	}
	return out
}
