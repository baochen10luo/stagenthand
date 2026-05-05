package clipboardbridge

import (
	"bytes"
	"encoding/base64"
	"path/filepath"
	"testing"
)

func TestDecodeImageResponse(t *testing.T) {
	t.Parallel()

	png := []byte{0x89, 'P', 'N', 'G', '\r', '\n', 0x1a, '\n', 0x01}
	response := Response{
		OK:    true,
		Image: base64.StdEncoding.EncodeToString(png),
	}

	got, err := DecodeImageResponse(response)
	if err != nil {
		t.Fatalf("DecodeImageResponse() error = %v", err)
	}

	if !bytes.Equal(got, png) {
		t.Fatalf("DecodeImageResponse() = %v, want %v", got, png)
	}
}

func TestDecodeImageResponseRejectsInvalidPayload(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name     string
		response Response
	}{
		{
			name: "server error",
			response: Response{
				OK:    false,
				Error: "clipboard empty",
			},
		},
		{
			name: "invalid base64",
			response: Response{
				OK:    true,
				Image: "%%%invalid%%%",
			},
		},
		{
			name: "not png",
			response: Response{
				OK:    true,
				Image: base64.StdEncoding.EncodeToString([]byte("hello")),
			},
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()

			if _, err := DecodeImageResponse(tt.response); err == nil {
				t.Fatalf("DecodeImageResponse(%+v) error = nil, want non-nil", tt.response)
			}
		})
	}
}

func TestTempOutputPathUsesPrefixAndExtension(t *testing.T) {
	t.Parallel()

	path, err := TempOutputPath("ccimg")
	if err != nil {
		t.Fatalf("TempOutputPath() error = %v", err)
	}

	if ext := filepath.Ext(path); ext != ".png" {
		t.Fatalf("TempOutputPath() ext = %q, want .png", ext)
	}

	if base := filepath.Base(path); len(base) < len("ccimg-") || base[:len("ccimg-")] != "ccimg-" {
		t.Fatalf("TempOutputPath() base = %q, want prefix ccimg-", base)
	}
}
