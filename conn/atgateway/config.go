package conn

import (
	"flag"
	"strings"

	v2 "github.com/atframework/robot-go/conn/atgateway/atframework/gateway/v2"
)

func ParseGatewayConfig(flagSet *flag.FlagSet) *GatewaySessionConfig {
	accessToken := readFlagValue(flagSet, "access-token")
	ke := parseKeyExchange(readFlagValue(flagSet, "key-exchange"))
	algos := parseCryptoAlgorithms(readFlagValue(flagSet, "crypto"))
	compressions := parseCompressionAlgorithms(readFlagValue(flagSet, "compression"))

	return &GatewaySessionConfig{
		AccessTokens:         [][]byte{[]byte(accessToken)},
		KeyExchange:          ke,
		SupportedAlgorithms:  algos,
		SupportedCompression: compressions,
		CompressionThreshold: 1024,
	}
}

func readFlagValue(flagSet *flag.FlagSet, name string) string {
	if flagSet == nil {
		return ""
	}

	flagValue := flagSet.Lookup(name)
	if flagValue == nil || flagValue.Value == nil {
		return ""
	}

	return flagValue.Value.String()
}

func parseKeyExchange(value string) v2.KeyExchangeT {
	for _, rawItem := range splitConfigItems(value) {
		switch rawItem {
		case "x25519":
			return v2.KeyExchangeX25519
		case "p256", "p-256", "secp256r1":
			return v2.KeyExchangeSecp256r1
		case "p384", "p-384", "secp384r1":
			return v2.KeyExchangeSecp384r1
		case "p521", "p-521", "secp521r1":
			return v2.KeyExchangeSecp521r1
		}
	}

	return v2.KeyExchangeNone
}

func parseCryptoAlgorithms(value string) []v2.CryptoAlgorithmT {
	mapping := map[string]v2.CryptoAlgorithmT{
		"none":                    v2.CryptoNone,
		"xxtea":                   v2.CryptoXxtea,
		"aes-128-cbc":             v2.CryptoAes128Cbc,
		"aes-192-cbc":             v2.CryptoAes192Cbc,
		"aes-256-cbc":             v2.CryptoAes256Cbc,
		"aes-128-gcm":             v2.CryptoAes128Gcm,
		"aes-192-gcm":             v2.CryptoAes192Gcm,
		"aes-256-gcm":             v2.CryptoAes256Gcm,
		"chacha20":                v2.CryptoChacha20,
		"chacha20-poly1305":       v2.CryptoChacha20Poly1305Ietf,
		"xchacha20-poly1305":      v2.CryptoXchacha20Poly1305Ietf,
		"chacha20-poly1305-ietf":  v2.CryptoChacha20Poly1305Ietf,
		"xchacha20-poly1305-ietf": v2.CryptoXchacha20Poly1305Ietf,
	}

	return parseList(value, mapping, []v2.CryptoAlgorithmT{v2.CryptoNone})
}

func parseCompressionAlgorithms(value string) []v2.CompressionAlgorithmT {
	mapping := map[string]v2.CompressionAlgorithmT{
		"none":   v2.CompressionNone,
		"zstd":   v2.CompressionZstd,
		"lz4":    v2.CompressionLz4,
		"snappy": v2.CompressionSnappy,
		"zlib":   v2.CompressionZlib,
	}

	return parseList(value, mapping, []v2.CompressionAlgorithmT{v2.CompressionNone})
}

func parseList[T comparable](value string, mapping map[string]T, fallback []T) []T {
	if len(splitConfigItems(value)) == 0 {
		result := make([]T, len(fallback))
		copy(result, fallback)
		return result
	}

	seen := make(map[T]struct{}, len(mapping))
	result := make([]T, 0, len(mapping))
	for _, item := range splitConfigItems(value) {
		mapped, ok := mapping[item]
		if !ok {
			continue
		}

		if _, exists := seen[mapped]; exists {
			continue
		}

		seen[mapped] = struct{}{}
		result = append(result, mapped)
	}

	if len(result) == 0 {
		result = make([]T, len(fallback))
		copy(result, fallback)
	}

	return result
}

func splitConfigItems(value string) []string {
	return strings.FieldsFunc(strings.ToLower(strings.TrimSpace(value)), func(r rune) bool {
		switch r {
		case ',', '|', ';', '[', ']', '(', ')', '{', '}':
			return true
		default:
			return r <= ' '
		}
	})
}
