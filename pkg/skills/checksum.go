package skills

import (
	"crypto/sha256"
	"encoding/hex"
	"fmt"
	"io"
	"os"
)

// SHA256File computes the SHA256 hash of a file using streaming reads.
func SHA256File(path string) (string, error) {
	f, err := os.Open(path)
	if err != nil {
		return "", fmt.Errorf("open file for checksum: %w", err)
	}
	defer f.Close()

	h := sha256.New()
	if _, err := io.Copy(h, f); err != nil {
		return "", fmt.Errorf("compute checksum: %w", err)
	}
	return hex.EncodeToString(h.Sum(nil)), nil
}

// VerifyChecksum computes the SHA256 of filePath and compares it to expectedHex.
// Returns nil if they match, or an error describing the mismatch.
func VerifyChecksum(filePath, expectedHex string) error {
	actual, err := SHA256File(filePath)
	if err != nil {
		return err
	}
	if actual != expectedHex {
		return fmt.Errorf("checksum mismatch for %s: expected %s, got %s", filePath, expectedHex, actual)
	}
	return nil
}
