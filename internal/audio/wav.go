package audio

import (
	"encoding/binary"
	"fmt"
	"io"
)

// WAVDuration reads a WAV file and returns its duration in seconds.
func WAVDuration(r io.ReadSeeker) (float64, error) {
	var header [12]byte
	if _, err := io.ReadFull(r, header[:]); err != nil {
		return 0, fmt.Errorf("read RIFF header: %w", err)
	}
	if string(header[0:4]) != "RIFF" || string(header[8:12]) != "WAVE" {
		return 0, fmt.Errorf("not a WAV file")
	}

	var (
		sampleRate    uint32
		numChannels   uint16
		bitsPerSample uint16
		dataSize      uint32
		foundFmt      bool
	)

	for {
		var chunkID [4]byte
		var chunkSize uint32
		if _, err := io.ReadFull(r, chunkID[:]); err != nil {
			if err == io.EOF {
				break
			}
			return 0, fmt.Errorf("read chunk ID: %w", err)
		}
		if err := binary.Read(r, binary.LittleEndian, &chunkSize); err != nil {
			return 0, fmt.Errorf("read chunk size: %w", err)
		}

		switch string(chunkID[:]) {
		case "fmt ":
			if chunkSize < 16 {
				return 0, fmt.Errorf("fmt chunk too small: %d", chunkSize)
			}
			var audioFormat uint16
			if err := binary.Read(r, binary.LittleEndian, &audioFormat); err != nil {
				return 0, fmt.Errorf("read audio format: %w", err)
			}
			if audioFormat != 1 { // PCM
				// Still try to parse, some WAVs use other formats
			}
			if err := binary.Read(r, binary.LittleEndian, &numChannels); err != nil {
				return 0, fmt.Errorf("read channels: %w", err)
			}
			if err := binary.Read(r, binary.LittleEndian, &sampleRate); err != nil {
				return 0, fmt.Errorf("read sample rate: %w", err)
			}
			// Skip byte rate (4 bytes) and block align (2 bytes)
			if _, err := io.ReadFull(r, make([]byte, 6)); err != nil {
				return 0, fmt.Errorf("skip byte rate/block align: %w", err)
			}
			if err := binary.Read(r, binary.LittleEndian, &bitsPerSample); err != nil {
				return 0, fmt.Errorf("read bits per sample: %w", err)
			}
			foundFmt = true
			// Skip any extra fmt data
			remaining := chunkSize - 16
			if remaining > 0 {
				if _, err := io.ReadFull(r, make([]byte, remaining)); err != nil {
					return 0, fmt.Errorf("skip extra fmt: %w", err)
				}
			}
		case "data":
			dataSize = chunkSize
			// Don't read data, just record the size
			if _, err := r.Seek(int64(chunkSize), io.SeekCurrent); err != nil {
				return 0, fmt.Errorf("skip data: %w", err)
			}
		default:
			// Skip other chunks
			if _, err := r.Seek(int64(chunkSize), io.SeekCurrent); err != nil {
				return 0, fmt.Errorf("skip chunk %s: %w", string(chunkID[:]), err)
			}
		}
	}

	if !foundFmt {
		return 0, fmt.Errorf("no fmt chunk found")
	}
	if sampleRate == 0 || numChannels == 0 || bitsPerSample == 0 {
		return 0, fmt.Errorf("incomplete WAV header: rate=%d ch=%d bits=%d", sampleRate, numChannels, bitsPerSample)
	}

	bytesPerSec := uint64(sampleRate) * uint64(numChannels) * uint64(bitsPerSample) / 8
	if bytesPerSec == 0 {
		return 0, fmt.Errorf("bytes per second is 0")
	}
	duration := float64(dataSize) / float64(bytesPerSec)
	return duration, nil
}
