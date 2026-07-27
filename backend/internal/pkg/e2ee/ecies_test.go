package e2ee

import (
	"bytes"
	"encoding/hex"
	"testing"

	"github.com/decred/dcrd/dcrec/secp256k1/v4"
	"github.com/stretchr/testify/require"
)

func TestECIESRoundTrip(t *testing.T) {
	priv, err := secp256k1.GeneratePrivateKey()
	require.NoError(t, err)
	pub := priv.PubKey().SerializeUncompressed()
	plaintext := []byte(`{"model":"claude-sonnet-4","messages":[{"role":"user","content":"你好 hello"}]}`)

	for _, aad := range [][]byte{AADRequest, AADResponse} {
		wire, err := Encrypt(pub, plaintext, aad)
		require.NoError(t, err)
		require.Len(t, wire[:65], 65)
		require.Equal(t, byte(0x04), wire[0])

		got, err := Decrypt(priv, wire, aad)
		require.NoError(t, err)
		require.Equal(t, plaintext, got)
	}
}

func TestECIESTamperFails(t *testing.T) {
	priv, err := secp256k1.GeneratePrivateKey()
	require.NoError(t, err)
	pub := priv.PubKey().SerializeUncompressed()
	wire, err := Encrypt(pub, []byte("tamper me"), AADRequest)
	require.NoError(t, err)

	// Flip one bit in each region: nonce, ciphertext, tag.
	for _, idx := range []int{65, 77, len(wire) - 1} {
		mutated := append([]byte(nil), wire...)
		mutated[idx] ^= 0x01
		_, err := Decrypt(priv, mutated, AADRequest)
		require.Error(t, err, "bit flip at %d must fail", idx)
	}

	// Wrong AAD direction must fail.
	_, err = Decrypt(priv, wire, AADResponse)
	require.Error(t, err)

	// Wrong recipient key must fail.
	otherPriv, err := secp256k1.GeneratePrivateKey()
	require.NoError(t, err)
	_, err = Decrypt(otherPriv, wire, AADRequest)
	require.Error(t, err)

	// Truncated wire must fail.
	_, err = Decrypt(priv, wire[:minWire-1], AADRequest)
	require.Error(t, err)
}

func TestParsePublicKeyRejectsBadKeys(t *testing.T) {
	priv, err := secp256k1.GeneratePrivateKey()
	require.NoError(t, err)

	_, err = ParsePublicKey(priv.PubKey().SerializeCompressed()) // 33B compressed
	require.Error(t, err)

	bad := priv.PubKey().SerializeUncompressed()
	bad[64] ^= 0x01 // off-curve
	_, err = ParsePublicKey(bad)
	require.Error(t, err)

	_, err = ParsePublicKey(nil)
	require.Error(t, err)
}

// Cross-implementation vectors generated with the exact deploy/attestor
// stack (@noble/curves + @noble/hashes + @noble/ciphers, same code as
// server.mjs eciesEncrypt). Regenerate with a script like:
//
//	const wire = eciesEncrypt(recipientPub, utf8(plaintext), utf8(aad))
//
// run inside deploy/attestor. Go must decrypt them byte-for-byte.
func TestECIESInteropWithNobleJS(t *testing.T) {
	vectors := []struct {
		name          string
		aad           []byte
		plaintext     string
		recipientPriv string
		wire          string
	}{
		{
			name:          "request direction (AAD v1|req|)",
			aad:           AADRequest,
			plaintext:     `{"model":"claude-sonnet-4","messages":[{"role":"user","content":"E2EE 互操作向量 interop ✓"}],"stream":false}`,
			recipientPriv: "77c3556a9c52b89eba6ff237f597b3be2b294b9486143a292cde38d7b24b8e5c",
			wire:          "04dd7ec287e9a0e702ae145ab755e3dffcfc478cfea7055cf68725372d99cb00d2d46917e82749021f809f23c6a87db8c5508b0b3d4837228d09dfb44022d4dd722978cb0c067d20ce80abe922220db827b4036b08e49c9ea82c5e7e581588a6b81d16710deb27359456263db78ce48d3f54052de4cb53a12f34f69f8ec31ab22b09a071e81427f17f70f63b468d6ec66e9591afdf751e6fe5bacc28b3898970faf310bdd2eddf74e6b8deb2e73cfa55f70d18e60dad8aa0a24ffa7d8143d9c1ed98165e422b71339e21d3ce1661989132",
		},
		{
			name:          "response direction (AAD v1|resp|)",
			aad:           AADResponse,
			plaintext:     `{"id":"chatcmpl-interop","object":"chat.completion","choices":[{"message":{"role":"assistant","content":"响应方向 interop vector ✓"}}]}`,
			recipientPriv: "6c14c7c3a117c116498a9161e30b497cffbfd59309d2616a4701dc6754b3d502",
			wire:          "04f5fdd81d23c3f2c17458f905b2f32a16637e1ec850cb1aeb2e894aa21f24b6cc4bb2c467960ddf8ce01e2725dc145116735f9011f38a3940782821d5afd2c1460e2016ff70431b5db926df3e19a40bec79d3b3329f192f7116bb31f3a2f14272dbaa05611fd01092521847399d7a3778b802b9ac10215ca743b49c715d4d2a898b1cc599d785c3023ad561864ea475e6ba032c2c6b4e26f35a6a0f0a25cd2961af2bc0da4ed55b8706caecd05096f05b1d95ca2efa35084c16bc9c7b2cbd08d875c56b741bcff7c0d2e0480cb8ae5cf71bc686172a763f300356df92aa277aededaa9ef5da9545c318",
		},
	}
	for _, v := range vectors {
		t.Run(v.name, func(t *testing.T) {
			privBytes, err := hex.DecodeString(v.recipientPriv)
			require.NoError(t, err)
			wire, err := hex.DecodeString(v.wire)
			require.NoError(t, err)

			priv := secp256k1.PrivKeyFromBytes(privBytes)
			got, err := Decrypt(priv, wire, v.aad)
			require.NoError(t, err)
			require.Equal(t, v.plaintext, string(got))

			// Wrong AAD must fail (binds the direction tag cross-impl too).
			wrong := AADResponse
			if bytes.Equal(v.aad, AADResponse) {
				wrong = AADRequest
			}
			_, err = Decrypt(priv, wire, wrong)
			require.Error(t, err)
		})
	}
}

// Normalization parity with server.mjs:
// while (!secp256k1.utils.isValidPrivateKey(d)) d = sha256(d).
// Expected values generated with @noble in deploy/attestor.
func TestPrivateKeyFromSeedNormalizationParity(t *testing.T) {
	cases := []struct {
		name    string
		seedHex string
		privHex string
		pubHex  string
	}{
		{
			// all-0xff >= curve order -> one sha256 round
			name:    "seed above curve order",
			seedHex: "ffffffffffffffffffffffffffffffffffffffffffffffffffffffffffffffff",
			privHex: "af9613760f72635fbdb44a5a0a63c39f12af30f950a6ee5c971be188e89c4051",
			pubHex:  "04d7546508bc40c907f4a0e2de2c086cf2160c5112ec2f19ae35925cfedf02044624a52b5e756e6db09ba586fb7c224fd200d3755a4a2b08a83f80afa2b62dead1",
		},
		{
			name:    "zero seed",
			seedHex: "0000000000000000000000000000000000000000000000000000000000000000",
			privHex: "66687aadf862bd776c8fc18b8e9f8e20089714856ee233b3902a591d0d5f2925",
			pubHex:  "04ee0b1602eb18fef7986887a7e8769a30c9df981d33c8380d255edef003abdcd243a0eb74afdf6740e6c423e62aec631519a24cf5b1d62bf8a3e06ddc695dcb77",
		},
		{
			// already valid -> unchanged
			name:    "valid seed passes through",
			seedHex: "0000000000000000000000000000000000000000000000000000000000000001",
			privHex: "0000000000000000000000000000000000000000000000000000000000000001",
			pubHex:  "0479be667ef9dcbbac55a06295ce870b07029bfcdb2dce28d959f2815b16f81798483ada7726a3c4655da4fbfc0e1108a8fd17b448a68554199c47d08ffb10d4b8",
		},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			seed, err := hex.DecodeString(tc.seedHex)
			require.NoError(t, err)
			priv, err := PrivateKeyFromSeed(seed)
			require.NoError(t, err)
			require.Equal(t, tc.privHex, hex.EncodeToString(priv.Serialize()))
			require.Equal(t, tc.pubHex, hex.EncodeToString(priv.PubKey().SerializeUncompressed()))
		})
	}

	_, err := PrivateKeyFromSeed([]byte("short"))
	require.Error(t, err)
}
