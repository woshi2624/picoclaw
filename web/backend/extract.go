package main

import (
	"crypto/sha256"
	"fmt"
	"os"
	"path/filepath"
	"runtime"
)

func ensurePicoclawBinary() (string, error) {
	if len(embeddedPicoclawBin) == 0 {
		return "", nil
	}
	home, err := os.UserHomeDir()
	if err != nil {
		return "", fmt.Errorf("ensurePicoclawBinary: cannot determine home dir: %w", err)
	}
	binName := "picoclaw"
	if runtime.GOOS == "windows" {
		binName = "picoclaw.exe"
	}
	binDir := filepath.Join(home, ".picoclaw", "bin")
	if err := os.MkdirAll(binDir, 0o755); err != nil {
		return "", fmt.Errorf("ensurePicoclawBinary: mkdir: %w", err)
	}
	destPath := filepath.Join(binDir, binName)
	// Skip extraction if existing file matches embedded bytes
	if existing, err := os.ReadFile(destPath); err == nil {
		if sha256.Sum256(existing) == sha256.Sum256(embeddedPicoclawBin) {
			return destPath, nil
		}
	}
	// Atomic write: write temp then rename
	tmpPath := destPath + ".tmp"
	if err := os.WriteFile(tmpPath, embeddedPicoclawBin, 0o755); err != nil {
		return "", fmt.Errorf("ensurePicoclawBinary: write: %w", err)
	}
	if err := os.Rename(tmpPath, destPath); err != nil {
		_ = os.Remove(tmpPath)
		return "", fmt.Errorf("ensurePicoclawBinary: rename: %w", err)
	}
	return destPath, nil
}
