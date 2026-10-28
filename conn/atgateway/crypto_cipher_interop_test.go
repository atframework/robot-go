package conn

import (
	"bytes"
	"encoding/hex"
	"testing"

	v2 "github.com/atframework/robot-go/conn/atgateway/atframework/gateway/v2"
)

func TestXXTEAMatchesGatewayVectors(t *testing.T) {
	// These atframe_utils vectors use big-endian key words and little-endian data words.
	key := []byte{1, 2, 3, 4, 5, 6, 7, 8, 9, 10, 11, 12, 13, 14, 15, 16}
	suite, err := NewCipherSuite(v2.CryptoXxtea, key)
	if err != nil {
		t.Fatal(err)
	}
	for _, vector := range []struct{ plaintext, ciphertext, padded string }{
		{"", "46a8b94900f2ad20", "0808080808080808"},
		{"00", "e1e14bfba767369c", "0007070707070707"},
		{"0001", "ba40d25ca4269023", "0001060606060606"},
		{"000102", "701e9534349cba2b", "0001020505050505"},
		{"00010203", "ae7653d2be482915", "0001020304040404"},
		{"0001020304", "04d63b6b0e308ad6", "0001020304030303"},
		{"00010203040506", "96f3a1698fafe307", "0001020304050601"},
		{"0001020304050607", "8497d8f5baa1b4cd", "0001020304050607"},
		{"000102030405060708", "f94946d79cf9e7746befa3a8763e0300", "00010203040506070807070707070707"},
		{"000102030405060708090a0b", "1d20ad3d8487fafca2cd512364a457bb", "000102030405060708090a0b04040404"},
		{"000102030405060708090a0b0c0d0e0f", "ba0409fb2637424d2d66093ae447a3bb", "000102030405060708090a0b0c0d0e0f"},
		{"0708090a0b0c0d0e0f101112131415", "2b30336c11d0c5bf1efba14a02343cbf", "0708090a0b0c0d0e0f10111213141501"},
	} {
		t.Run(vector.plaintext, func(t *testing.T) {
			plaintext, err := hex.DecodeString(vector.plaintext)
			if err != nil {
				t.Fatal(err)
			}
			padded, err := hex.DecodeString(vector.padded)
			if err != nil {
				t.Fatal(err)
			}
			ciphertext, err := hex.DecodeString(vector.ciphertext)
			if err != nil {
				t.Fatal(err)
			}
			actual, err := suite.Encrypt(plaintext, nil, nil)
			if err != nil || !bytes.Equal(ciphertext, actual) {
				t.Fatalf("encrypt: got %x, want %x, error %v", actual, ciphertext, err)
			}
			decrypted, err := suite.Decrypt(ciphertext, nil, nil)
			if err != nil || !bytes.Equal(padded, decrypted) {
				t.Fatalf("decrypt: got %x, plaintext %x, error %v", decrypted, plaintext, err)
			}
		})
	}
}

func TestXXTEARejectsInvalidCiphertext(t *testing.T) {
	suite, err := NewCipherSuite(v2.CryptoXxtea, make([]byte, 16))
	if err != nil {
		t.Fatal(err)
	}
	for _, size := range []int{0, 4, 7, 9, 12} {
		if plaintext, err := suite.Decrypt(make([]byte, size), nil, nil); err == nil || len(plaintext) != 0 {
			t.Errorf("ciphertext length %d: got %x, error %v", size, plaintext, err)
		}
	}
}
