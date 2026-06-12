package xaipipeline

import (
	"context"
	"errors"
	"os"
	"path/filepath"
	"testing"
)

func TestMediaToolCommandPrefersSystemBinary(t *testing.T) {
	binDir := t.TempDir()
	writeExecutable(t, filepath.Join(binDir, "ffmpeg"))
	writeExecutable(t, filepath.Join(binDir, "bunx"))
	t.Setenv("PATH", binDir)

	cmd, err := mediaToolCommand(context.Background(), "ffmpeg", "-version")
	if err != nil {
		t.Fatalf("mediaToolCommand: %v", err)
	}

	if cmd.Path != filepath.Join(binDir, "ffmpeg") {
		t.Fatalf("Path = %q, want system ffmpeg", cmd.Path)
	}
	wantArgs := []string{filepath.Join(binDir, "ffmpeg"), "-version"}
	if !equalStringSlices(cmd.Args, wantArgs) {
		t.Fatalf("Args = %#v, want %#v", cmd.Args, wantArgs)
	}
}

func TestMediaToolCommandNilContextUsesBackground(t *testing.T) {
	binDir := t.TempDir()
	writeExecutable(t, filepath.Join(binDir, "ffmpeg"))
	t.Setenv("PATH", binDir)

	cmd, err := mediaToolCommand(nil, "ffmpeg", "-version")
	if err != nil {
		t.Fatalf("mediaToolCommand: %v", err)
	}
	if cmd.Path != filepath.Join(binDir, "ffmpeg") {
		t.Fatalf("Path = %q, want system ffmpeg", cmd.Path)
	}
}

func TestMediaToolCommandFallsBackToRemotionBunx(t *testing.T) {
	binDir := t.TempDir()
	writeExecutable(t, filepath.Join(binDir, "bunx"))
	t.Setenv("PATH", binDir)

	cmd, err := mediaToolCommand(context.Background(), "ffprobe", "input.mp4")
	if err != nil {
		t.Fatalf("mediaToolCommand: %v", err)
	}

	if cmd.Path != filepath.Join(binDir, "bunx") {
		t.Fatalf("Path = %q, want bunx fallback", cmd.Path)
	}
	wantArgs := []string{filepath.Join(binDir, "bunx"), "remotion", "ffprobe", "input.mp4"}
	if !equalStringSlices(cmd.Args, wantArgs) {
		t.Fatalf("Args = %#v, want %#v", cmd.Args, wantArgs)
	}
}

func TestMediaToolCommandRejectsCanceledContextBeforeLookup(t *testing.T) {
	t.Setenv("PATH", t.TempDir())
	ctx, cancel := context.WithCancel(context.Background())
	cancel()

	_, err := mediaToolCommand(ctx, "ffmpeg", "-version")
	if !errors.Is(err, context.Canceled) {
		t.Fatalf("mediaToolCommand error = %v, want context.Canceled", err)
	}
}

func TestMediaToolCommandErrorsWhenNoSystemOrBunxTool(t *testing.T) {
	t.Setenv("PATH", t.TempDir())

	_, err := mediaToolCommand(context.Background(), "ffmpeg", "-version")
	if err == nil {
		t.Fatal("mediaToolCommand error = nil, want missing tool error")
	}
}

func writeExecutable(t *testing.T, path string) {
	t.Helper()
	if err := os.WriteFile(path, []byte("#!/bin/sh\nexit 0\n"), 0755); err != nil {
		t.Fatalf("write executable %s: %v", path, err)
	}
}

func equalStringSlices(a, b []string) bool {
	if len(a) != len(b) {
		return false
	}
	for i := range a {
		if a[i] != b[i] {
			return false
		}
	}
	return true
}
