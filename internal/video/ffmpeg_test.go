package video

import (
	"context"
	"os"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestConcatenateShots_EmptyPaths(t *testing.T) {
	err := ConcatenateShots(context.Background(), nil, "/tmp/out.mp4")
	require.Error(t, err)
	assert.Contains(t, err.Error(), "no shot paths")
}

func TestConcatenateShots_EmptySlice(t *testing.T) {
	err := ConcatenateShots(context.Background(), []string{}, "/tmp/out.mp4")
	require.Error(t, err)
	assert.Contains(t, err.Error(), "no shot paths")
}

func TestConcatenateShots_FfmpegNotFound(t *testing.T) {
	// Override PATH to empty so ffmpeg cannot be found
	origPath := os.Getenv("PATH")
	t.Setenv("PATH", "")
	defer os.Setenv("PATH", origPath)

	err := ConcatenateShots(context.Background(), []string{"/tmp/a.mp4", "/tmp/b.mp4"}, "/tmp/out.mp4")
	require.Error(t, err)
	assert.Contains(t, err.Error(), "ffmpeg")
}
