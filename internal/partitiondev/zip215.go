package partitiondev

import (
	"civora/internal/codec"
	"civora/internal/network"
	"crypto/ed25519"
	"crypto/sha512"
	"encoding/hex"
	"fmt"
	cmtjson "github.com/cometbft/cometbft/libs/json"
	"github.com/cometbft/cometbft/types"
	"math/big"
)

// ZIP215Fixture uses an unreduced encoding of the identity for R. This is a
// deliberately unusual TEST_ONLY signature accepted by native ZIP-215. It
// distinguishes native commit verification from strict OpenSSL Ed25519.
func ZIP215Fixture(chain string) ([]byte, error) {
	trust := network.Trust(chain)
	encoded, e := SignedHeader(trust, 9, 9001, codec.Hash("TEST_ONLY/appHash", nil), 5)
	if e != nil {
		return nil, e
	}
	raw, e := codec.Blob(encoded)
	if e != nil {
		return nil, e
	}
	var sh types.SignedHeader
	if e = cmtjson.Unmarshal(raw, &sh); e != nil {
		return nil, e
	}
	sig := &sh.Commit.Signatures[0]
	var private []byte
	for seat := 0; seat < 7; seat++ {
		key := network.TestKey(chain, "consensus", seat)
		if hex.EncodeToString(key.PubKey().Address()) == hex.EncodeToString(sig.ValidatorAddress) {
			private = key
			break
		}
	}
	if len(private) != 64 {
		return nil, fmt.Errorf("test signing key")
	}
	vote := types.Vote{Type: 2, Height: sh.Height, Round: sh.Commit.Round, BlockID: sh.Commit.BlockID, Timestamp: sig.Timestamp, ValidatorAddress: sig.ValidatorAddress, ValidatorIndex: 0}
	message := types.VoteSignBytes(chain, vote.ToProto())
	little := func(b []byte) *big.Int {
		r := append([]byte{}, b...)
		for i, j := 0, len(r)-1; i < j; i, j = i+1, j-1 {
			r[i], r[j] = r[j], r[i]
		}
		return new(big.Int).SetBytes(r)
	}
	order, _ := new(big.Int).SetString("7237005577332262213973186563042994240857116359379907606001950938285454250989", 10)
	h := sha512.Sum512(private[:32])
	h[0] &= 248
	h[31] &= 63
	h[31] |= 64
	scalar := little(h[:32])
	r := make([]byte, 32)
	for i := range r {
		r[i] = 255
	}
	r[0] = 238
	r[31] = 127
	bytes := append(append(append([]byte{}, r...), private[32:]...), message...)
	challenge := sha512.Sum512(bytes)
	s := new(big.Int).Mod(new(big.Int).Mul(little(challenge[:]), scalar), order)
	sb := s.FillBytes(make([]byte, 32))
	for i, j := 0, len(sb)-1; i < j; i, j = i+1, j-1 {
		sb[i], sb[j] = sb[j], sb[i]
	}
	sig.Signature = append(r, sb...)
	key := network.TestKey(chain, "consensus", 0).PubKey()
	for seat := 0; seat < 7; seat++ {
		k := network.TestKey(chain, "consensus", seat).PubKey()
		if hex.EncodeToString(k.Bytes()) == hex.EncodeToString(private[32:]) {
			key = k
			break
		}
	}
	if !key.VerifySignature(message, sig.Signature) || ed25519.Verify(ed25519.PublicKey(private[32:]), message, sig.Signature) {
		return nil, fmt.Errorf("ZIP-215 boundary vector did not separate verifiers")
	}
	raw, e = cmtjson.Marshal(sh)
	if e != nil {
		return nil, e
	}
	return codec.Must(map[string]any{"label": "PUBLIC_TEST_ONLY_ZIP215_UNREDUCED_IDENTITY_R", "trust": trust, "signed_header": Encode(raw), "validator_public_key": hex.EncodeToString(private[32:]), "signature": hex.EncodeToString(sig.Signature), "message": Encode(message)}), nil
}
