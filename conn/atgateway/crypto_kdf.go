package conn

import (
	"crypto/hkdf"
	"crypto/sha256"
	"fmt"

	v2 "github.com/atframework/robot-go/conn/atgateway/atframework/gateway/v2"
)

// CryptoAlgorithmParams describes the key, IV, block, and tag sizes
// for a specific crypto algorithm.
type CryptoAlgorithmParams struct {
	KeySize   int  // Key size in bytes.
	IVSize    int  // IV/Nonce size in bytes.
	BlockSize int  // Block size in bytes (1 for stream/AEAD ciphers).
	TagSize   int  // Authentication tag size in bytes (0 for non-AEAD).
	IsAEAD    bool // Whether the algorithm provides authenticated encryption.
}

// GetCryptoAlgorithmParams returns the algorithm parameters for the given crypto algorithm.
func GetCryptoAlgorithmParams(algo v2.CryptoAlgorithmT) (CryptoAlgorithmParams, error) {
	switch algo {
	case v2.CryptoXxtea:
		return CryptoAlgorithmParams{KeySize: 16, IVSize: 0, BlockSize: 4, IsAEAD: false}, nil
	case v2.CryptoAes128Cbc:
		return CryptoAlgorithmParams{KeySize: 16, IVSize: 16, BlockSize: 16, IsAEAD: false}, nil
	case v2.CryptoAes192Cbc:
		return CryptoAlgorithmParams{KeySize: 24, IVSize: 16, BlockSize: 16, IsAEAD: false}, nil
	case v2.CryptoAes256Cbc:
		return CryptoAlgorithmParams{KeySize: 32, IVSize: 16, BlockSize: 16, IsAEAD: false}, nil
	case v2.CryptoAes128Gcm:
		return CryptoAlgorithmParams{KeySize: 16, IVSize: 12, BlockSize: 1, TagSize: 16, IsAEAD: true}, nil
	case v2.CryptoAes192Gcm:
		return CryptoAlgorithmParams{KeySize: 24, IVSize: 12, BlockSize: 1, TagSize: 16, IsAEAD: true}, nil
	case v2.CryptoAes256Gcm:
		return CryptoAlgorithmParams{KeySize: 32, IVSize: 12, BlockSize: 1, TagSize: 16, IsAEAD: true}, nil
	case v2.CryptoChacha20:
		return CryptoAlgorithmParams{KeySize: 32, IVSize: 16, BlockSize: 1, IsAEAD: false}, nil
	case v2.CryptoChacha20Poly1305Ietf:
		return CryptoAlgorithmParams{KeySize: 32, IVSize: 12, BlockSize: 1, TagSize: 16, IsAEAD: true}, nil
	case v2.CryptoXchacha20Poly1305Ietf:
		return CryptoAlgorithmParams{KeySize: 32, IVSize: 24, BlockSize: 1, TagSize: 16, IsAEAD: true}, nil
	default:
		return CryptoAlgorithmParams{}, fmt.Errorf("unsupported crypto algorithm: %d", algo)
	}
}

// DeriveKeyMaterial uses HKDF-SHA256 to derive a key and IV from the ECDH shared secret.
// Per the atgateway v2 protocol: salt = nil, info = "" (empty).
func DeriveKeyMaterial(sharedSecret []byte, keySize, ivSize int) (key, iv []byte, err error) {
	totalLen := keySize + ivSize
	if totalLen == 0 {
		return nil, nil, nil
	}

	material, err := hkdf.Key(sha256.New, sharedSecret, nil, "", totalLen)
	if err != nil {
		return nil, nil, fmt.Errorf("HKDF key derivation: %w", err)
	}

	key = material[:keySize]
	if ivSize > 0 {
		iv = material[keySize:]
	}
	return key, iv, nil
}
