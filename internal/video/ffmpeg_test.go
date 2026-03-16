package video

import (
	"context"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
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

func TestConcatenateShots_HappyPath(t *testing.T) {
	if _, err := exec.LookPath("ffmpeg"); err != nil {
		t.Skip("ffmpeg not available")
	}

	dir := t.TempDir()

	// Create two minimal valid mp4 files using ffmpeg
	for i, name := range []string{"a.mp4", "b.mp4"} {
		path := filepath.Join(dir, name)
		// Generate a 0.1s silent video with solid color
		color := "red"
		if i == 1 {
			color = "blue"
		}
		cmd := exec.Command("ffmpeg",
			"-f", "lavfi", "-i", fmt.Sprintf("color=c=%s:size=64x64:duration=0.1:rate=10", color),
			"-f", "lavfi", "-i", "anullsrc=r=44100:cl=mono",
			"-t", "0.1",
			"-c:v", "libx264", "-pix_fmt", "yuv420p",
			"-c:a", "aac",
			"-y", path,
		)
		out, err := cmd.CombinedOutput()
		require.NoError(t, err, "ffmpeg create %s: %s", name, string(out))
	}

	outputPath := filepath.Join(dir, "out.mp4")
	err := ConcatenateShots(context.Background(),
		[]string{filepath.Join(dir, "a.mp4"), filepath.Join(dir, "b.mp4")},
		outputPath,
	)
	require.NoError(t, err)

	// Verify the output file exists and has content
	info, err := os.Stat(outputPath)
	require.NoError(t, err)
	assert.Greater(t, info.Size(), int64(0))
}

func TestConcatenateShots_OutputDirNotExist(t *testing.T) {
	if _, err := exec.LookPath("ffmpeg"); err != nil {
		t.Skip("ffmpeg not available")
	}

	dir := t.TempDir()
	inputFile := filepath.Join(dir, "a.mp4")
	// Create a minimal mp4
	cmd := exec.Command("ffmpeg",
		"-f", "lavfi", "-i", "color=c=red:size=64x64:duration=0.1:rate=10",
		"-c:v", "libx264", "-pix_fmt", "yuv420p",
		"-y", inputFile,
	)
	out, err := cmd.CombinedOutput()
	require.NoError(t, err, "ffmpeg create: %s", string(out))

	// Use an output path whose directory does not exist
	badOutputPath := filepath.Join(dir, "nonexistent_subdir", "out.mp4")
	err = ConcatenateShots(context.Background(), []string{inputFile}, badOutputPath)
	require.Error(t, err)
	assert.Contains(t, err.Error(), "ffmpeg concat failed")
}
