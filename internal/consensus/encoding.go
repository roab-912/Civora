package consensus

import "encoding/base64"

func encode(b []byte) string { return base64.RawURLEncoding.EncodeToString(b) }
