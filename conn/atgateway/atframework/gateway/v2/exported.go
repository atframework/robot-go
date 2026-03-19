package v2

import (
	"errors"
	"fmt"

	flatbuffers "github.com/google/flatbuffers/go"
)

// ========================= Exported Type Aliases =========================

type KeyExchangeT = key_exchange_t
type CryptoAlgorithmT = crypto_algorithm_t
type CompressionAlgorithmT = compression_algorithm_t
type HandshakeStepT = handshake_step_t
type ClientMessageTypeT = client_message_type_t
type ClientMessageBodyT = client_message_body
type KdfAlgorithmT = kdf_algorithm_t
type AccessDataAlgorithmT = access_data_algorithm_t
type ErrorCodeT = error_code_t
type CompressionLevelT = compression_level_t

// ========================= Exported Enum Constants =========================

// Key exchange algorithms.
const (
	KeyExchangeNone      KeyExchangeT = key_exchange_tkNone
	KeyExchangeX25519    KeyExchangeT = key_exchange_tkX25519
	KeyExchangeSecp256r1 KeyExchangeT = key_exchange_tkSecp256r1
	KeyExchangeSecp384r1 KeyExchangeT = key_exchange_tkSecp384r1
	KeyExchangeSecp521r1 KeyExchangeT = key_exchange_tkSecp521r1
)

// Crypto algorithms.
const (
	CryptoNone                  CryptoAlgorithmT = crypto_algorithm_tkNone
	CryptoXxtea                 CryptoAlgorithmT = crypto_algorithm_tkXxtea
	CryptoAes128Cbc             CryptoAlgorithmT = crypto_algorithm_tkAes128Cbc
	CryptoAes192Cbc             CryptoAlgorithmT = crypto_algorithm_tkAes192Cbc
	CryptoAes256Cbc             CryptoAlgorithmT = crypto_algorithm_tkAes256Cbc
	CryptoAes128Gcm             CryptoAlgorithmT = crypto_algorithm_tkAes128Gcm
	CryptoAes192Gcm             CryptoAlgorithmT = crypto_algorithm_tkAes192Gcm
	CryptoAes256Gcm             CryptoAlgorithmT = crypto_algorithm_tkAes256Gcm
	CryptoChacha20              CryptoAlgorithmT = crypto_algorithm_tkChacha20
	CryptoChacha20Poly1305Ietf  CryptoAlgorithmT = crypto_algorithm_tkChacha20Poly1305Ietf
	CryptoXchacha20Poly1305Ietf CryptoAlgorithmT = crypto_algorithm_tkXchacha20Poly1305Ietf
)

// Compression algorithms.
const (
	CompressionNone   CompressionAlgorithmT = compression_algorithm_tkNone
	CompressionZstd   CompressionAlgorithmT = compression_algorithm_tkZstd
	CompressionLz4    CompressionAlgorithmT = compression_algorithm_tkLz4
	CompressionSnappy CompressionAlgorithmT = compression_algorithm_tkSnappy
	CompressionZlib   CompressionAlgorithmT = compression_algorithm_tkZlib
)

// Compression levels.
const (
	CompressionLevelDefault   CompressionLevelT = compression_level_tkDefault
	CompressionLevelStorage   CompressionLevelT = compression_level_tkStorage
	CompressionLevelFast      CompressionLevelT = compression_level_tkFast
	CompressionLevelLowCpu    CompressionLevelT = compression_level_tkLowCpu
	CompressionLevelBalanced  CompressionLevelT = compression_level_tkBalanced
	CompressionLevelHighRatio CompressionLevelT = compression_level_tkHighRatio
	CompressionLevelMaxRatio  CompressionLevelT = compression_level_tkMaxRatio
)

// Handshake steps.
const (
	HandshakeKeyExchangeReq HandshakeStepT = handshake_step_tkKeyExchangeReq
	HandshakeKeyExchangeRsp HandshakeStepT = handshake_step_tkKeyExchangeRsp
	HandshakeReconnectReq   HandshakeStepT = handshake_step_tkReconnectReq
	HandshakeReconnectRsp   HandshakeStepT = handshake_step_tkReconnectRsp
)

// Client message types.
const (
	MsgTypeUnknown   ClientMessageTypeT = client_message_type_tkUnknown
	MsgTypePost      ClientMessageTypeT = client_message_type_tkPost
	MsgTypeHandshake ClientMessageTypeT = client_message_type_tkHandshake
	MsgTypePing      ClientMessageTypeT = client_message_type_tkPing
	MsgTypePong      ClientMessageTypeT = client_message_type_tkPong
	MsgTypeKickoff   ClientMessageTypeT = client_message_type_tkKickoff
	MsgTypeConfirm   ClientMessageTypeT = client_message_type_tkConfirm
)

// Client message body union discriminants.
const (
	BodyNone      ClientMessageBodyT = client_message_bodyNONE
	BodyPost      ClientMessageBodyT = client_message_bodycs_body_post
	BodyKickoff   ClientMessageBodyT = client_message_bodycs_body_kickoff
	BodyPing      ClientMessageBodyT = client_message_bodycs_body_ping
	BodyHandshake ClientMessageBodyT = client_message_bodycs_body_handshake
	BodyConfirm   ClientMessageBodyT = client_message_bodycs_body_confirm
)

// KDF algorithms.
const (
	KdfHkdfSha256 KdfAlgorithmT = kdf_algorithm_tkHkdfSha256
)

// Access data algorithms.
const (
	AccessDataHmacSha256 AccessDataAlgorithmT = access_data_algorithm_tkHmacSha256
)

// Error codes.
const (
	ErrCodeSuccess         ErrorCodeT = error_code_tkSuccess
	ErrCodeFirstIdel       ErrorCodeT = error_code_tkFirstIdel
	ErrCodeHandshake       ErrorCodeT = error_code_tkHandshake
	ErrCodeBusy            ErrorCodeT = error_code_tkBusy
	ErrCodeSessionExpired  ErrorCodeT = error_code_tkSessionExpired
	ErrCodeRefuseReconnect ErrorCodeT = error_code_tkRefuseReconnect
)

// ========================= Parsed Data Types =========================

// CryptoInfo holds crypto parameters for a post message or initialize_crypto.
type CryptoInfo struct {
	Algorithm CryptoAlgorithmT
	IV        []byte
	AAD       []byte
}

// CompressionInfo holds compression parameters for a post message.
type CompressionInfo struct {
	Type         CompressionAlgorithmT
	OriginalSize uint64
}

// AccessDataInfo holds a single access_data entry from a handshake message.
type AccessDataInfo struct {
	Algorithm AccessDataAlgorithmT
	Timestamp int64
	Nonce1    uint64
	Nonce2    uint64
	Signature []byte
}

// HandshakeInfo holds all parsed fields from a handshake message body.
type HandshakeInfo struct {
	SessionID             uint64
	Step                  HandshakeStepT
	KeyExchange           KeyExchangeT
	KdfTypes              []KdfAlgorithmT
	Algorithms            []CryptoAlgorithmT
	PublicKey             []byte
	AccessData            []AccessDataInfo
	InitializeCrypto      *CryptoInfo
	CompressionAlgorithms []CompressionAlgorithmT
	MaxPostMessageSize    uint64
	SessionToken          []byte
	HandshakeSequence     uint64
}

// PostInfo holds all parsed fields from a post message body.
type PostInfo struct {
	Crypto      *CryptoInfo
	Compression *CompressionInfo
	Length      uint64
	Data        []byte
}

// KickoffInfo holds parsed fields from a kickoff message body.
type KickoffInfo struct {
	Reason    int32
	SubReason int32
	Message   string
}

// ConfirmInfo holds parsed fields from a confirm message body.
type ConfirmInfo struct {
	SessionID         uint64
	HandshakeSequence uint64
}

// PingInfo holds parsed fields from a ping/pong message body.
type PingInfo struct {
	Timepoint int64
}

// ParsedMessage represents a fully parsed gateway protocol message.
type ParsedMessage struct {
	Type      ClientMessageTypeT
	Sequence  uint64
	Handshake *HandshakeInfo // non-nil for kHandshake
	Post      *PostInfo      // non-nil for kPost
	Ping      *PingInfo      // non-nil for kPing/kPong
	Kickoff   *KickoffInfo   // non-nil for kKickoff
	Confirm   *ConfirmInfo   // non-nil for kConfirm
}

// ========================= Internal Helpers =========================

// readInt8VectorAsBytes reads a [byte] (int8) flatbuffers vector as []byte.
func readInt8VectorAsBytes(tab flatbuffers.Table, slotVOffset flatbuffers.VOffsetT) []byte {
	o := flatbuffers.UOffsetT(tab.Offset(slotVOffset))
	if o == 0 {
		return nil
	}
	length := tab.VectorLen(o)
	if length == 0 {
		return nil
	}
	start := tab.Vector(o)
	result := make([]byte, length)
	copy(result, tab.Bytes[start:start+flatbuffers.UOffsetT(length)])
	return result
}

// ========================= Parser =========================

// HasClientMessageIdentifier checks if the buffer has the "ATGW" file identifier.
func HasClientMessageIdentifier(buf []byte) bool {
	return client_messageBufferHasIdentifier(buf)
}

// ParseClientMessage parses a flatbuffers-encoded client_message.
func ParseClientMessage(buf []byte) (*ParsedMessage, error) {
	if len(buf) < 8 {
		return nil, errors.New("buffer too small for client_message")
	}
	if !client_messageBufferHasIdentifier(buf) {
		return nil, fmt.Errorf("invalid message identifier: expected ATGW")
	}

	msg := GetRootAsclient_message(buf, 0)
	head := msg.Head(nil)
	if head == nil {
		return nil, errors.New("missing message head")
	}

	result := &ParsedMessage{
		Type:     ClientMessageTypeT(head.Type()),
		Sequence: head.Sequence(),
	}

	bodyType := msg.BodyType()
	var bodyTable flatbuffers.Table
	if !msg.Body(&bodyTable) {
		return result, nil
	}

	switch bodyType {
	case client_message_bodycs_body_handshake:
		result.Handshake = parseHandshakeBody(bodyTable)
	case client_message_bodycs_body_post:
		result.Post = parsePostBody(bodyTable)
	case client_message_bodycs_body_ping:
		result.Ping = parsePingBody(bodyTable)
	case client_message_bodycs_body_kickoff:
		result.Kickoff = parseKickoffBody(bodyTable)
	case client_message_bodycs_body_confirm:
		result.Confirm = parseConfirmBody(bodyTable)
	}

	return result, nil
}

func parseHandshakeBody(tab flatbuffers.Table) *HandshakeInfo {
	hs := &cs_body_handshake{}
	hs.Init(tab.Bytes, tab.Pos)

	info := &HandshakeInfo{
		SessionID:          hs.SessionId(),
		Step:               HandshakeStepT(hs.Step()),
		KeyExchange:        KeyExchangeT(hs.KeyExchange()),
		MaxPostMessageSize: hs.MaxPostMessageSize(),
		HandshakeSequence:  hs.HandshakeSequence(),
	}

	// Public key: [byte] at vtable offset 16
	info.PublicKey = readInt8VectorAsBytes(hs._tab, 16)

	// Session token: [byte] at vtable offset 24
	info.SessionToken = readInt8VectorAsBytes(hs._tab, 24)

	// KDF types
	if n := hs.KdfTypeLength(); n > 0 {
		info.KdfTypes = make([]KdfAlgorithmT, n)
		for i := 0; i < n; i++ {
			info.KdfTypes[i] = KdfAlgorithmT(hs.KdfType(i))
		}
	}

	// Algorithms
	if n := hs.AlgorithmsLength(); n > 0 {
		info.Algorithms = make([]CryptoAlgorithmT, n)
		for i := 0; i < n; i++ {
			info.Algorithms[i] = CryptoAlgorithmT(hs.Algorithms(i))
		}
	}

	// Compression algorithms
	if n := hs.CompressionAlgorithmLength(); n > 0 {
		info.CompressionAlgorithms = make([]CompressionAlgorithmT, n)
		for i := 0; i < n; i++ {
			info.CompressionAlgorithms[i] = CompressionAlgorithmT(hs.CompressionAlgorithm(i))
		}
	}

	// Access data
	if n := hs.AccessDataLength(); n > 0 {
		info.AccessData = make([]AccessDataInfo, n)
		for i := 0; i < n; i++ {
			var ad cs_body_handshake_access_data
			if hs.AccessData(&ad, i) {
				info.AccessData[i] = AccessDataInfo{
					Algorithm: AccessDataAlgorithmT(ad.Algorithm()),
					Timestamp: ad.Timestamp(),
					Nonce1:    ad.Nonce1(),
					Nonce2:    ad.Nonce2(),
					Signature: readInt8VectorAsBytes(ad._tab, 12), // signature at vtable offset 12
				}
			}
		}
	}

	// Initialize crypto
	if initCrypto := hs.InitializeCrypto(nil); initCrypto != nil {
		info.InitializeCrypto = &CryptoInfo{
			Algorithm: CryptoAlgorithmT(initCrypto.Algorithm()),
			IV:        readInt8VectorAsBytes(initCrypto._tab, 6), // iv at vtable offset 6
			AAD:       readInt8VectorAsBytes(initCrypto._tab, 8), // aad at vtable offset 8
		}
	}

	return info
}

func parsePostBody(tab flatbuffers.Table) *PostInfo {
	post := &cs_body_post{}
	post.Init(tab.Bytes, tab.Pos)

	info := &PostInfo{
		Length: post.Length(),
		Data:   readInt8VectorAsBytes(post._tab, 10), // data at vtable offset 10
	}

	if cryptoInfo := post.Crypto(nil); cryptoInfo != nil {
		info.Crypto = &CryptoInfo{
			Algorithm: CryptoAlgorithmT(cryptoInfo.Algorithm()),
			IV:        readInt8VectorAsBytes(cryptoInfo._tab, 6),
			AAD:       readInt8VectorAsBytes(cryptoInfo._tab, 8),
		}
	}

	if compInfo := post.Compression(nil); compInfo != nil {
		info.Compression = &CompressionInfo{
			Type:         CompressionAlgorithmT(compInfo.Type()),
			OriginalSize: compInfo.OriginalSize(),
		}
	}

	return info
}

func parsePingBody(tab flatbuffers.Table) *PingInfo {
	ping := &cs_body_ping{}
	ping.Init(tab.Bytes, tab.Pos)
	return &PingInfo{Timepoint: ping.Timepoint()}
}

func parseKickoffBody(tab flatbuffers.Table) *KickoffInfo {
	ko := &cs_body_kickoff{}
	ko.Init(tab.Bytes, tab.Pos)
	return &KickoffInfo{
		Reason:    ko.Reason(),
		SubReason: ko.SubReason(),
		Message:   string(ko.Message()),
	}
}

func parseConfirmBody(tab flatbuffers.Table) *ConfirmInfo {
	cf := &cs_body_confirm{}
	cf.Init(tab.Bytes, tab.Pos)
	return &ConfirmInfo{
		SessionID:         cf.SessionId(),
		HandshakeSequence: cf.HandshakeSequence(),
	}
}

// ========================= Builders =========================

// BuildHandshakeMessage builds a complete flatbuffers client_message for a handshake.
func BuildHandshakeMessage(seq uint64, hs *HandshakeInfo) []byte {
	builder := flatbuffers.NewBuilder(512)

	// Phase 1: Create all byte vectors (must be outside any Start/End block).
	var pubKeyOffset flatbuffers.UOffsetT
	if len(hs.PublicKey) > 0 {
		pubKeyOffset = builder.CreateByteVector(hs.PublicKey)
	}

	var sessionTokenOffset flatbuffers.UOffsetT
	if len(hs.SessionToken) > 0 {
		sessionTokenOffset = builder.CreateByteVector(hs.SessionToken)
	}

	// Signature vectors for access data entries.
	sigOffsets := make([]flatbuffers.UOffsetT, len(hs.AccessData))
	for i := range hs.AccessData {
		if len(hs.AccessData[i].Signature) > 0 {
			sigOffsets[i] = builder.CreateByteVector(hs.AccessData[i].Signature)
		}
	}

	// IV/AAD vectors for initializeCrypto.
	var initCryptoIVOffset, initCryptoAADOffset flatbuffers.UOffsetT
	if hs.InitializeCrypto != nil {
		if len(hs.InitializeCrypto.IV) > 0 {
			initCryptoIVOffset = builder.CreateByteVector(hs.InitializeCrypto.IV)
		}
		if len(hs.InitializeCrypto.AAD) > 0 {
			initCryptoAADOffset = builder.CreateByteVector(hs.InitializeCrypto.AAD)
		}
	}

	// Phase 2: Create access_data table entries.
	adOffsets := make([]flatbuffers.UOffsetT, len(hs.AccessData))
	for i := range hs.AccessData {
		ad := &hs.AccessData[i]
		cs_body_handshake_access_dataStart(builder)
		cs_body_handshake_access_dataAddAlgorithm(builder, access_data_algorithm_t(ad.Algorithm))
		cs_body_handshake_access_dataAddTimestamp(builder, ad.Timestamp)
		cs_body_handshake_access_dataAddNonce1(builder, ad.Nonce1)
		cs_body_handshake_access_dataAddNonce2(builder, ad.Nonce2)
		if len(ad.Signature) > 0 {
			cs_body_handshake_access_dataAddSignature(builder, sigOffsets[i])
		}
		adOffsets[i] = cs_body_handshake_access_dataEnd(builder)
	}

	// Phase 3: Create vectors for handshake fields.
	var accessDataVecOffset flatbuffers.UOffsetT
	if len(adOffsets) > 0 {
		cs_body_handshakeStartAccessDataVector(builder, len(adOffsets))
		for i := len(adOffsets) - 1; i >= 0; i-- {
			builder.PrependUOffsetT(adOffsets[i])
		}
		accessDataVecOffset = builder.EndVector(len(adOffsets))
	}

	var algsOffset flatbuffers.UOffsetT
	if len(hs.Algorithms) > 0 {
		cs_body_handshakeStartAlgorithmsVector(builder, len(hs.Algorithms))
		for i := len(hs.Algorithms) - 1; i >= 0; i-- {
			builder.PrependByte(byte(hs.Algorithms[i]))
		}
		algsOffset = builder.EndVector(len(hs.Algorithms))
	}

	var kdfOffset flatbuffers.UOffsetT
	if len(hs.KdfTypes) > 0 {
		cs_body_handshakeStartKdfTypeVector(builder, len(hs.KdfTypes))
		for i := len(hs.KdfTypes) - 1; i >= 0; i-- {
			builder.PrependByte(byte(hs.KdfTypes[i]))
		}
		kdfOffset = builder.EndVector(len(hs.KdfTypes))
	}

	var compAlgOffset flatbuffers.UOffsetT
	if len(hs.CompressionAlgorithms) > 0 {
		cs_body_handshakeStartCompressionAlgorithmVector(builder, len(hs.CompressionAlgorithms))
		for i := len(hs.CompressionAlgorithms) - 1; i >= 0; i-- {
			builder.PrependByte(byte(hs.CompressionAlgorithms[i]))
		}
		compAlgOffset = builder.EndVector(len(hs.CompressionAlgorithms))
	}

	// Phase 4: Create initializeCrypto table.
	var initCryptoOffset flatbuffers.UOffsetT
	if hs.InitializeCrypto != nil {
		cs_body_post_cryptoStart(builder)
		cs_body_post_cryptoAddAlgorithm(builder, crypto_algorithm_t(hs.InitializeCrypto.Algorithm))
		if len(hs.InitializeCrypto.IV) > 0 {
			cs_body_post_cryptoAddIv(builder, initCryptoIVOffset)
		}
		if len(hs.InitializeCrypto.AAD) > 0 {
			cs_body_post_cryptoAddAad(builder, initCryptoAADOffset)
		}
		initCryptoOffset = cs_body_post_cryptoEnd(builder)
	}

	// Phase 5: Create handshake body table.
	cs_body_handshakeStart(builder)
	cs_body_handshakeAddSessionId(builder, hs.SessionID)
	cs_body_handshakeAddStep(builder, handshake_step_t(hs.Step))
	cs_body_handshakeAddKeyExchange(builder, key_exchange_t(hs.KeyExchange))
	if len(hs.KdfTypes) > 0 {
		cs_body_handshakeAddKdfType(builder, kdfOffset)
	}
	if len(hs.Algorithms) > 0 {
		cs_body_handshakeAddAlgorithms(builder, algsOffset)
	}
	if len(hs.AccessData) > 0 {
		cs_body_handshakeAddAccessData(builder, accessDataVecOffset)
	}
	if len(hs.PublicKey) > 0 {
		cs_body_handshakeAddPublicKey(builder, pubKeyOffset)
	}
	if hs.InitializeCrypto != nil {
		cs_body_handshakeAddInitializeCrypto(builder, initCryptoOffset)
	}
	if len(hs.CompressionAlgorithms) > 0 {
		cs_body_handshakeAddCompressionAlgorithm(builder, compAlgOffset)
	}
	cs_body_handshakeAddMaxPostMessageSize(builder, hs.MaxPostMessageSize)
	if len(hs.SessionToken) > 0 {
		cs_body_handshakeAddSessionToken(builder, sessionTokenOffset)
	}
	cs_body_handshakeAddHandshakeSequence(builder, hs.HandshakeSequence)
	handshakeOffset := cs_body_handshakeEnd(builder)

	// Phase 6: Create head table.
	client_message_headStart(builder)
	client_message_headAddType(builder, client_message_type_tkHandshake)
	client_message_headAddSequence(builder, seq)
	headOffset := client_message_headEnd(builder)

	// Phase 7: Create message table.
	client_messageStart(builder)
	client_messageAddHead(builder, headOffset)
	client_messageAddBodyType(builder, client_message_bodycs_body_handshake)
	client_messageAddBody(builder, handshakeOffset)
	msgOffset := client_messageEnd(builder)

	Finishclient_messageBuffer(builder, msgOffset)
	return builder.FinishedBytes()
}

// BuildPostMessage builds a complete flatbuffers client_message for a post.
func BuildPostMessage(seq uint64, post *PostInfo) []byte {
	builder := flatbuffers.NewBuilder(256 + len(post.Data))

	// Create data byte vector.
	var dataOffset flatbuffers.UOffsetT
	if len(post.Data) > 0 {
		dataOffset = builder.CreateByteVector(post.Data)
	}

	// Create crypto IV/AAD vectors.
	var cryptoIVOffset, cryptoAADOffset flatbuffers.UOffsetT
	if post.Crypto != nil {
		if len(post.Crypto.IV) > 0 {
			cryptoIVOffset = builder.CreateByteVector(post.Crypto.IV)
		}
		if len(post.Crypto.AAD) > 0 {
			cryptoAADOffset = builder.CreateByteVector(post.Crypto.AAD)
		}
	}

	// Create crypto table.
	var cryptoOffset flatbuffers.UOffsetT
	if post.Crypto != nil {
		cs_body_post_cryptoStart(builder)
		cs_body_post_cryptoAddAlgorithm(builder, crypto_algorithm_t(post.Crypto.Algorithm))
		if len(post.Crypto.IV) > 0 {
			cs_body_post_cryptoAddIv(builder, cryptoIVOffset)
		}
		if len(post.Crypto.AAD) > 0 {
			cs_body_post_cryptoAddAad(builder, cryptoAADOffset)
		}
		cryptoOffset = cs_body_post_cryptoEnd(builder)
	}

	// Create compression table.
	var compressionOffset flatbuffers.UOffsetT
	if post.Compression != nil {
		cs_body_post_compressionStart(builder)
		cs_body_post_compressionAddType(builder, compression_algorithm_t(post.Compression.Type))
		cs_body_post_compressionAddOriginalSize(builder, post.Compression.OriginalSize)
		compressionOffset = cs_body_post_compressionEnd(builder)
	}

	// Create post body.
	cs_body_postStart(builder)
	if post.Crypto != nil {
		cs_body_postAddCrypto(builder, cryptoOffset)
	}
	if post.Compression != nil {
		cs_body_postAddCompression(builder, compressionOffset)
	}
	cs_body_postAddLength(builder, post.Length)
	if len(post.Data) > 0 {
		cs_body_postAddData(builder, dataOffset)
	}
	postOffset := cs_body_postEnd(builder)

	// Create head.
	client_message_headStart(builder)
	client_message_headAddType(builder, client_message_type_tkPost)
	client_message_headAddSequence(builder, seq)
	headOffset := client_message_headEnd(builder)

	// Create message.
	client_messageStart(builder)
	client_messageAddHead(builder, headOffset)
	client_messageAddBodyType(builder, client_message_bodycs_body_post)
	client_messageAddBody(builder, postOffset)
	msgOffset := client_messageEnd(builder)

	Finishclient_messageBuffer(builder, msgOffset)
	return builder.FinishedBytes()
}

// BuildPingMessage builds a complete flatbuffers client_message for a ping.
func BuildPingMessage(seq uint64, timepoint int64) []byte {
	builder := flatbuffers.NewBuilder(64)

	cs_body_pingStart(builder)
	cs_body_pingAddTimepoint(builder, timepoint)
	pingOffset := cs_body_pingEnd(builder)

	client_message_headStart(builder)
	client_message_headAddType(builder, client_message_type_tkPing)
	client_message_headAddSequence(builder, seq)
	headOffset := client_message_headEnd(builder)

	client_messageStart(builder)
	client_messageAddHead(builder, headOffset)
	client_messageAddBodyType(builder, client_message_bodycs_body_ping)
	client_messageAddBody(builder, pingOffset)
	msgOffset := client_messageEnd(builder)

	Finishclient_messageBuffer(builder, msgOffset)
	return builder.FinishedBytes()
}

// BuildPongMessage builds a complete flatbuffers client_message for a pong.
func BuildPongMessage(seq uint64, timepoint int64) []byte {
	builder := flatbuffers.NewBuilder(64)

	cs_body_pingStart(builder)
	cs_body_pingAddTimepoint(builder, timepoint)
	pingOffset := cs_body_pingEnd(builder)

	client_message_headStart(builder)
	client_message_headAddType(builder, client_message_type_tkPong)
	client_message_headAddSequence(builder, seq)
	headOffset := client_message_headEnd(builder)

	// Pong uses the same body type (cs_body_ping) as Ping.
	client_messageStart(builder)
	client_messageAddHead(builder, headOffset)
	client_messageAddBodyType(builder, client_message_bodycs_body_ping)
	client_messageAddBody(builder, pingOffset)
	msgOffset := client_messageEnd(builder)

	Finishclient_messageBuffer(builder, msgOffset)
	return builder.FinishedBytes()
}

// BuildConfirmMessage builds a complete flatbuffers client_message for a confirm.
func BuildConfirmMessage(seq uint64, sessionID, handshakeSequence uint64) []byte {
	builder := flatbuffers.NewBuilder(64)

	cs_body_confirmStart(builder)
	cs_body_confirmAddSessionId(builder, sessionID)
	cs_body_confirmAddHandshakeSequence(builder, handshakeSequence)
	confirmOffset := cs_body_confirmEnd(builder)

	client_message_headStart(builder)
	client_message_headAddType(builder, client_message_type_tkConfirm)
	client_message_headAddSequence(builder, seq)
	headOffset := client_message_headEnd(builder)

	client_messageStart(builder)
	client_messageAddHead(builder, headOffset)
	client_messageAddBodyType(builder, client_message_bodycs_body_confirm)
	client_messageAddBody(builder, confirmOffset)
	msgOffset := client_messageEnd(builder)

	Finishclient_messageBuffer(builder, msgOffset)
	return builder.FinishedBytes()
}

// BuildKickoffMessage builds a complete flatbuffers client_message for a kickoff.
func BuildKickoffMessage(seq uint64, reason, subReason int32, message string) []byte {
	builder := flatbuffers.NewBuilder(128 + len(message))

	var msgStrOffset flatbuffers.UOffsetT
	if message != "" {
		msgStrOffset = builder.CreateString(message)
	}

	cs_body_kickoffStart(builder)
	cs_body_kickoffAddReason(builder, reason)
	cs_body_kickoffAddSubReason(builder, subReason)
	if message != "" {
		cs_body_kickoffAddMessage(builder, msgStrOffset)
	}
	kickoffOffset := cs_body_kickoffEnd(builder)

	client_message_headStart(builder)
	client_message_headAddType(builder, client_message_type_tkKickoff)
	client_message_headAddSequence(builder, seq)
	headOffset := client_message_headEnd(builder)

	client_messageStart(builder)
	client_messageAddHead(builder, headOffset)
	client_messageAddBodyType(builder, client_message_bodycs_body_kickoff)
	client_messageAddBody(builder, kickoffOffset)
	msgOffset := client_messageEnd(builder)

	Finishclient_messageBuffer(builder, msgOffset)
	return builder.FinishedBytes()
}
