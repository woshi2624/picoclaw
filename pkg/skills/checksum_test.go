package skills

import (
	"crypto/sha256"
	"encoding/hex"
	"os"
	"path/filepath"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestSHA256File(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "test.bin")

	content := []byte("hello world")
	require.NoError(t, os.WriteFile(path, content, 0o644))

	got, err := SHA256File(path)
	require.NoError(t, err)

	h := sha256.Sum256(content)
	expected := hex.EncodeToString(h[:])
	assert.Equal(t, expected, got)
}

func TestSHA256File_NotExist(t *testing.T) {
	_, err := SHA256File("/nonexistent/path")
	require.Error(t, err)
}

func TestVerifyChecksum_Match(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "data.bin")

	content := []byte("test data for checksum")
	require.NoError(t, os.WriteFile(path, content, 0o644))

	h := sha256.Sum256(content)
	expected := hex.EncodeToString(h[:])

	err := VerifyChecksum(path, expected)
	require.NoError(t, err)
}

func TestVerifyChecksum_Mismatch(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "data.bin")
	require.NoError(t, os.WriteFile(path, []byte("actual content"), 0o644))

	err := VerifyChecksum(path, "0000000000000000000000000000000000000000000000000000000000000000")
	require.Error(t, err)
	assert.Contains(t, err.Error(), "checksum mismatch")
}

func TestVerifyChecksum_FileNotExist(t *testing.T) {
	err := VerifyChecksum("/nonexistent", "abc123")
	require.Error(t, err)
}
