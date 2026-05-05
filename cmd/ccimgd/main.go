package main

import (
	"encoding/base64"
	"encoding/json"
	"errors"
	"flag"
	"fmt"
	"net"
	"os"
	"os/exec"
	"runtime"
	"time"

	"github.com/baochen10luo/stagenthand/internal/clipboardbridge"
)

func main() {
	addr := flag.String("addr", "127.0.0.1:9998", "listen address")
	timeout := flag.Duration("clipboard-timeout", 5*time.Second, "clipboard command timeout")
	flag.Parse()

	if err := serve(*addr, *timeout); err != nil {
		fmt.Fprintln(os.Stderr, err)
		os.Exit(1)
	}
}

func serve(addr string, timeout time.Duration) error {
	listener, err := net.Listen("tcp", addr)
	if err != nil {
		return fmt.Errorf("listen on %s: %w", addr, err)
	}
	defer listener.Close()

	fmt.Fprintf(os.Stderr, "ccimgd listening on %s\n", addr)

	for {
		conn, err := listener.Accept()
		if err != nil {
			return fmt.Errorf("accept clipboard bridge connection: %w", err)
		}

		go handleConn(conn, timeout)
	}
}

func handleConn(conn net.Conn, timeout time.Duration) {
	defer conn.Close()

	data, err := readClipboardPNG(timeout)
	response := clipboardbridge.Response{OK: err == nil}
	if err != nil {
		response.Error = err.Error()
	} else {
		response.Image = base64.StdEncoding.EncodeToString(data)
	}

	_ = json.NewEncoder(conn).Encode(response)
}

func readClipboardPNG(timeout time.Duration) ([]byte, error) {
	name, args, err := clipboardCommand()
	if err != nil {
		return nil, err
	}

	cmd := exec.Command(name, args...)
	timer := time.AfterFunc(timeout, func() {
		_ = cmd.Process.Kill()
	})
	defer timer.Stop()

	output, err := cmd.Output()
	if err != nil {
		return nil, fmt.Errorf("read clipboard PNG with %s: %w", name, err)
	}

	if len(output) == 0 {
		return nil, errors.New("clipboard does not contain a PNG image")
	}

	return output, nil
}

func clipboardCommand() (string, []string, error) {
	switch runtime.GOOS {
	case "darwin":
		return "pngpaste", []string{"-"}, nil
	case "linux":
		if os.Getenv("WAYLAND_DISPLAY") != "" {
			return "wl-paste", []string{"--no-newline", "--type", "image/png"}, nil
		}

		if os.Getenv("DISPLAY") != "" {
			return "xclip", []string{"-selection", "clipboard", "-target", "image/png", "-out"}, nil
		}
	}

	return "", nil, fmt.Errorf("unsupported clipboard environment on %s", runtime.GOOS)
}
