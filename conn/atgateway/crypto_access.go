package conn

import (
	"crypto/hmac"
	"crypto/rand"
	"crypto/sha256"
	"encoding/binary"
	"encoding/hex"
	"fmt"
	"time"

	v2 "github.com/atframework/robot-go/conn/atgateway/atframework/gateway/v2"
)

// MakeAccessDataPlaintext builds the plaintext string used for HMAC-SHA256 signature.
//
// Format: "{timestamp}:{nonce1}-{nonce2}:{session_id}"
// If keyExchange != kNone, appends: ":{key_exchange_int}:{hex(sha256(public_key))}"
// If sessionToken is non-empty, appends: ":{hex(sha256(session_token))}"
func MakeAccessDataPlaintext(
	sessionID uint64,
	timestamp int64,
	nonce1, nonce2 uint64,
	keyExchange v2.KeyExchangeT,
	publicKey, sessionToken []byte,
) string {
	plaintext := fmt.Sprintf("%d:%d-%d:%d", timestamp, nonce1, nonce2, sessionID)

	if keyExchange != v2.KeyExchangeNone {
		h := sha256.Sum256(publicKey)
		plaintext += fmt.Sprintf(":%d:%s", int(keyExchange), hex.EncodeToString(h[:]))
	}

	if len(sessionToken) > 0 {
		h := sha256.Sum256(sessionToken)
		plaintext += fmt.Sprintf(":%s", hex.EncodeToString(h[:]))
	}

	return plaintext
}

// CalculateAccessDataSignature computes HMAC-SHA256(accessToken, plaintext).
func CalculateAccessDataSignature(accessToken []byte, plaintext string) []byte {
	mac := hmac.New(sha256.New, accessToken)
	mac.Write([]byte(plaintext))
	return mac.Sum(nil)
}

// GenerateAccessData creates the timestamp, nonces, and HMAC-SHA256 signatures
// for the access_data entries in a handshake message.
// One signature is generated per access token.
func GenerateAccessData(
	accessTokens [][]byte,
	sessionID uint64,
	keyExchange v2.KeyExchangeT,
	publicKey, sessionToken []byte,
) (timestamp int64, nonce1, nonce2 uint64, signatures [][]byte, err error) {
	timestamp = time.Now().Unix()

	var buf [16]byte
	if _, err = rand.Read(buf[:]); err != nil {
		return 0, 0, 0, nil, fmt.Errorf("generate random nonces: %w", err)
	}
	nonce1 = binary.LittleEndian.Uint64(buf[:8])
	nonce2 = binary.LittleEndian.Uint64(buf[8:])

	plaintext := MakeAccessDataPlaintext(sessionID, timestamp, nonce1, nonce2,
		keyExchange, publicKey, sessionToken)

	signatures = make([][]byte, len(accessTokens))
	for i, token := range accessTokens {
		signatures[i] = CalculateAccessDataSignature(token, plaintext)
	}

	return timestamp, nonce1, nonce2, signatures, nil
}
