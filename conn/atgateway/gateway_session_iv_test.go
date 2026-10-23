package conn

import (
	"bytes"
	"testing"

	v2 "github.com/atframework/robot-go/conn/atgateway/atframework/gateway/v2"
)

func TestReceiveIVContinuesAfterHandshakeAndReset(t *testing.T) {
	for _, algorithm := range []v2.CryptoAlgorithmT{
		v2.CryptoAes256Gcm, v2.CryptoAes256Cbc,
		v2.CryptoChacha20Poly1305Ietf, v2.CryptoXchacha20Poly1305Ietf, v2.CryptoXxtea,
	} {
		config := &GatewaySessionConfig{KeyExchange: v2.KeyExchangeX25519, SupportedAlgorithms: []v2.CryptoAlgorithmT{algorithm}}
		session := NewGatewaySession(config)
		// A new handshake on the same client must replace the old receive state.
		for round := 0; round < 2; round++ {
			if _, err := session.BuildKeyExchangeReq(); err != nil {
				t.Fatal(err)
			}
			serverKey, publicKey, err := GenerateECDHKeyPair(config.KeyExchange)
			if err != nil {
				t.Fatal(err)
			}
			secret, err := ComputeECDHSharedSecret(serverKey, session.selfPublicKey, config.KeyExchange)
			if err != nil {
				t.Fatal(err)
			}
			params, err := GetCryptoAlgorithmParams(algorithm)
			if err != nil {
				t.Fatal(err)
			}
			key, _, err := DeriveKeyMaterial(secret, params.KeySize, params.IVSize)
			if err != nil {
				t.Fatal(err)
			}
			serverCipher, err := NewCipherSuite(algorithm, key)
			if err != nil {
				t.Fatal(err)
			}
			iv := bytes.Repeat([]byte{byte(0x41 + round)}, params.IVSize)
			aad := []byte("handshake and stream AAD")
			token := bytes.Repeat([]byte{0x65}, 32)
			encryptedToken, err := serverCipher.Encrypt(token, iv, aad)
			if err != nil {
				t.Fatal(err)
			}
			hs := &v2.HandshakeInfo{
				Step: v2.HandshakeKeyExchangeRsp, SessionID: 123, HandshakeSequence: uint64(round + 2),
				KeyExchange: config.KeyExchange, PublicKey: publicKey, Algorithms: []v2.CryptoAlgorithmT{algorithm},
				InitializeCrypto: &v2.CryptoInfo{Algorithm: algorithm, IV: iv, AAD: aad}, SessionToken: encryptedToken,
			}
			if _, err := session.HandleKeyExchangeRsp(v2.BuildHandshakeMessage(1, hs)); err != nil {
				t.Fatal(err)
			}
			if !bytes.Equal(session.sessionToken, token) {
				t.Fatal("handshake token differs")
			}
			RollIV(GetIVRollPolicy(algorithm), iv, encryptedToken)
			for packet := 0; packet < 3; packet++ {
				var transmittedIV []byte
				if packet == 1 {
					iv = bytes.Repeat([]byte{0x73}, params.IVSize)
					transmittedIV = append([]byte(nil), iv...)
				}
				payload := bytes.Repeat([]byte{byte(0x31 + packet)}, 48)
				encrypted, err := serverCipher.Encrypt(payload, iv, aad)
				if err != nil {
					t.Fatal(err)
				}
				post := &v2.PostInfo{Length: uint64(len(payload)), Data: encrypted,
					Crypto: &v2.CryptoInfo{Algorithm: algorithm, IV: transmittedIV, AAD: aad}}
				if err := session.decodePostPayload(post); err != nil {
					t.Fatalf("algorithm %d round %d packet %d: %v", algorithm, round, packet, err)
				}
				if !bytes.Equal(post.Data, payload) {
					t.Fatalf("algorithm %d round %d packet %d: payload differs", algorithm, round, packet)
				}
				RollIV(GetIVRollPolicy(algorithm), iv, encrypted)
			}
		}
	}
}
