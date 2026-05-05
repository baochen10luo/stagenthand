package main

import (
	"bufio"
	"encoding/json"
	"flag"
	"fmt"
	"net"
	"os"
	"time"

	"github.com/baochen10luo/stagenthand/internal/clipboardbridge"
)

func main() {
	addr := flag.String("addr", "127.0.0.1:9998", "clipboard bridge address")
	out := flag.String("out", "", "output PNG path (defaults to a temp file)")
	timeout := flag.Duration("timeout", 5*time.Second, "network timeout")
	flag.Parse()

	path, err := fetchImage(*addr, *out, *timeout)
	if err != nil {
		fmt.Fprintln(os.Stderr, err)
		os.Exit(1)
	}

	fmt.Println(path)
}

func fetchImage(addr string, out string, timeout time.Duration) (string, error) {
	conn, err := net.DialTimeout("tcp", addr, timeout)
	if err != nil {
		return "", fmt.Errorf("connect to clipboard bridge: %w", err)
	}
	defer conn.Close()

	if err := conn.SetDeadline(time.Now().Add(timeout)); err != nil {
		return "", fmt.Errorf("set connection deadline: %w", err)
	}

	if _, err := conn.Write([]byte("{}\n")); err != nil {
		return "", fmt.Errorf("request clipboard image: %w", err)
	}

	var response clipboardbridge.Response
	if err := json.NewDecoder(bufio.NewReader(conn)).Decode(&response); err != nil {
		return "", fmt.Errorf("decode clipboard bridge response: %w", err)
	}

	data, err := clipboardbridge.DecodeImageResponse(response)
	if err != nil {
		return "", err
	}

	path := out
	if path == "" {
		path, err = clipboardbridge.TempOutputPath("ccimg")
		if err != nil {
			return "", err
		}
	}

	if err := os.WriteFile(path, data, 0o600); err != nil {
		return "", fmt.Errorf("write png file: %w", err)
	}

	return path, nil
}
