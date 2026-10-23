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
	for _, vector := range []struct{ plaintext, ciphertext string }{
		{"0001020304050607", "8497d8f5baa1b4cd"},
		{"0708090a0b0c0d0e0f101112131415", "2b30336c11d0c5bf1efba14a02343cbf"},
	} {
		plaintext, err := hex.DecodeString(vector.plaintext)
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
		if err != nil || !bytes.Equal(blockAlignPad(plaintext, 4), decrypted) {
			t.Fatalf("decrypt: got %x, plaintext %x, error %v", decrypted, plaintext, err)
		}
	}
}
