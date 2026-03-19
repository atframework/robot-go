package conn

import (
	"encoding/binary"
	"fmt"
)

// FrameHeaderSize is the size of the wire format frame header (hash + length).
const FrameHeaderSize = 8 // 4 bytes murmur3 hash + 4 bytes message length

// EncodeFrame wraps flatbuffers-serialized data into a wire format frame:
//
//	[murmur3_hash (4B LE)] [msg_len (4B LE)] [flatbuffers_data (msg_len bytes)]
func EncodeFrame(fbData []byte) []byte {
	hash := MurmurHash3X86_32(fbData, 0)
	frame := make([]byte, FrameHeaderSize+len(fbData))
	binary.LittleEndian.PutUint32(frame[0:4], hash)
	binary.LittleEndian.PutUint32(frame[4:8], uint32(len(fbData)))
	copy(frame[FrameHeaderSize:], fbData)
	return frame
}

// DecodeFrames extracts complete protocol frames from a byte buffer.
// Returns the decoded frame payloads, any remaining incomplete data, and an error
// if a hash mismatch is detected.
func DecodeFrames(data []byte) (frames [][]byte, remaining []byte, err error) {
	for len(data) >= FrameHeaderSize {
		expectedHash := binary.LittleEndian.Uint32(data[0:4])
		msgLen := binary.LittleEndian.Uint32(data[4:8])

		totalLen := FrameHeaderSize + int(msgLen)
		if len(data) < totalLen {
			break // incomplete frame, wait for more data
		}

		payload := data[FrameHeaderSize:totalLen]
		actualHash := MurmurHash3X86_32(payload, 0)
		if actualHash != expectedHash {
			return nil, nil, fmt.Errorf("frame hash mismatch: expected 0x%08x, got 0x%08x", expectedHash, actualHash)
		}

		// Copy payload to avoid holding reference to the original buffer.
		frame := make([]byte, len(payload))
		copy(frame, payload)
		frames = append(frames, frame)

		data = data[totalLen:]
	}

	if len(data) > 0 {
		remaining = data
	}
	return frames, remaining, nil
}
