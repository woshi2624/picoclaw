package skills

import (
	"os"
	"path/filepath"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestLoadLockfile_NotExist(t *testing.T) {
	lf, err := LoadLockfile(t.TempDir())
	require.NoError(t, err)
	assert.Equal(t, 1, lf.Version)
	assert.Empty(t, lf.Skills)
}

func TestLoadAndSaveLockfile(t *testing.T) {
	workspace := t.TempDir()

	lf := &Lockfile{
		Version: 1,
		Skills: map[string]LockfileEntry{
			"weather": {
				Name:    "weather",
				ZipURL:  "https://example.com/weather.zip",
				Source:  "clawhub",
				Version: "1.0.0",
			},
		},
	}

	err := SaveLockfile(workspace, lf)
	require.NoError(t, err)

	// Verify file was created.
	path := filepath.Join(workspace, "skills", lockfileName)
	_, err = os.Stat(path)
	require.NoError(t, err)

	// Reload and verify.
	loaded, err := LoadLockfile(workspace)
	require.NoError(t, err)
	assert.Equal(t, 1, loaded.Version)
	assert.Len(t, loaded.Skills, 1)

	entry := loaded.Skills["weather"]
	assert.Equal(t, "weather", entry.Name)
	assert.Equal(t, "1.0.0", entry.Version)
	assert.Equal(t, "clawhub", entry.Source)
}

func TestLockfile_SetAndRemoveEntry(t *testing.T) {
	lf := &Lockfile{Version: 1, Skills: make(map[string]LockfileEntry)}

	lf.SetEntry("test-skill", LockfileEntry{
		Name:    "test-skill",
		Source:  "clawhub",
		Version: "2.0.0",
	})
	assert.Len(t, lf.Skills, 1)
	assert.Equal(t, "2.0.0", lf.Skills["test-skill"].Version)

	// Update existing entry.
	lf.SetEntry("test-skill", LockfileEntry{
		Name:    "test-skill",
		Source:  "clawhub",
		Version: "3.0.0",
	})
	assert.Len(t, lf.Skills, 1)
	assert.Equal(t, "3.0.0", lf.Skills["test-skill"].Version)

	lf.RemoveEntry("test-skill")
	assert.Empty(t, lf.Skills)

	// Removing non-existent key is a no-op.
	lf.RemoveEntry("non-existent")
	assert.Empty(t, lf.Skills)
}

func TestLockfile_SetEntry_NilMap(t *testing.T) {
	lf := &Lockfile{Version: 1}
	lf.SetEntry("slug", LockfileEntry{Name: "slug", Version: "1.0.0"})
	assert.Len(t, lf.Skills, 1)
}
