package skills

import (
	"context"
	"encoding/json"
	"fmt"
	"io"
	"log/slog"
	"net/http"
	"net/url"
	"os"
	"strings"
	"time"

	"github.com/sipeed/picoclaw/pkg/utils"
)

const (
	defaultClawHubTimeout  = 30 * time.Second
	defaultMaxZipSize      = 50 * 1024 * 1024 // 50 MB
	defaultMaxResponseSize = 2 * 1024 * 1024  // 2 MB
)

// ClawHubRegistry implements SkillRegistry for the ClawHub platform.
type ClawHubRegistry struct {
	baseURL                     string
	authToken                   string // Optional - for elevated rate limits
	searchPath                  string // Search API
	skillsPath                  string // For retrieving skill metadata
	downloadPath                string // For fetching ZIP files for download
	primaryDownloadURLTemplate  string // Optional template for primary download source
	fallbackDownloadURLTemplate string // Optional template for fallback download source
	maxZipSize                  int
	maxResponseSize             int
	client                      *http.Client
}

// NewClawHubRegistry creates a new ClawHub registry client from config.
func NewClawHubRegistry(cfg ClawHubConfig) *ClawHubRegistry {
	baseURL := cfg.BaseURL
	if baseURL == "" {
		baseURL = "https://clawhub.ai"
	}
	searchPath := cfg.SearchPath
	if searchPath == "" {
		searchPath = "/api/v1/search"
	}
	skillsPath := cfg.SkillsPath
	if skillsPath == "" {
		skillsPath = "/api/v1/skills"
	}
	downloadPath := cfg.DownloadPath
	if downloadPath == "" {
		downloadPath = "/api/v1/download"
	}

	timeout := defaultClawHubTimeout
	if cfg.Timeout > 0 {
		timeout = time.Duration(cfg.Timeout) * time.Second
	}

	maxZip := defaultMaxZipSize
	if cfg.MaxZipSize > 0 {
		maxZip = cfg.MaxZipSize
	}

	maxResp := defaultMaxResponseSize
	if cfg.MaxResponseSize > 0 {
		maxResp = cfg.MaxResponseSize
	}

	return &ClawHubRegistry{
		baseURL:                     baseURL,
		authToken:                   cfg.AuthToken,
		searchPath:                  searchPath,
		skillsPath:                  skillsPath,
		downloadPath:                downloadPath,
		primaryDownloadURLTemplate:  cfg.PrimaryDownloadURLTemplate,
		fallbackDownloadURLTemplate: cfg.FallbackDownloadURLTemplate,
		maxZipSize:                  maxZip,
		maxResponseSize:             maxResp,
		client: &http.Client{
			Timeout: timeout,
			Transport: &http.Transport{
				MaxIdleConns:        5,
				IdleConnTimeout:     30 * time.Second,
				TLSHandshakeTimeout: 10 * time.Second,
			},
		},
	}
}

func (c *ClawHubRegistry) Name() string {
	return "clawhub"
}

// --- Search ---

type clawhubSearchResponse struct {
	Results []clawhubSearchResult `json:"results"`
}

type clawhubSearchResult struct {
	Score       float64 `json:"score"`
	Slug        *string `json:"slug"`
	DisplayName *string `json:"displayName"`
	Summary     *string `json:"summary"`
	Version     *string `json:"version"`
}

func (c *ClawHubRegistry) Search(ctx context.Context, query string, limit int) ([]SearchResult, error) {
	u, err := url.Parse(c.baseURL + c.searchPath)
	if err != nil {
		return nil, fmt.Errorf("invalid base URL: %w", err)
	}

	q := u.Query()
	q.Set("q", query)
	if limit > 0 {
		q.Set("limit", fmt.Sprintf("%d", limit))
	}
	u.RawQuery = q.Encode()

	body, err := c.doGet(ctx, u.String())
	if err != nil {
		return nil, fmt.Errorf("search request failed: %w", err)
	}

	var resp clawhubSearchResponse
	if err := json.Unmarshal(body, &resp); err != nil {
		return nil, fmt.Errorf("failed to parse search response: %w", err)
	}

	results := make([]SearchResult, 0, len(resp.Results))
	for _, r := range resp.Results {
		slug := utils.DerefStr(r.Slug, "")
		if slug == "" {
			continue
		}

		summary := utils.DerefStr(r.Summary, "")
		if summary == "" {
			continue
		}

		displayName := utils.DerefStr(r.DisplayName, "")
		if displayName == "" {
			displayName = slug
		}

		results = append(results, SearchResult{
			Score:        r.Score,
			Slug:         slug,
			DisplayName:  displayName,
			Summary:      summary,
			Version:      utils.DerefStr(r.Version, ""),
			RegistryName: c.Name(),
		})
	}

	return results, nil
}

// --- GetSkillMeta ---

type clawhubSkillResponse struct {
	Slug          string                 `json:"slug"`
	DisplayName   string                 `json:"displayName"`
	Summary       string                 `json:"summary"`
	LatestVersion *clawhubVersionInfo    `json:"latestVersion"`
	Moderation    *clawhubModerationInfo `json:"moderation"`
}

type clawhubVersionInfo struct {
	Version string `json:"version"`
	SHA256  string `json:"sha256"`
}

type clawhubModerationInfo struct {
	IsMalwareBlocked bool `json:"isMalwareBlocked"`
	IsSuspicious     bool `json:"isSuspicious"`
}

func (c *ClawHubRegistry) GetSkillMeta(ctx context.Context, slug string) (*SkillMeta, error) {
	if err := utils.ValidateSkillIdentifier(slug); err != nil {
		return nil, fmt.Errorf("invalid slug %q: error: %s", slug, err.Error())
	}

	u := c.baseURL + c.skillsPath + "/" + url.PathEscape(slug)

	body, err := c.doGet(ctx, u)
	if err != nil {
		return nil, fmt.Errorf("skill metadata request failed: %w", err)
	}

	var resp clawhubSkillResponse
	if err := json.Unmarshal(body, &resp); err != nil {
		return nil, fmt.Errorf("failed to parse skill metadata: %w", err)
	}

	meta := &SkillMeta{
		Slug:         resp.Slug,
		DisplayName:  resp.DisplayName,
		Summary:      resp.Summary,
		RegistryName: c.Name(),
	}

	if resp.LatestVersion != nil {
		meta.LatestVersion = resp.LatestVersion.Version
		meta.ExpectedSHA256 = resp.LatestVersion.SHA256
	}
	if resp.Moderation != nil {
		meta.IsMalwareBlocked = resp.Moderation.IsMalwareBlocked
		meta.IsSuspicious = resp.Moderation.IsSuspicious
	}

	return meta, nil
}

// --- DownloadAndInstall ---

// DownloadAndInstall fetches metadata (with fallback), resolves version,
// downloads the skill ZIP, and extracts it to targetDir.
// Returns an InstallResult for the caller to use for moderation decisions.
func (c *ClawHubRegistry) DownloadAndInstall(
	ctx context.Context,
	slug, version, targetDir string,
) (*InstallResult, error) {
	if err := utils.ValidateSkillIdentifier(slug); err != nil {
		return nil, fmt.Errorf("invalid slug %q: error: %s", slug, err.Error())
	}

	// Step 1: Fetch metadata (with fallback).
	result := &InstallResult{}
	meta, err := c.GetSkillMeta(ctx, slug)
	if err != nil {
		// Fallback: proceed without metadata.
		meta = nil
	}

	if meta != nil {
		result.IsMalwareBlocked = meta.IsMalwareBlocked
		result.IsSuspicious = meta.IsSuspicious
		result.Summary = meta.Summary
	}

	// Step 2: Resolve version.
	installVersion := version
	if installVersion == "" && meta != nil {
		installVersion = meta.LatestVersion
	}
	if installVersion == "" {
		installVersion = "latest"
	}
	result.Version = installVersion

	// Step 3: Download ZIP via fallback chain.
	tmpPath, err := c.downloadWithFallback(ctx, slug, installVersion)
	if err != nil {
		return nil, fmt.Errorf("download failed: %w", err)
	}
	defer os.Remove(tmpPath)

	// Step 3.5: Verify SHA256 checksum (if available from metadata).
	if meta != nil && meta.ExpectedSHA256 != "" {
		if err := VerifyChecksum(tmpPath, meta.ExpectedSHA256); err != nil {
			return nil, fmt.Errorf("checksum verification failed: %w", err)
		}
		slog.Info("SHA256 checksum verified", "slug", slug)
	}

	// Step 4: Extract from file on disk.
	if err := utils.ExtractZipFile(tmpPath, targetDir); err != nil {
		return nil, err
	}

	return result, nil
}

// --- Download fallback chain ---

// expandTemplate replaces {slug} and {version} placeholders in a URL template.
func expandTemplate(tmpl, slug, version string) string {
	s := strings.ReplaceAll(tmpl, "{slug}", slug)
	s = strings.ReplaceAll(s, "{version}", version)
	return s
}

// downloadWithFallback tries multiple download sources in order:
// 1. Primary template URL (if configured)
// 2. Fallback template URL (if configured)
// 3. Original ClawHub download URL
// Returns the path to a temp file on success, or the last error if all fail.
func (c *ClawHubRegistry) downloadWithFallback(ctx context.Context, slug, version string) (string, error) {
	type candidate struct {
		label string
		url   string
	}

	var candidates []candidate

	if c.primaryDownloadURLTemplate != "" {
		candidates = append(candidates, candidate{
			label: "primary",
			url:   expandTemplate(c.primaryDownloadURLTemplate, slug, version),
		})
	}
	if c.fallbackDownloadURLTemplate != "" {
		candidates = append(candidates, candidate{
			label: "fallback",
			url:   expandTemplate(c.fallbackDownloadURLTemplate, slug, version),
		})
	}

	// Always include the original ClawHub URL as last resort.
	u, err := url.Parse(c.baseURL + c.downloadPath)
	if err != nil {
		return "", fmt.Errorf("invalid base URL: %w", err)
	}
	q := u.Query()
	q.Set("slug", slug)
	if version != "latest" {
		q.Set("version", version)
	}
	u.RawQuery = q.Encode()
	candidates = append(candidates, candidate{label: "clawhub", url: u.String()})

	var lastErr error
	for _, c2 := range candidates {
		tmpPath, dlErr := c.downloadToTempFileWithRetry(ctx, c2.url)
		if dlErr == nil {
			if len(candidates) > 1 {
				slog.Info("skill download succeeded", "source", c2.label, "slug", slug)
			}
			return tmpPath, nil
		}
		lastErr = dlErr
		slog.Warn("skill download failed, trying next source",
			"source", c2.label, "slug", slug, "error", dlErr)
	}
	return "", lastErr
}

// --- HTTP helper ---

func (c *ClawHubRegistry) doGet(ctx context.Context, urlStr string) ([]byte, error) {
	req, err := c.newGetRequest(ctx, urlStr, "application/json")
	if err != nil {
		return nil, err
	}

	resp, err := utils.DoRequestWithRetry(c.client, req)
	if err != nil {
		return nil, err
	}
	defer resp.Body.Close()

	// Limit response body read to prevent memory issues.
	body, err := io.ReadAll(io.LimitReader(resp.Body, int64(c.maxResponseSize)))
	if err != nil {
		return nil, fmt.Errorf("failed to read response: %w", err)
	}

	if resp.StatusCode < 200 || resp.StatusCode >= 300 {
		return nil, fmt.Errorf("HTTP %d: %s", resp.StatusCode, string(body))
	}

	return body, nil
}

func (c *ClawHubRegistry) newGetRequest(ctx context.Context, urlStr, accept string) (*http.Request, error) {
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, urlStr, nil)
	if err != nil {
		return nil, err
	}
	req.Header.Set("Accept", accept)
	if c.authToken != "" {
		req.Header.Set("Authorization", "Bearer "+c.authToken)
	}
	return req, nil
}

func (c *ClawHubRegistry) downloadToTempFileWithRetry(ctx context.Context, urlStr string) (string, error) {
	req, err := c.newGetRequest(ctx, urlStr, "application/zip")
	if err != nil {
		return "", err
	}

	resp, err := utils.DoRequestWithRetry(c.client, req)
	if err != nil {
		return "", err
	}
	defer resp.Body.Close()

	if resp.StatusCode < 200 || resp.StatusCode >= 300 {
		errBody := make([]byte, 512)
		n, _ := io.ReadFull(resp.Body, errBody)
		return "", fmt.Errorf("HTTP %d: %s", resp.StatusCode, string(errBody[:n]))
	}

	tmpFile, err := os.CreateTemp("", "picoclaw-dl-*")
	if err != nil {
		return "", fmt.Errorf("failed to create temp file: %w", err)
	}
	tmpPath := tmpFile.Name()

	cleanup := func() {
		_ = tmpFile.Close()
		_ = os.Remove(tmpPath)
	}

	src := io.LimitReader(resp.Body, int64(c.maxZipSize)+1)
	written, err := io.Copy(tmpFile, src)
	if err != nil {
		cleanup()
		return "", fmt.Errorf("download write failed: %w", err)
	}

	if written > int64(c.maxZipSize) {
		cleanup()
		return "", fmt.Errorf("download too large: %d bytes (max %d)", written, c.maxZipSize)
	}

	if err := tmpFile.Close(); err != nil {
		_ = os.Remove(tmpPath)
		return "", fmt.Errorf("failed to close temp file: %w", err)
	}

	return tmpPath, nil
}
