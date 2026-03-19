package conn

import (
	"bytes"
	"compress/zlib"
	"fmt"
	"io"

	"github.com/golang/snappy"
	"github.com/klauspost/compress/zstd"
	"github.com/pierrec/lz4/v4"

	v2 "github.com/atframework/robot-go/conn/atgateway/atframework/gateway/v2"
)

// Compressor provides compress/decompress operations for a specific algorithm.
type Compressor interface {
	Compress(data []byte) ([]byte, error)
	Decompress(data []byte, originalSize int) ([]byte, error)
}

// NewCompressor creates a Compressor for the given compression algorithm.
func NewCompressor(algo v2.CompressionAlgorithmT) (Compressor, error) {
	switch algo {
	case v2.CompressionZstd:
		return newZstdCompressor()
	case v2.CompressionLz4:
		return &lz4Compressor{}, nil
	case v2.CompressionSnappy:
		return &snappyCompressor{}, nil
	case v2.CompressionZlib:
		return &zlibCompressor{}, nil
	default:
		return nil, fmt.Errorf("unsupported compression algorithm: %d", algo)
	}
}

// ========================= Zstd =========================

type zstdCompressor struct {
	encoder *zstd.Encoder
	decoder *zstd.Decoder
}

func newZstdCompressor() (*zstdCompressor, error) {
	enc, err := zstd.NewWriter(nil, zstd.WithEncoderLevel(zstd.SpeedDefault))
	if err != nil {
		return nil, fmt.Errorf("create zstd encoder: %w", err)
	}
	dec, err := zstd.NewReader(nil)
	if err != nil {
		enc.Close()
		return nil, fmt.Errorf("create zstd decoder: %w", err)
	}
	return &zstdCompressor{encoder: enc, decoder: dec}, nil
}

func (c *zstdCompressor) Compress(data []byte) ([]byte, error) {
	return c.encoder.EncodeAll(data, nil), nil
}

func (c *zstdCompressor) Decompress(data []byte, originalSize int) ([]byte, error) {
	dst := make([]byte, 0, originalSize)
	return c.decoder.DecodeAll(data, dst)
}

// ========================= LZ4 =========================

type lz4Compressor struct{}

func (c *lz4Compressor) Compress(data []byte) ([]byte, error) {
	dst := make([]byte, lz4.CompressBlockBound(len(data)))
	var compressor lz4.Compressor
	n, err := compressor.CompressBlock(data, dst)
	if err != nil {
		return nil, fmt.Errorf("lz4 compress: %w", err)
	}
	if n == 0 {
		// Data was not compressible by LZ4; should not happen in practice
		// since protocol layer checks compression threshold.
		return nil, fmt.Errorf("lz4: data not compressible")
	}
	return dst[:n], nil
}

func (c *lz4Compressor) Decompress(data []byte, originalSize int) ([]byte, error) {
	dst := make([]byte, originalSize)
	n, err := lz4.UncompressBlock(data, dst)
	if err != nil {
		return nil, fmt.Errorf("lz4 decompress: %w", err)
	}
	return dst[:n], nil
}

// ========================= Snappy =========================

type snappyCompressor struct{}

func (c *snappyCompressor) Compress(data []byte) ([]byte, error) {
	return snappy.Encode(nil, data), nil
}

func (c *snappyCompressor) Decompress(data []byte, _ int) ([]byte, error) {
	return snappy.Decode(nil, data)
}

// ========================= Zlib =========================

type zlibCompressor struct{}

func (c *zlibCompressor) Compress(data []byte) ([]byte, error) {
	var buf bytes.Buffer
	w := zlib.NewWriter(&buf)
	if _, err := w.Write(data); err != nil {
		return nil, fmt.Errorf("zlib compress write: %w", err)
	}
	if err := w.Close(); err != nil {
		return nil, fmt.Errorf("zlib compress close: %w", err)
	}
	return buf.Bytes(), nil
}

func (c *zlibCompressor) Decompress(data []byte, originalSize int) ([]byte, error) {
	r, err := zlib.NewReader(bytes.NewReader(data))
	if err != nil {
		return nil, fmt.Errorf("zlib decompress init: %w", err)
	}
	defer r.Close()
	dst := make([]byte, 0, originalSize)
	dst, err = io.ReadAll(io.LimitReader(r, int64(originalSize)+1))
	if err != nil {
		return nil, fmt.Errorf("zlib decompress read: %w", err)
	}
	return dst, nil
}
