package xaipipeline

import (
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"fmt"
)

func manifestIdentityHash(manifest Manifest) (string, error) {
	data, err := json.Marshal(manifest)
	if err != nil {
		return "", fmt.Errorf("hash xai manifest identity: %w", err)
	}
	sum := sha256.Sum256(data)
	return hex.EncodeToString(sum[:]), nil
}
