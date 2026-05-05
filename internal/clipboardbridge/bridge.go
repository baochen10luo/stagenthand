package clipboardbridge

import (
	"encoding/base64"
	"errors"
	"fmt"
	"os"
	"strings"
)

var pngHeader = []byte{0x89, 'P', 'N', 'G', '\r', '\n', 0x1a, '\n'}

type Response struct {
	OK    bool   `json:"ok"`
	Image string `json:"image,omitempty"`
	Error string `json:"error,omitempty"`
}

func DecodeImageResponse(response Response) ([]byte, error) {
	if !response.OK {
		if response.Error == "" {
			return nil, errors.New("clipboard bridge request failed")
		}

		return nil, errors.New(response.Error)
	}

	if response.Image == "" {
		return nil, errors.New("clipboard bridge response did not include image data")
	}

	data, err := base64.StdEncoding.DecodeString(response.Image)
	if err != nil {
		return nil, fmt.Errorf("decode clipboard image: %w", err)
	}

	if len(data) < len(pngHeader) || string(data[:len(pngHeader)]) != string(pngHeader) {
		return nil, errors.New("clipboard bridge payload is not a PNG image")
	}

	return data, nil
}

func TempOutputPath(prefix string) (string, error) {
	prefix = strings.TrimSpace(prefix)
	if prefix == "" {
		prefix = "ccimg"
	}

	file, err := os.CreateTemp("", prefix+"-*.png")
	if err != nil {
		return "", fmt.Errorf("create temp png: %w", err)
	}

	path := file.Name()
	if err := file.Close(); err != nil {
		return "", fmt.Errorf("close temp png: %w", err)
	}

	return path, nil
}
