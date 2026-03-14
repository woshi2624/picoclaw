package api

import (
	"context"
	"encoding/json"
	"fmt"
	"log/slog"
	"net/http"
	"os"
	"path/filepath"
	"strings"
	"sync"
	"time"

	"github.com/sipeed/picoclaw/pkg/config"
	"github.com/sipeed/picoclaw/pkg/fileutil"
	"github.com/sipeed/picoclaw/pkg/skills"
	"github.com/sipeed/picoclaw/pkg/utils"
)

// registerSkillRoutes binds skill management endpoints to the ServeMux.
func (h *Handler) registerSkillRoutes(mux *http.ServeMux) {
	mux.HandleFunc("GET /api/skills", h.handleListSkills)
	mux.HandleFunc("GET /api/skills/search", h.handleSearchSkills)
	mux.HandleFunc("POST /api/skills/install", h.handleInstallSkill)
	mux.HandleFunc("POST /api/skills/upgrade", h.handleUpgradeSkills)
	mux.HandleFunc("DELETE /api/skills/{name}", h.handleUninstallSkill)
}

// handleListSkills returns all installed skills (workspace > global > builtin).
//
//	GET /api/skills
func (h *Handler) handleListSkills(w http.ResponseWriter, r *http.Request) {
	loader, err := h.buildSkillsLoader()
	if err != nil {
		http.Error(w, fmt.Sprintf("failed to initialize skills loader: %v", err), http.StatusInternalServerError)
		return
	}

	installed := loader.ListSkills()

	w.Header().Set("Content-Type", "application/json")
	json.NewEncoder(w).Encode(map[string]any{
		"skills": installed,
		"total":  len(installed),
	})
}

// handleSearchSkills searches registries for installable skills.
//
//	GET /api/skills/search?q=query&limit=10
func (h *Handler) handleSearchSkills(w http.ResponseWriter, r *http.Request) {
	query := strings.TrimSpace(r.URL.Query().Get("q"))
	if query == "" {
		http.Error(w, `{"error":"query parameter 'q' is required"}`, http.StatusBadRequest)
		return
	}

	limit := 10
	if l := r.URL.Query().Get("limit"); l != "" {
		var n int
		if _, err := fmt.Sscanf(l, "%d", &n); err == nil && n >= 1 && n <= 50 {
			limit = n
		}
	}

	registryMgr, err := h.buildRegistryManager()
	if err != nil {
		http.Error(w, fmt.Sprintf(`{"error":"failed to build registry manager: %v"}`, err), http.StatusInternalServerError)
		return
	}

	ctx, cancel := context.WithTimeout(r.Context(), 30*time.Second)
	defer cancel()

	results, err := registryMgr.SearchAll(ctx, query, limit)
	if err != nil {
		http.Error(w, fmt.Sprintf(`{"error":"search failed: %v"}`, err), http.StatusInternalServerError)
		return
	}

	w.Header().Set("Content-Type", "application/json")
	json.NewEncoder(w).Encode(map[string]any{
		"results": results,
		"total":   len(results),
		"query":   query,
	})
}

// installRequest is the JSON body for POST /api/skills/install.
type installRequest struct {
	Slug     string `json:"slug"`
	Registry string `json:"registry"`
	Version  string `json:"version"`
	Force    bool   `json:"force"`
}

// skillInstallMu serializes skill install/uninstall operations.
var skillInstallMu sync.Mutex

// handleInstallSkill installs a skill from a registry.
//
//	POST /api/skills/install
func (h *Handler) handleInstallSkill(w http.ResponseWriter, r *http.Request) {
	var req installRequest
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		http.Error(w, `{"error":"invalid request body"}`, http.StatusBadRequest)
		return
	}

	if err := utils.ValidateSkillIdentifier(req.Slug); err != nil {
		http.Error(w, fmt.Sprintf(`{"error":"invalid slug: %s"}`, err.Error()), http.StatusBadRequest)
		return
	}
	if err := utils.ValidateSkillIdentifier(req.Registry); err != nil {
		http.Error(w, fmt.Sprintf(`{"error":"invalid registry: %s"}`, err.Error()), http.StatusBadRequest)
		return
	}

	cfg, err := config.LoadConfig(h.configPath)
	if err != nil {
		http.Error(w, fmt.Sprintf(`{"error":"failed to load config: %v"}`, err), http.StatusInternalServerError)
		return
	}

	workspace := cfg.WorkspacePath()
	skillsDir := filepath.Join(workspace, "skills")
	targetDir := filepath.Join(skillsDir, req.Slug)

	// Serialize install/uninstall operations.
	skillInstallMu.Lock()
	defer skillInstallMu.Unlock()

	if !req.Force {
		if _, err := os.Stat(targetDir); err == nil {
			w.Header().Set("Content-Type", "application/json")
			w.WriteHeader(http.StatusConflict)
			json.NewEncoder(w).Encode(map[string]any{
				"error": fmt.Sprintf("skill '%s' already installed at %s. Use force=true to reinstall.", req.Slug, targetDir),
			})
			return
		}
	} else {
		os.RemoveAll(targetDir)
	}

	registryMgr, err := h.buildRegistryManager()
	if err != nil {
		http.Error(w, fmt.Sprintf(`{"error":"failed to build registry manager: %v"}`, err), http.StatusInternalServerError)
		return
	}

	registry := registryMgr.GetRegistry(req.Registry)
	if registry == nil {
		http.Error(w, fmt.Sprintf(`{"error":"registry '%s' not found"}`, req.Registry), http.StatusBadRequest)
		return
	}

	if err := os.MkdirAll(skillsDir, 0o755); err != nil {
		http.Error(w, fmt.Sprintf(`{"error":"failed to create skills directory: %v"}`, err), http.StatusInternalServerError)
		return
	}

	ctx, cancel := context.WithTimeout(r.Context(), 2*time.Minute)
	defer cancel()

	result, err := registry.DownloadAndInstall(ctx, req.Slug, req.Version, targetDir)
	if err != nil {
		os.RemoveAll(targetDir)
		http.Error(w, fmt.Sprintf(`{"error":"installation failed: %v"}`, err), http.StatusInternalServerError)
		return
	}

	if result.IsMalwareBlocked {
		os.RemoveAll(targetDir)
		w.Header().Set("Content-Type", "application/json")
		w.WriteHeader(http.StatusForbidden)
		json.NewEncoder(w).Encode(map[string]any{
			"error": fmt.Sprintf("skill '%s' is flagged as malicious and cannot be installed", req.Slug),
		})
		return
	}

	// Write origin metadata (same format as LLM tool).
	writeSkillOriginMeta(targetDir, registry.Name(), req.Slug, result.Version)

	// Update lockfile (best-effort, do not block on failure).
	if lf, lfErr := skills.LoadLockfile(workspace); lfErr == nil {
		lf.SetEntry(req.Slug, skills.LockfileEntry{
			Name:    req.Slug,
			Source:  registry.Name(),
			Version: result.Version,
		})
		if saveErr := skills.SaveLockfile(workspace, lf); saveErr != nil {
			slog.Warn("failed to update lockfile after install", "slug", req.Slug, "error", saveErr)
		}
	}

	resp := map[string]any{
		"status":  "ok",
		"slug":    req.Slug,
		"version": result.Version,
		"path":    targetDir,
	}
	if result.IsSuspicious {
		resp["warning"] = fmt.Sprintf("skill '%s' is flagged as suspicious (may contain risky patterns)", req.Slug)
	}
	if result.Summary != "" {
		resp["summary"] = result.Summary
	}

	w.Header().Set("Content-Type", "application/json")
	json.NewEncoder(w).Encode(resp)
}

// handleUninstallSkill removes an installed skill.
//
//	DELETE /api/skills/{name}
func (h *Handler) handleUninstallSkill(w http.ResponseWriter, r *http.Request) {
	name := r.PathValue("name")
	if err := utils.ValidateSkillIdentifier(name); err != nil {
		http.Error(w, fmt.Sprintf(`{"error":"invalid skill name: %s"}`, err.Error()), http.StatusBadRequest)
		return
	}

	cfg, err := config.LoadConfig(h.configPath)
	if err != nil {
		http.Error(w, fmt.Sprintf(`{"error":"failed to load config: %v"}`, err), http.StatusInternalServerError)
		return
	}

	workspace := cfg.WorkspacePath()
	skillDir := filepath.Join(workspace, "skills", name)

	skillInstallMu.Lock()
	defer skillInstallMu.Unlock()

	if _, err := os.Stat(skillDir); os.IsNotExist(err) {
		http.Error(w, fmt.Sprintf(`{"error":"skill '%s' not found"}`, name), http.StatusNotFound)
		return
	}

	if err := os.RemoveAll(skillDir); err != nil {
		http.Error(w, fmt.Sprintf(`{"error":"failed to remove skill: %v"}`, err), http.StatusInternalServerError)
		return
	}

	// Remove from lockfile (best-effort).
	if lf, lfErr := skills.LoadLockfile(workspace); lfErr == nil {
		lf.RemoveEntry(name)
		if saveErr := skills.SaveLockfile(workspace, lf); saveErr != nil {
			slog.Warn("failed to update lockfile after uninstall", "name", name, "error", saveErr)
		}
	}

	w.Header().Set("Content-Type", "application/json")
	json.NewEncoder(w).Encode(map[string]any{
		"status": "ok",
		"name":   name,
	})
}

// upgradeRequest is the JSON body for POST /api/skills/upgrade.
type upgradeRequest struct {
	Slug   string `json:"slug"`
	DryRun bool   `json:"dry_run"`
}

// handleUpgradeSkills checks for and optionally applies skill upgrades.
//
//	POST /api/skills/upgrade
func (h *Handler) handleUpgradeSkills(w http.ResponseWriter, r *http.Request) {
	var req upgradeRequest
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		http.Error(w, `{"error":"invalid request body"}`, http.StatusBadRequest)
		return
	}

	cfg, err := config.LoadConfig(h.configPath)
	if err != nil {
		http.Error(w, fmt.Sprintf(`{"error":"failed to load config: %v"}`, err), http.StatusInternalServerError)
		return
	}

	workspace := cfg.WorkspacePath()

	registryMgr, err := h.buildRegistryManager()
	if err != nil {
		http.Error(w, fmt.Sprintf(`{"error":"failed to build registry manager: %v"}`, err), http.StatusInternalServerError)
		return
	}

	// Use the first available registry for downloads.
	var registry skills.SkillRegistry
	if reg := registryMgr.GetRegistry("clawhub"); reg != nil {
		registry = reg
	}

	ctx, cancel := context.WithTimeout(r.Context(), 3*time.Minute)
	defer cancel()

	opts := skills.UpgradeOptions{
		WorkspaceDir: workspace,
		DryRun:       req.DryRun,
		Registry:     registry,
	}

	skillInstallMu.Lock()
	defer skillInstallMu.Unlock()

	var results []skills.UpgradeResult

	if req.Slug != "" {
		// Single skill upgrade.
		if err := utils.ValidateSkillIdentifier(req.Slug); err != nil {
			http.Error(w, fmt.Sprintf(`{"error":"invalid slug: %s"}`, err.Error()), http.StatusBadRequest)
			return
		}

		lf, lfErr := skills.LoadLockfile(workspace)
		if lfErr != nil {
			http.Error(w, fmt.Sprintf(`{"error":"failed to load lockfile: %v"}`, lfErr), http.StatusInternalServerError)
			return
		}

		entry, ok := lf.Skills[req.Slug]
		if !ok {
			http.Error(w, fmt.Sprintf(`{"error":"skill '%s' not found in lockfile"}`, req.Slug), http.StatusNotFound)
			return
		}

		result := skills.CheckForUpgrade(ctx, req.Slug, entry, opts)
		results = []skills.UpgradeResult{*result}
	} else {
		// All skills upgrade.
		var allErr error
		results, allErr = skills.CheckAllUpgrades(ctx, opts)
		if allErr != nil {
			http.Error(w, fmt.Sprintf(`{"error":"upgrade check failed: %v"}`, allErr), http.StatusInternalServerError)
			return
		}
	}

	w.Header().Set("Content-Type", "application/json")
	json.NewEncoder(w).Encode(map[string]any{
		"results": results,
		"total":   len(results),
	})
}

// --- helpers ---

// buildSkillsLoader creates a SkillsLoader from the current config.
func (h *Handler) buildSkillsLoader() (*skills.SkillsLoader, error) {
	cfg, err := config.LoadConfig(h.configPath)
	if err != nil {
		return nil, fmt.Errorf("failed to load config: %w", err)
	}

	workspace := cfg.WorkspacePath()
	globalDir := filepath.Dir(h.configPath)
	globalSkillsDir := filepath.Join(globalDir, "skills")
	builtinSkillsDir := filepath.Join(globalDir, "picoclaw", "skills")

	return skills.NewSkillsLoader(workspace, globalSkillsDir, builtinSkillsDir), nil
}

// buildRegistryManager creates a RegistryManager from the current config.
func (h *Handler) buildRegistryManager() (*skills.RegistryManager, error) {
	cfg, err := config.LoadConfig(h.configPath)
	if err != nil {
		return nil, fmt.Errorf("failed to load config: %w", err)
	}

	chCfg := cfg.Tools.Skills.Registries.ClawHub
	regCfg := skills.RegistryConfig{
		ClawHub: skills.ClawHubConfig{
			Enabled:                     chCfg.Enabled,
			BaseURL:                     chCfg.BaseURL,
			AuthToken:                   chCfg.AuthToken,
			SearchPath:                  chCfg.SearchPath,
			SkillsPath:                  chCfg.SkillsPath,
			DownloadPath:                chCfg.DownloadPath,
			Timeout:                     chCfg.Timeout,
			MaxZipSize:                  chCfg.MaxZipSize,
			MaxResponseSize:             chCfg.MaxResponseSize,
			PrimaryDownloadURLTemplate:  chCfg.PrimaryDownloadURLTemplate,
			FallbackDownloadURLTemplate: chCfg.FallbackDownloadURLTemplate,
		},
		MaxConcurrentSearches: cfg.Tools.Skills.MaxConcurrentSearches,
	}

	return skills.NewRegistryManagerFromConfig(regCfg), nil
}

// skillOriginMeta tracks which registry a skill was installed from.
type skillOriginMeta struct {
	Version          int    `json:"version"`
	Registry         string `json:"registry"`
	Slug             string `json:"slug"`
	InstalledVersion string `json:"installed_version"`
	InstalledAt      int64  `json:"installed_at"`
}

func writeSkillOriginMeta(targetDir, registryName, slug, version string) {
	meta := skillOriginMeta{
		Version:          1,
		Registry:         registryName,
		Slug:             slug,
		InstalledVersion: version,
		InstalledAt:      time.Now().UnixMilli(),
	}

	data, err := json.MarshalIndent(meta, "", "  ")
	if err != nil {
		return
	}

	fileutil.WriteFileAtomic(filepath.Join(targetDir, ".skill-origin.json"), data, 0o600)
}
