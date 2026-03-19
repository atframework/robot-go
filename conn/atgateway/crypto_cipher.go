package conn

import (
	"crypto/aes"
	"crypto/cipher"
	"encoding/binary"
	"errors"
	"fmt"

	"golang.org/x/crypto/chacha20poly1305"

	v2 "github.com/atframework/robot-go/conn/atgateway/atframework/gateway/v2"
)

// ========================= IV Roll Policy =========================

// IVRollPolicy defines how IV/nonce is updated after each encrypt/decrypt operation.
type IVRollPolicy int

const (
	IVRollNone            IVRollPolicy = iota // No IV rolling (XXTEA).
	IVRollAEADInc1BE                          // Increment IV as big-endian integer by 1 (GCM, Poly1305).
	IVRollChainCiphertext                     // Replace IV with last iv_size bytes of ciphertext (CBC).
)

// GetIVRollPolicy returns the IV rolling policy for the given crypto algorithm.
func GetIVRollPolicy(algo v2.CryptoAlgorithmT) IVRollPolicy {
	switch algo {
	case v2.CryptoAes128Gcm, v2.CryptoAes192Gcm, v2.CryptoAes256Gcm,
		v2.CryptoChacha20Poly1305Ietf, v2.CryptoXchacha20Poly1305Ietf:
		return IVRollAEADInc1BE
	case v2.CryptoAes128Cbc, v2.CryptoAes192Cbc, v2.CryptoAes256Cbc:
		return IVRollChainCiphertext
	default:
		return IVRollNone
	}
}

// RollIV updates the IV in-place according to the given policy.
// For IVRollChainCiphertext, ciphertext is the encrypted data whose tail replaces the IV.
// For IVRollAEADInc1BE, ciphertext is ignored.
func RollIV(policy IVRollPolicy, iv, ciphertext []byte) {
	switch policy {
	case IVRollAEADInc1BE:
		incrementIVBE(iv)
	case IVRollChainCiphertext:
		if len(ciphertext) >= len(iv) {
			copy(iv, ciphertext[len(ciphertext)-len(iv):])
		}
	}
}

// incrementIVBE treats the IV as a big-endian unsigned integer and increments it by 1.
func incrementIVBE(iv []byte) {
	for i := len(iv) - 1; i >= 0; i-- {
		iv[i]++
		if iv[i] != 0 {
			break
		}
	}
}

// ========================= CipherSuite Interface =========================

// CipherSuite provides stateless encrypt/decrypt operations for a specific algorithm.
// The IV is passed per-call; the caller is responsible for IV state management and rolling.
type CipherSuite interface {
	Encrypt(plaintext, iv, aad []byte) (ciphertext []byte, err error)
	Decrypt(ciphertext, iv, aad []byte) (plaintext []byte, err error)
	Params() CryptoAlgorithmParams
}

// NewCipherSuite creates a CipherSuite for the given algorithm and key.
func NewCipherSuite(algo v2.CryptoAlgorithmT, key []byte) (CipherSuite, error) {
	params, err := GetCryptoAlgorithmParams(algo)
	if err != nil {
		return nil, err
	}
	if len(key) != params.KeySize {
		return nil, fmt.Errorf("key size mismatch: got %d, want %d", len(key), params.KeySize)
	}

	switch algo {
	case v2.CryptoAes128Gcm, v2.CryptoAes192Gcm, v2.CryptoAes256Gcm:
		return newAESGCMCipher(key, params)
	case v2.CryptoAes128Cbc, v2.CryptoAes192Cbc, v2.CryptoAes256Cbc:
		return newAESCBCCipher(key, params)
	case v2.CryptoChacha20Poly1305Ietf:
		return newChacha20Poly1305Cipher(key, params)
	case v2.CryptoXchacha20Poly1305Ietf:
		return newXChacha20Poly1305Cipher(key, params)
	case v2.CryptoXxtea:
		return newXXTEACipher(key, params)
	case v2.CryptoChacha20:
		return nil, fmt.Errorf("chacha20 stream cipher is not supported (use chacha20-poly1305 instead)")
	default:
		return nil, fmt.Errorf("unsupported crypto algorithm: %d", algo)
	}
}

// ========================= AES-GCM =========================

type aesGCMCipher struct {
	aead   cipher.AEAD
	params CryptoAlgorithmParams
}

func newAESGCMCipher(key []byte, params CryptoAlgorithmParams) (*aesGCMCipher, error) {
	block, err := aes.NewCipher(key)
	if err != nil {
		return nil, fmt.Errorf("create AES block cipher: %w", err)
	}
	aead, err := cipher.NewGCM(block)
	if err != nil {
		return nil, fmt.Errorf("create GCM: %w", err)
	}
	return &aesGCMCipher{aead: aead, params: params}, nil
}

func (c *aesGCMCipher) Encrypt(plaintext, iv, aad []byte) ([]byte, error) {
	return c.aead.Seal(nil, iv, plaintext, aad), nil
}

func (c *aesGCMCipher) Decrypt(ciphertext, iv, aad []byte) ([]byte, error) {
	return c.aead.Open(nil, iv, ciphertext, aad)
}

func (c *aesGCMCipher) Params() CryptoAlgorithmParams { return c.params }

// ========================= AES-CBC with PKCS7 =========================

type aesCBCCipher struct {
	block  cipher.Block
	params CryptoAlgorithmParams
}

func newAESCBCCipher(key []byte, params CryptoAlgorithmParams) (*aesCBCCipher, error) {
	block, err := aes.NewCipher(key)
	if err != nil {
		return nil, fmt.Errorf("create AES block cipher: %w", err)
	}
	return &aesCBCCipher{block: block, params: params}, nil
}

func (c *aesCBCCipher) Encrypt(plaintext, iv, _ []byte) ([]byte, error) {
	// Pad to block alignment only (no extra block when already aligned).
	// Matches C++ behavior: padding_value = padded_size - in.size(), which is 0 when aligned.
	padded := blockAlignPad(plaintext, c.block.BlockSize())
	ct := make([]byte, len(padded))
	cipher.NewCBCEncrypter(c.block, iv).CryptBlocks(ct, padded)
	return ct, nil
}

func (c *aesCBCCipher) Decrypt(ciphertext, iv, _ []byte) ([]byte, error) {
	if len(ciphertext) == 0 || len(ciphertext)%c.block.BlockSize() != 0 {
		return nil, errors.New("ciphertext length is not a multiple of block size")
	}
	pt := make([]byte, len(ciphertext))
	cipher.NewCBCDecrypter(c.block, iv).CryptBlocks(pt, ciphertext)
	// Do NOT PKCS7-unpad here. The caller uses post.Length to trim output,
	// matching C++ decode_post which trims to original_size.
	return pt, nil
}

func (c *aesCBCCipher) Params() CryptoAlgorithmParams { return c.params }

// ========================= ChaCha20-Poly1305 IETF =========================

type chacha20Poly1305Cipher struct {
	aead   cipher.AEAD
	params CryptoAlgorithmParams
}

func newChacha20Poly1305Cipher(key []byte, params CryptoAlgorithmParams) (*chacha20Poly1305Cipher, error) {
	aead, err := chacha20poly1305.New(key)
	if err != nil {
		return nil, fmt.Errorf("create chacha20-poly1305: %w", err)
	}
	return &chacha20Poly1305Cipher{aead: aead, params: params}, nil
}

func (c *chacha20Poly1305Cipher) Encrypt(plaintext, iv, aad []byte) ([]byte, error) {
	return c.aead.Seal(nil, iv, plaintext, aad), nil
}

func (c *chacha20Poly1305Cipher) Decrypt(ciphertext, iv, aad []byte) ([]byte, error) {
	return c.aead.Open(nil, iv, ciphertext, aad)
}

func (c *chacha20Poly1305Cipher) Params() CryptoAlgorithmParams { return c.params }

// ========================= XChaCha20-Poly1305 IETF =========================

type xchacha20Poly1305Cipher struct {
	aead   cipher.AEAD
	params CryptoAlgorithmParams
}

func newXChacha20Poly1305Cipher(key []byte, params CryptoAlgorithmParams) (*xchacha20Poly1305Cipher, error) {
	aead, err := chacha20poly1305.NewX(key)
	if err != nil {
		return nil, fmt.Errorf("create xchacha20-poly1305: %w", err)
	}
	return &xchacha20Poly1305Cipher{aead: aead, params: params}, nil
}

func (c *xchacha20Poly1305Cipher) Encrypt(plaintext, iv, aad []byte) ([]byte, error) {
	return c.aead.Seal(nil, iv, plaintext, aad), nil
}

func (c *xchacha20Poly1305Cipher) Decrypt(ciphertext, iv, aad []byte) ([]byte, error) {
	return c.aead.Open(nil, iv, ciphertext, aad)
}

func (c *xchacha20Poly1305Cipher) Params() CryptoAlgorithmParams { return c.params }

// ========================= XXTEA =========================

const xxteaDelta = 0x9e3779b9

type xxteaCipher struct {
	key    [4]uint32
	params CryptoAlgorithmParams
}

func newXXTEACipher(key []byte, params CryptoAlgorithmParams) (*xxteaCipher, error) {
	if len(key) != 16 {
		return nil, fmt.Errorf("XXTEA key must be 16 bytes, got %d", len(key))
	}
	c := &xxteaCipher{params: params}
	c.key[0] = binary.LittleEndian.Uint32(key[0:4])
	c.key[1] = binary.LittleEndian.Uint32(key[4:8])
	c.key[2] = binary.LittleEndian.Uint32(key[8:12])
	c.key[3] = binary.LittleEndian.Uint32(key[12:16])
	return c, nil
}

func (c *xxteaCipher) Encrypt(plaintext, _, _ []byte) ([]byte, error) {
	// Pad to uint32 alignment (4 bytes), minimum 8 bytes (2 uint32s required by XXTEA).
	paddedLen := (len(plaintext) + 3) &^ 3
	if paddedLen < 8 {
		paddedLen = 8
	}
	buf := make([]byte, paddedLen)
	copy(buf, plaintext)

	v := bytesToUint32sLE(buf)
	xxteaEncryptBlock(v, c.key)
	return uint32sToBytesLE(v), nil
}

func (c *xxteaCipher) Decrypt(ciphertext, _, _ []byte) ([]byte, error) {
	if len(ciphertext) < 8 || len(ciphertext)%4 != 0 {
		return nil, errors.New("XXTEA ciphertext must be >= 8 bytes and 4-byte aligned")
	}
	v := bytesToUint32sLE(ciphertext)
	xxteaDecryptBlock(v, c.key)
	return uint32sToBytesLE(v), nil
}

func (c *xxteaCipher) Params() CryptoAlgorithmParams { return c.params }

func xxteaEncryptBlock(v []uint32, key [4]uint32) {
	n := len(v)
	if n < 2 {
		return
	}
	rounds := 6 + 52/uint32(n)
	var sum uint32
	z := v[n-1]
	for ; rounds > 0; rounds-- {
		sum += xxteaDelta
		e := (sum >> 2) & 3
		for p := 0; p < n-1; p++ {
			y := v[p+1]
			v[p] += xxteaMX(sum, y, z, uint32(p), e, key)
			z = v[p]
		}
		y := v[0]
		v[n-1] += xxteaMX(sum, y, z, uint32(n-1), e, key)
		z = v[n-1]
	}
}

func xxteaDecryptBlock(v []uint32, key [4]uint32) {
	n := len(v)
	if n < 2 {
		return
	}
	rounds := 6 + 52/uint32(n)
	sum := rounds * xxteaDelta
	y := v[0]
	for ; rounds > 0; rounds-- {
		e := (sum >> 2) & 3
		for p := n - 1; p > 0; p-- {
			z := v[p-1]
			v[p] -= xxteaMX(sum, y, z, uint32(p), e, key)
			y = v[p]
		}
		z := v[n-1]
		v[0] -= xxteaMX(sum, y, z, 0, e, key)
		y = v[0]
		sum -= xxteaDelta
	}
}

func xxteaMX(sum, y, z, p, e uint32, key [4]uint32) uint32 {
	return ((z>>5 ^ y<<2) + (y>>3 ^ z<<4)) ^ ((sum ^ y) + (key[(p&3)^e] ^ z))
}

func bytesToUint32sLE(data []byte) []uint32 {
	n := len(data) / 4
	result := make([]uint32, n)
	for i := range n {
		result[i] = binary.LittleEndian.Uint32(data[i*4:])
	}
	return result
}

func uint32sToBytesLE(v []uint32) []byte {
	result := make([]byte, len(v)*4)
	for i, val := range v {
		binary.LittleEndian.PutUint32(result[i*4:], val)
	}
	return result
}

// ========================= PKCS7 Padding =========================

// blockAlignPad pads data to the next multiple of blockSize.
// If data is already aligned, no padding is added (unlike PKCS7 which adds a full block).
// Padding bytes use PKCS7 values (1-15), matching C++ encrypt_data behavior.
func blockAlignPad(data []byte, blockSize int) []byte {
	remainder := len(data) % blockSize
	if remainder == 0 {
		return data
	}
	padding := blockSize - remainder
	padded := make([]byte, len(data)+padding)
	copy(padded, data)
	for i := len(data); i < len(padded); i++ {
		padded[i] = byte(padding)
	}
	return padded
}
