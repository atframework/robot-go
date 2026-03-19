package conn

import (
	"crypto/ecdh"
	"crypto/rand"
	"errors"
	"fmt"
	"sync/atomic"

	v2 "github.com/atframework/robot-go/conn/atgateway/atframework/gateway/v2"
)

// GatewaySessionConfig holds the configuration for an atgateway protocol session.
type GatewaySessionConfig struct {
	AccessTokens         [][]byte
	KeyExchange          v2.KeyExchangeT
	SupportedAlgorithms  []v2.CryptoAlgorithmT
	SupportedCompression []v2.CompressionAlgorithmT
	CompressionThreshold uint64
}

// GatewaySession manages the atgateway v2 protocol state, including
// handshake, key derivation, encryption, and message encoding/decoding.
type GatewaySession struct {
	config *GatewaySessionConfig

	// Session state from handshake.
	sessionID         uint64
	sessionToken      []byte
	handshakeSequence uint64

	// ECDH state (populated during handshake).
	privateKey    *ecdh.PrivateKey
	selfPublicKey []byte

	// Negotiated parameters from server response.
	selectedAlgorithm   v2.CryptoAlgorithmT
	selectedCompression v2.CompressionAlgorithmT

	// Crypto state.
	cipher       CipherSuite
	sendIV       []byte
	recvIV       []byte
	ivRollPolicy IVRollPolicy

	// Compression.
	compressor Compressor

	// Limits from server.
	maxPostMessageSize uint64

	// State flags.
	handshakeDone atomic.Bool

	// Monotonic sequence counter.
	sequence atomic.Uint64
}

// NewGatewaySession creates a new protocol session with the given config.
func NewGatewaySession(config *GatewaySessionConfig) *GatewaySession {
	return &GatewaySession{config: config}
}

// IsHandshakeDone returns true after a successful handshake.
func (s *GatewaySession) IsHandshakeDone() bool {
	return s.handshakeDone.Load()
}

// SessionID returns the server-assigned session ID.
func (s *GatewaySession) SessionID() uint64 {
	return s.sessionID
}

// nextSeq returns the next sequence number for outgoing messages.
func (s *GatewaySession) nextSeq() uint64 {
	return s.sequence.Add(1)
}

// ========================= Handshake =========================

// BuildKeyExchangeReq generates an ECDH key pair and builds the initial
// handshake request frame (kKeyExchangeReq).
func (s *GatewaySession) BuildKeyExchangeReq() ([]byte, error) {
	var (
		privKey *ecdh.PrivateKey
		pubKey  []byte
		err     error
	)
	if s.config.KeyExchange != v2.KeyExchangeNone {
		privKey, pubKey, err = GenerateECDHKeyPair(s.config.KeyExchange)
		if err != nil {
			return nil, fmt.Errorf("generate ECDH key pair: %w", err)
		}
	}
	s.privateKey = privKey
	s.selfPublicKey = pubKey

	// Generate HMAC access data signatures.
	var accessData []v2.AccessDataInfo
	if len(s.config.AccessTokens) > 0 {
		timestamp, nonce1, nonce2, signatures, err := GenerateAccessData(
			s.config.AccessTokens, 0, s.config.KeyExchange, pubKey, nil)
		if err != nil {
			return nil, fmt.Errorf("generate access data: %w", err)
		}
		accessData = make([]v2.AccessDataInfo, len(signatures))
		for i, sig := range signatures {
			accessData[i] = v2.AccessDataInfo{
				Algorithm: v2.AccessDataHmacSha256,
				Timestamp: timestamp,
				Nonce1:    nonce1,
				Nonce2:    nonce2,
				Signature: sig,
			}
		}
	}

	hs := &v2.HandshakeInfo{
		Step:                  v2.HandshakeKeyExchangeReq,
		KeyExchange:           s.config.KeyExchange,
		PublicKey:             pubKey,
		Algorithms:            s.config.SupportedAlgorithms,
		KdfTypes:              []v2.KdfAlgorithmT{v2.KdfHkdfSha256},
		AccessData:            accessData,
		CompressionAlgorithms: s.config.SupportedCompression,
	}

	fbData := v2.BuildHandshakeMessage(s.nextSeq(), hs)
	return EncodeFrame(fbData), nil
}

// HandleKeyExchangeRsp processes the server's handshake response, derives
// key material, creates cipher suites, decrypts the session token, and
// returns a confirm frame to send back to the server.
func (s *GatewaySession) HandleKeyExchangeRsp(frameData []byte) (confirmFrame []byte, err error) {
	msg, err := v2.ParseClientMessage(frameData)
	if err != nil {
		return nil, fmt.Errorf("parse handshake response: %w", err)
	}
	if msg.Type == v2.MsgTypeKickoff && msg.Kickoff != nil {
		return nil, &KickoffError{
			Reason:    msg.Kickoff.Reason,
			SubReason: msg.Kickoff.SubReason,
			Message:   msg.Kickoff.Message,
		}
	}
	if msg.Type != v2.MsgTypeHandshake || msg.Handshake == nil {
		return nil, fmt.Errorf("expected handshake message, got type %d", msg.Type)
	}

	hs := msg.Handshake
	if hs.Step != v2.HandshakeKeyExchangeRsp {
		return nil, fmt.Errorf("expected KeyExchangeRsp step, got %d", hs.Step)
	}

	// Store session info.
	s.sessionID = hs.SessionID
	s.handshakeSequence = hs.HandshakeSequence

	// Pick negotiated algorithm (server selects, first entry).
	if len(hs.Algorithms) > 0 {
		s.selectedAlgorithm = hs.Algorithms[0]
	}
	if len(hs.CompressionAlgorithms) > 0 {
		s.selectedCompression = hs.CompressionAlgorithms[0]
	}
	s.maxPostMessageSize = hs.MaxPostMessageSize

	// Compute ECDH shared secret.
	var key []byte
	var baseIV []byte
	if s.selectedAlgorithm != v2.CryptoNone {
		sharedSecret, err := ComputeECDHSharedSecret(s.privateKey, hs.PublicKey, s.config.KeyExchange)
		if err != nil {
			return nil, fmt.Errorf("ECDH key agreement: %w", err)
		}

		// Derive key material via HKDF-SHA256.
		params, err := GetCryptoAlgorithmParams(s.selectedAlgorithm)
		if err != nil {
			return nil, fmt.Errorf("get algorithm params: %w", err)
		}
		key, baseIV, err = DeriveKeyMaterial(sharedSecret, params.KeySize, params.IVSize)
		if err != nil {
			return nil, fmt.Errorf("derive key material: %w", err)
		}
	}

	// Create cipher suite.
	if s.selectedAlgorithm != v2.CryptoNone {
		s.cipher, err = NewCipherSuite(s.selectedAlgorithm, key)
		if err != nil {
			return nil, fmt.Errorf("create cipher suite: %w", err)
		}
		s.ivRollPolicy = GetIVRollPolicy(s.selectedAlgorithm)
	}

	// Decrypt session token using initialize_crypto IV/AAD (one-shot, separate from main IV chain).
	if len(hs.SessionToken) > 0 && s.cipher != nil && hs.InitializeCrypto != nil {
		s.sessionToken, err = s.cipher.Decrypt(
			hs.SessionToken, hs.InitializeCrypto.IV, hs.InitializeCrypto.AAD)
		if err != nil {
			return nil, fmt.Errorf("decrypt session token: %w", err)
		}
	}

	// Initialize send/recv IVs from HKDF-derived base IV.
	if len(baseIV) > 0 {
		s.sendIV = make([]byte, len(baseIV))
		copy(s.sendIV, baseIV)
		s.recvIV = make([]byte, len(baseIV))
		copy(s.recvIV, baseIV)
	}

	// The server already used its send IV (= base IV) to encrypt the session token,
	// so its send IV has been rolled. Advance recvIV to match.
	if len(hs.SessionToken) > 0 && s.cipher != nil && len(s.recvIV) > 0 {
		RollIV(s.ivRollPolicy, s.recvIV, hs.SessionToken)
	}

	// Create compressor if needed.
	if s.selectedCompression != v2.CompressionNone {
		s.compressor, err = NewCompressor(s.selectedCompression)
		if err != nil {
			return nil, fmt.Errorf("create compressor: %w", err)
		}
	}

	// Build and return the confirm frame.
	fbData := v2.BuildConfirmMessage(s.nextSeq(), s.sessionID, s.handshakeSequence)
	confirmFrame = EncodeFrame(fbData)

	s.handshakeDone.Store(true)
	return confirmFrame, nil
}

// BuildKickoff creates a protocol-level close notification for graceful shutdown.
func (s *GatewaySession) BuildKickoff(reason, subReason int32, message string) ([]byte, error) {
	if !s.handshakeDone.Load() {
		return nil, errors.New("handshake not complete")
	}

	fbData := v2.BuildKickoffMessage(s.nextSeq(), reason, subReason, message)
	return EncodeFrame(fbData), nil
}

// ========================= Data Encoding/Decoding =========================

// EncodePost compresses and encrypts the application payload, then wraps it
// in a cs_body_post FlatBuffers message frame.
func (s *GatewaySession) EncodePost(payload []byte) ([]byte, error) {
	if !s.handshakeDone.Load() {
		return nil, errors.New("handshake not complete")
	}

	// Enforce max_post_message_size.
	if s.maxPostMessageSize > 0 && uint64(len(payload)) > s.maxPostMessageSize {
		return nil, errors.New("message too large")
	}

	data := payload
	var compInfo *v2.CompressionInfo

	// Compress if above threshold and compression actually reduces size.
	if s.compressor != nil && s.config.CompressionThreshold > 0 &&
		uint64(len(data)) >= s.config.CompressionThreshold {
		compressed, err := s.compressor.Compress(data)
		if err != nil {
			return nil, fmt.Errorf("compress: %w", err)
		}
		if len(compressed) < len(data) {
			// C++ stores compressed data size (post-compression) in original_size.
			compInfo = &v2.CompressionInfo{
				Type:         s.selectedCompression,
				OriginalSize: uint64(len(compressed)),
			}
			data = compressed
		}
	}

	// Encrypt.
	var cryptoInfo *v2.CryptoInfo
	if s.cipher != nil && s.selectedAlgorithm != v2.CryptoNone {
		// Snapshot current send IV for this message.
		iv := make([]byte, len(s.sendIV))
		copy(iv, s.sendIV)

		// Generate random AAD for AEAD ciphers.
		var aad []byte
		if s.cipher.Params().IsAEAD {
			aad = make([]byte, 16)
			if _, err := rand.Read(aad); err != nil {
				return nil, fmt.Errorf("generate AAD: %w", err)
			}
		}

		encrypted, err := s.cipher.Encrypt(data, iv, aad)
		if err != nil {
			return nil, fmt.Errorf("encrypt: %w", err)
		}

		// Roll the send IV after successful encryption.
		RollIV(s.ivRollPolicy, s.sendIV, encrypted)

		cryptoInfo = &v2.CryptoInfo{
			Algorithm: s.selectedAlgorithm,
			IV:        iv,
			AAD:       aad,
		}
		data = encrypted
	}

	post := &v2.PostInfo{
		Crypto:      cryptoInfo,
		Compression: compInfo,
		Length:      uint64(len(payload)),
		Data:        data,
	}

	fbData := v2.BuildPostMessage(s.nextSeq(), post)
	return EncodeFrame(fbData), nil
}

// DecodeMessage parses a FlatBuffers frame, and for kPost messages performs
// decryption and decompression, returning the parsed message with the
// application-level payload in Post.Data.
func (s *GatewaySession) DecodeMessage(frameData []byte) (*v2.ParsedMessage, error) {
	msg, err := v2.ParseClientMessage(frameData)
	if err != nil {
		return nil, fmt.Errorf("parse message: %w", err)
	}

	if msg.Post != nil {
		if err := s.decodePostPayload(msg.Post); err != nil {
			return nil, err
		}
	}

	return msg, nil
}

// decodePostPayload decrypts and decompresses a post message's data in-place.
func (s *GatewaySession) decodePostPayload(post *v2.PostInfo) error {
	data := post.Data

	// Decrypt if encrypted.
	if post.Crypto != nil && post.Crypto.Algorithm != v2.CryptoNone && s.cipher != nil {
		// Use the IV from the message (the sender's IV at time of encryption).
		iv := post.Crypto.IV
		if len(iv) == 0 {
			// Fallback to tracked recvIV.
			iv = s.recvIV
		}
		decrypted, err := s.cipher.Decrypt(data, iv, post.Crypto.AAD)
		if err != nil {
			return fmt.Errorf("decrypt post: %w", err)
		}
		// Roll recv IV to stay in sync with the sender's send IV.
		RollIV(s.ivRollPolicy, s.recvIV, data)
		data = decrypted
	}

	// Decompress if compressed.
	if post.Compression != nil && post.Compression.Type != v2.CompressionNone && s.compressor != nil {
		// C++ decode_post: trim decrypted data to compression.original_size before decompression.
		// compression.original_size stores the compressed data size (post-compression, pre-encryption).
		if uint64(len(data)) > post.Compression.OriginalSize {
			data = data[:post.Compression.OriginalSize]
		}
		// Use post.Length (original uncompressed size) as the decompress output buffer hint.
		decompressed, err := s.compressor.Decompress(data, int(post.Length))
		if err != nil {
			return fmt.Errorf("decompress post: %w", err)
		}
		data = decompressed
	}

	// Trim padding (matches C++ decode_post final trim to original_size).
	if uint64(len(data)) > post.Length {
		data = data[:post.Length]
	}

	post.Data = data
	return nil
}

// ========================= Control Messages =========================

// BuildPing builds a ping frame with the given timepoint.
func (s *GatewaySession) BuildPing(timepoint int64) []byte {
	fbData := v2.BuildPingMessage(s.nextSeq(), timepoint)
	return EncodeFrame(fbData)
}

// BuildPong builds a pong frame echoing the given timepoint.
func (s *GatewaySession) BuildPong(timepoint int64) []byte {
	fbData := v2.BuildPongMessage(s.nextSeq(), timepoint)
	return EncodeFrame(fbData)
}
