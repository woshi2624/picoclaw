package skills

import (
	"context"
	"encoding/json"
	"fmt"
	"io"
	"log/slog"
	"net/http"
	"os"
	"path/filepath"
	"strings"
	"time"

	"github.com/sipeed/picoclaw/pkg/utils"
)

// UpgradeResult describes the outcome of an upgrade check or execution for one skill.
type UpgradeResult struct {
	Slug       string `json:"slug"`
	OldVersion string `json:"old_version"`
	NewVersion string `json:"new_version"`
	Status     string `json:"status"` // "upgraded", "up_to_date", "failed", "upgrade_available"
	Error      string `json:"error,omitempty"`
}

// UpgradeOptions configures upgrade behavior.
type UpgradeOptions struct {
	WorkspaceDir string
	DryRun       bool              // If true, only check, don't install.
	Registry     SkillRegistry     // Used for downloading new versions.
	HTTPClient   *http.Client      // For fetching manifests; nil uses default.
}

// CheckForUpgrade checks a single skill for available updates by reading its
// config.json update_url, fetching the manifest, and comparing versions.
// If DryRun is false and an upgrade is available, it performs the upgrade.
func CheckForUpgrade(ctx context.Context, slug string, entry LockfileEntry, opts UpgradeOptions) *UpgradeResult {
	result := &UpgradeResult{
		Slug:       slug,
		OldVersion: entry.Version,
		Status:     "up_to_date",
	}

	skillDir := filepath.Join(opts.WorkspaceDir, "skills", slug)
	configPath := filepath.Join(skillDir, "config.json")

	// Read config.json to find update_url.
	updateURL := readUpdateURL(configPath)
	if updateURL == "" {
		// No update URL configured — try checking via registry metadata.
		if opts.Registry != nil {
			return checkUpgradeViaRegistry(ctx, slug, entry, opts)
		}
		result.Status = "up_to_date"
		return result
	}

	// Fetch manifest from update_url.
	client := opts.HTTPClient
	if client == nil {
		client = &http.Client{Timeout: 30 * time.Second}
	}

	manifest, err := fetchManifest(ctx, client, updateURL)
	if err != nil {
		result.Status = "failed"
		result.Error = fmt.Sprintf("failed to fetch manifest: %v", err)
		return result
	}

	latestVersion, zipURL, sha256 := extractManifestInfo(manifest)
	if latestVersion == "" {
		result.Status = "failed"
		result.Error = "manifest missing version"
		return result
	}

	result.NewVersion = latestVersion

	if !VersionIsNewer(latestVersion, entry.Version) {
		result.Status = "up_to_date"
		return result
	}

	if opts.DryRun {
		result.Status = "upgrade_available"
		return result
	}

	// Perform upgrade.
	if zipURL == "" {
		// Fall back to registry download.
		if opts.Registry != nil {
			return performRegistryUpgrade(ctx, slug, entry, latestVersion, sha256, opts)
		}
		result.Status = "failed"
		result.Error = "manifest missing download URL and no registry available"
		return result
	}

	if err := performManifestUpgrade(ctx, slug, skillDir, zipURL, sha256, opts); err != nil {
		result.Status = "failed"
		result.Error = err.Error()
		return result
	}

	// Update lockfile.
	updateLockfileVersion(opts.WorkspaceDir, slug, entry, latestVersion)

	result.Status = "upgraded"
	return result
}

// CheckAllUpgrades checks all skills in the lockfile for upgrades.
func CheckAllUpgrades(ctx context.Context, opts UpgradeOptions) ([]UpgradeResult, error) {
	lf, err := LoadLockfile(opts.WorkspaceDir)
	if err != nil {
		return nil, fmt.Errorf("failed to load lockfile: %w", err)
	}

	results := make([]UpgradeResult, 0, len(lf.Skills))
	for slug, entry := range lf.Skills {
		select {
		case <-ctx.Done():
			return results, ctx.Err()
		default:
		}
		r := CheckForUpgrade(ctx, slug, entry, opts)
		results = append(results, *r)
	}
	return results, nil
}

// --- internal helpers ---

// readUpdateURL reads config.json and extracts the update URL from various field names.
func readUpdateURL(configPath string) string {
	data, err := os.ReadFile(configPath)
	if err != nil {
		return ""
	}
	var raw map[string]any
	if err := json.Unmarshal(data, &raw); err != nil {
		return ""
	}
	for _, key := range []string{"self_update_url", "selfUpdateUrl", "update_url", "updateUrl", "manifest_url", "manifestUrl"} {
		if v, ok := raw[key]; ok {
			if s, ok := v.(string); ok && s != "" {
				return s
			}
		}
	}
	return ""
}

// fetchManifest fetches and parses a JSON manifest from a URL.
func fetchManifest(ctx context.Context, client *http.Client, urlStr string) (map[string]any, error) {
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, urlStr, nil)
	if err != nil {
		return nil, err
	}
	req.Header.Set("Accept", "application/json")

	resp, err := client.Do(req)
	if err != nil {
		return nil, err
	}
	defer resp.Body.Close()

	if resp.StatusCode < 200 || resp.StatusCode >= 300 {
		return nil, fmt.Errorf("HTTP %d", resp.StatusCode)
	}

	body, err := io.ReadAll(io.LimitReader(resp.Body, 2*1024*1024))
	if err != nil {
		return nil, err
	}

	var manifest map[string]any
	if err := json.Unmarshal(body, &manifest); err != nil {
		return nil, err
	}
	return manifest, nil
}

// extractManifestInfo extracts version, zip_url, and sha256 from a manifest,
// supporting both flat and nested formats (same as Python extract_update_manifest_info).
func extractManifestInfo(manifest map[string]any) (version, zipURL, sha256 string) {
	candidates := []map[string]any{manifest}
	for _, key := range []string{"latest", "release", "data", "skill", "package"} {
		if nested, ok := manifest[key]; ok {
			if m, ok := nested.(map[string]any); ok {
				candidates = append(candidates, m)
			}
		}
	}

	for _, item := range candidates {
		if version == "" {
			version = firstNonEmpty(item, "version", "latest_version", "latestVersion")
		}
		if zipURL == "" {
			zipURL = firstNonEmpty(item, "zip_url", "zipUrl", "download_url", "downloadUrl", "package_url", "packageUrl", "url")
		}
		if sha256 == "" {
			sha256 = firstNonEmpty(item, "sha256", "sha_256", "checksum")
		}
	}
	sha256 = strings.ToLower(sha256)
	return
}

func firstNonEmpty(m map[string]any, keys ...string) string {
	for _, k := range keys {
		if v, ok := m[k]; ok {
			if s, ok := v.(string); ok && s != "" {
				return s
			}
		}
	}
	return ""
}

// checkUpgradeViaRegistry checks for upgrades by querying the registry metadata.
func checkUpgradeViaRegistry(ctx context.Context, slug string, entry LockfileEntry, opts UpgradeOptions) *UpgradeResult {
	result := &UpgradeResult{
		Slug:       slug,
		OldVersion: entry.Version,
		Status:     "up_to_date",
	}

	meta, err := opts.Registry.GetSkillMeta(ctx, slug)
	if err != nil {
		result.Status = "failed"
		result.Error = fmt.Sprintf("failed to get registry metadata: %v", err)
		return result
	}

	if meta.LatestVersion == "" || !VersionIsNewer(meta.LatestVersion, entry.Version) {
		return result
	}

	result.NewVersion = meta.LatestVersion

	if opts.DryRun {
		result.Status = "upgrade_available"
		return result
	}

	return performRegistryUpgrade(ctx, slug, entry, meta.LatestVersion, meta.ExpectedSHA256, opts)
}

// performRegistryUpgrade performs an upgrade using the registry's DownloadAndInstall.
func performRegistryUpgrade(ctx context.Context, slug string, entry LockfileEntry, newVersion, sha256 string, opts UpgradeOptions) *UpgradeResult {
	result := &UpgradeResult{
		Slug:       slug,
		OldVersion: entry.Version,
		NewVersion: newVersion,
	}

	skillDir := filepath.Join(opts.WorkspaceDir, "skills", slug)

	// Preserve config.json if it exists.
	configBackup := backupConfigJSON(skillDir)

	// Remove old skill and reinstall.
	os.RemoveAll(skillDir)

	_, err := opts.Registry.DownloadAndInstall(ctx, slug, newVersion, skillDir)
	if err != nil {
		result.Status = "failed"
		result.Error = fmt.Sprintf("registry upgrade failed: %v", err)
		return result
	}

	// Restore config.json.
	restoreConfigJSON(skillDir, configBackup)

	updateLockfileVersion(opts.WorkspaceDir, slug, entry, newVersion)

	result.Status = "upgraded"
	return result
}

// performManifestUpgrade downloads from a manifest URL and installs.
func performManifestUpgrade(ctx context.Context, slug, skillDir, zipURL, expectedSHA string, opts UpgradeOptions) error {
	// Preserve config.json.
	configBackup := backupConfigJSON(skillDir)

	// Download to temp file.
	client := opts.HTTPClient
	if client == nil {
		client = &http.Client{Timeout: 2 * time.Minute}
	}

	req, err := http.NewRequestWithContext(ctx, http.MethodGet, zipURL, nil)
	if err != nil {
		return err
	}
	req.Header.Set("Accept", "application/zip")

	resp, err := client.Do(req)
	if err != nil {
		return fmt.Errorf("download failed: %w", err)
	}
	defer resp.Body.Close()

	if resp.StatusCode < 200 || resp.StatusCode >= 300 {
		return fmt.Errorf("download HTTP %d", resp.StatusCode)
	}

	tmpFile, err := os.CreateTemp("", "picoclaw-upgrade-*")
	if err != nil {
		return err
	}
	tmpPath := tmpFile.Name()
	defer os.Remove(tmpPath)

	if _, err := io.Copy(tmpFile, io.LimitReader(resp.Body, 50*1024*1024+1)); err != nil {
		tmpFile.Close()
		return err
	}
	tmpFile.Close()

	// Verify checksum if provided.
	if expectedSHA != "" {
		if err := VerifyChecksum(tmpPath, expectedSHA); err != nil {
			return fmt.Errorf("checksum verification failed: %w", err)
		}
		slog.Info("upgrade SHA256 verified", "slug", slug)
	}

	// Remove old and extract new.
	os.RemoveAll(skillDir)
	if err := utils.ExtractZipFile(tmpPath, skillDir); err != nil {
		return fmt.Errorf("extraction failed: %w", err)
	}

	// Restore config.json.
	restoreConfigJSON(skillDir, configBackup)

	return nil
}

func backupConfigJSON(skillDir string) []byte {
	data, err := os.ReadFile(filepath.Join(skillDir, "config.json"))
	if err != nil {
		return nil
	}
	return data
}

func restoreConfigJSON(skillDir string, data []byte) {
	if data == nil {
		return
	}
	os.WriteFile(filepath.Join(skillDir, "config.json"), data, 0o644)
}

func updateLockfileVersion(workspaceDir, slug string, entry LockfileEntry, newVersion string) {
	lf, err := LoadLockfile(workspaceDir)
	if err != nil {
		slog.Warn("failed to load lockfile for upgrade update", "slug", slug, "error", err)
		return
	}
	entry.Version = newVersion
	lf.SetEntry(slug, entry)
	if err := SaveLockfile(workspaceDir, lf); err != nil {
		slog.Warn("failed to save lockfile after upgrade", "slug", slug, "error", err)
	}
}
