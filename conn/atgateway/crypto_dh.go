package conn

import (
	"crypto/ecdh"
	"crypto/rand"
	"fmt"

	v2 "github.com/atframework/robot-go/conn/atgateway/atframework/gateway/v2"
)

// encodeECDHPublicKeyForWire matches atgateway's OpenSSL ECDH public-key format:
// a 1-byte length prefix followed by the TLS encoded point bytes.
func encodeECDHPublicKeyForWire(publicKey []byte) ([]byte, error) {
	if len(publicKey) == 0 {
		return nil, nil
	}
	if len(publicKey) > 0xff {
		return nil, fmt.Errorf("ECDH public key too large for wire format: %d", len(publicKey))
	}

	encoded := make([]byte, len(publicKey)+1)
	encoded[0] = byte(len(publicKey))
	copy(encoded[1:], publicKey)
	return encoded, nil
}

// decodeECDHPublicKeyFromWire accepts the atgateway wire format and also tolerates
// raw public keys to keep interop resilient during local debugging.
func decodeECDHPublicKeyFromWire(peerPublicKeyBytes []byte) ([]byte, error) {
	if len(peerPublicKeyBytes) == 0 {
		return nil, fmt.Errorf("empty peer public key")
	}

	pointLen := int(peerPublicKeyBytes[0])
	if pointLen > 0 && pointLen+1 == len(peerPublicKeyBytes) {
		decoded := make([]byte, pointLen)
		copy(decoded, peerPublicKeyBytes[1:])
		return decoded, nil
	}

	decoded := make([]byte, len(peerPublicKeyBytes))
	copy(decoded, peerPublicKeyBytes)
	return decoded, nil
}

// ECDHCurveFromKeyExchange maps a protocol key-exchange enum to the corresponding
// Go standard library ECDH curve.
func ECDHCurveFromKeyExchange(ke v2.KeyExchangeT) (ecdh.Curve, error) {
	switch ke {
	case v2.KeyExchangeNone:
		return nil, nil
	case v2.KeyExchangeX25519:
		return ecdh.X25519(), nil
	case v2.KeyExchangeSecp256r1:
		return ecdh.P256(), nil
	case v2.KeyExchangeSecp384r1:
		return ecdh.P384(), nil
	case v2.KeyExchangeSecp521r1:
		return ecdh.P521(), nil
	default:
		return nil, fmt.Errorf("unsupported key exchange type: %d", ke)
	}
}

// GenerateECDHKeyPair generates a new ECDH key pair for the given key-exchange type.
// Returns the private key and the serialized public key bytes.
func GenerateECDHKeyPair(ke v2.KeyExchangeT) (*ecdh.PrivateKey, []byte, error) {
	if ke == v2.KeyExchangeNone {
		return nil, nil, nil
	}

	curve, err := ECDHCurveFromKeyExchange(ke)
	if err != nil {
		return nil, nil, err
	}

	privKey, err := curve.GenerateKey(rand.Reader)
	if err != nil {
		return nil, nil, fmt.Errorf("generate ECDH key pair: %w", err)
	}

	encodedPublicKey, err := encodeECDHPublicKeyForWire(privKey.PublicKey().Bytes())
	if err != nil {
		return nil, nil, err
	}

	return privKey, encodedPublicKey, nil
}

// ComputeECDHSharedSecret performs the ECDH key agreement using the local private key
// and the remote peer's public key bytes. Returns the raw shared secret.
func ComputeECDHSharedSecret(privKey *ecdh.PrivateKey, peerPublicKeyBytes []byte, ke v2.KeyExchangeT) ([]byte, error) {
	if ke == v2.KeyExchangeNone {
		return nil, nil
	}

	curve, err := ECDHCurveFromKeyExchange(ke)
	if err != nil {
		return nil, err
	}

	decodedPeerPublicKey, err := decodeECDHPublicKeyFromWire(peerPublicKeyBytes)
	if err != nil {
		return nil, err
	}

	peerPub, err := curve.NewPublicKey(decodedPeerPublicKey)
	if err != nil {
		return nil, fmt.Errorf("parse peer public key: %w", err)
	}

	secret, err := privKey.ECDH(peerPub)
	if err != nil {
		return nil, fmt.Errorf("compute ECDH shared secret: %w", err)
	}

	return secret, nil
}
