package skills

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestExtractManifestInfo_Flat(t *testing.T) {
	manifest := map[string]any{
		"version": "2.0.0",
		"zip_url": "https://example.com/skill.zip",
		"sha256":  "ABC123",
	}
	version, zipURL, sha256 := extractManifestInfo(manifest)
	assert.Equal(t, "2.0.0", version)
	assert.Equal(t, "https://example.com/skill.zip", zipURL)
	assert.Equal(t, "abc123", sha256) // lowered
}

func TestExtractManifestInfo_Nested(t *testing.T) {
	manifest := map[string]any{
		"latest": map[string]any{
			"version":      "3.0.0",
			"download_url": "https://cdn.example.com/v3.zip",
			"checksum":     "DEF456",
		},
	}
	version, zipURL, sha256 := extractManifestInfo(manifest)
	assert.Equal(t, "3.0.0", version)
	assert.Equal(t, "https://cdn.example.com/v3.zip", zipURL)
	assert.Equal(t, "def456", sha256)
}

func TestExtractManifestInfo_Mixed(t *testing.T) {
	// Version at top level, URL nested.
	manifest := map[string]any{
		"version": "1.5.0",
		"release": map[string]any{
			"packageUrl": "https://releases.example.com/v1.5.zip",
		},
	}
	version, zipURL, _ := extractManifestInfo(manifest)
	assert.Equal(t, "1.5.0", version)
	assert.Equal(t, "https://releases.example.com/v1.5.zip", zipURL)
}

func TestReadUpdateURL(t *testing.T) {
	dir := t.TempDir()

	// Test with update_url.
	configData := map[string]any{"update_url": "https://example.com/manifest.json"}
	data, _ := json.Marshal(configData)
	configPath := filepath.Join(dir, "config.json")
	os.WriteFile(configPath, data, 0o644)

	url := readUpdateURL(configPath)
	assert.Equal(t, "https://example.com/manifest.json", url)

	// Test with selfUpdateUrl (camelCase).
	configData2 := map[string]any{"selfUpdateUrl": "https://example.com/manifest2.json"}
	data2, _ := json.Marshal(configData2)
	os.WriteFile(configPath, data2, 0o644)

	url2 := readUpdateURL(configPath)
	assert.Equal(t, "https://example.com/manifest2.json", url2)

	// Test with no file.
	noURL := readUpdateURL(filepath.Join(dir, "nonexistent.json"))
	assert.Empty(t, noURL)
}

func TestCheckForUpgrade_DryRun_UpgradeAvailable(t *testing.T) {
	workspace := t.TempDir()
	slug := "test-skill"
	skillDir := filepath.Join(workspace, "skills", slug)
	os.MkdirAll(skillDir, 0o755)

	// Create manifest server.
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		json.NewEncoder(w).Encode(map[string]any{
			"version": "2.0.0",
			"zip_url": "https://example.com/test-skill-2.0.0.zip",
		})
	}))
	defer srv.Close()

	// Create config.json with update_url.
	configData, _ := json.Marshal(map[string]any{"update_url": srv.URL + "/manifest.json"})
	os.WriteFile(filepath.Join(skillDir, "config.json"), configData, 0o644)

	entry := LockfileEntry{Name: slug, Version: "1.0.0", Source: "clawhub"}

	result := CheckForUpgrade(context.Background(), slug, entry, UpgradeOptions{
		WorkspaceDir: workspace,
		DryRun:       true,
	})

	assert.Equal(t, "upgrade_available", result.Status)
	assert.Equal(t, "1.0.0", result.OldVersion)
	assert.Equal(t, "2.0.0", result.NewVersion)
}

func TestCheckForUpgrade_AlreadyUpToDate(t *testing.T) {
	workspace := t.TempDir()
	slug := "test-skill"
	skillDir := filepath.Join(workspace, "skills", slug)
	os.MkdirAll(skillDir, 0o755)

	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		json.NewEncoder(w).Encode(map[string]any{
			"version": "1.0.0",
			"zip_url": "https://example.com/test-skill.zip",
		})
	}))
	defer srv.Close()

	configData, _ := json.Marshal(map[string]any{"update_url": srv.URL + "/manifest.json"})
	os.WriteFile(filepath.Join(skillDir, "config.json"), configData, 0o644)

	entry := LockfileEntry{Name: slug, Version: "1.0.0", Source: "clawhub"}

	result := CheckForUpgrade(context.Background(), slug, entry, UpgradeOptions{
		WorkspaceDir: workspace,
		DryRun:       true,
	})

	assert.Equal(t, "up_to_date", result.Status)
}

func TestCheckAllUpgrades(t *testing.T) {
	workspace := t.TempDir()

	// Create lockfile with two skills.
	lf := &Lockfile{
		Version: 1,
		Skills: map[string]LockfileEntry{
			"skill-a": {Name: "skill-a", Version: "1.0.0", Source: "clawhub"},
			"skill-b": {Name: "skill-b", Version: "2.0.0", Source: "clawhub"},
		},
	}
	require.NoError(t, SaveLockfile(workspace, lf))

	// Create skill directories (no config.json, so no update_url).
	for _, s := range []string{"skill-a", "skill-b"} {
		os.MkdirAll(filepath.Join(workspace, "skills", s), 0o755)
	}

	results, err := CheckAllUpgrades(context.Background(), UpgradeOptions{
		WorkspaceDir: workspace,
		DryRun:       true,
	})

	require.NoError(t, err)
	assert.Len(t, results, 2)
	// Both should be up_to_date since no update_url and no registry.
	for _, r := range results {
		assert.Equal(t, "up_to_date", r.Status)
	}
}

func TestCheckForUpgrade_ManifestFetchFails(t *testing.T) {
	workspace := t.TempDir()
	slug := "test-skill"
	skillDir := filepath.Join(workspace, "skills", slug)
	os.MkdirAll(skillDir, 0o755)

	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusInternalServerError)
	}))
	defer srv.Close()

	configData, _ := json.Marshal(map[string]any{"update_url": srv.URL + "/manifest.json"})
	os.WriteFile(filepath.Join(skillDir, "config.json"), configData, 0o644)

	entry := LockfileEntry{Name: slug, Version: "1.0.0", Source: "clawhub"}

	result := CheckForUpgrade(context.Background(), slug, entry, UpgradeOptions{
		WorkspaceDir: workspace,
		DryRun:       true,
	})

	assert.Equal(t, "failed", result.Status)
	assert.Contains(t, result.Error, "failed to fetch manifest")
}
