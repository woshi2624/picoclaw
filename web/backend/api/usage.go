package api

import (
	"bufio"
	"encoding/json"
	"net/http"
	"os"
	"path/filepath"
	"sort"
	"strconv"
	"time"
)

func (h *Handler) registerUsageRoutes(mux *http.ServeMux) {
	mux.HandleFunc("GET /api/usage/summary", h.handleUsageSummary)
}

type usageEntry struct {
	Timestamp        string `json:"ts"`
	Model            string `json:"model"`
	PromptTokens     int    `json:"prompt_tokens"`
	CompletionTokens int    `json:"completion_tokens"`
	TotalTokens      int    `json:"total_tokens"`
	SessionKey       string `json:"session,omitempty"`
	AgentID          string `json:"agent_id,omitempty"`
}

type usageTotals struct {
	PromptTokens     int `json:"prompt_tokens"`
	CompletionTokens int `json:"completion_tokens"`
	TotalTokens      int `json:"total_tokens"`
	RequestCount     int `json:"request_count"`
}

type dailyUsage struct {
	Date             string `json:"date"`
	PromptTokens     int    `json:"prompt_tokens"`
	CompletionTokens int    `json:"completion_tokens"`
	TotalTokens      int    `json:"total_tokens"`
	RequestCount     int    `json:"request_count"`
}

type modelUsage struct {
	Model            string `json:"model"`
	PromptTokens     int    `json:"prompt_tokens"`
	CompletionTokens int    `json:"completion_tokens"`
	TotalTokens      int    `json:"total_tokens"`
	RequestCount     int    `json:"request_count"`
}

type usageSummaryResponse struct {
	Totals  usageTotals  `json:"totals"`
	Daily   []dailyUsage `json:"daily"`
	ByModel []modelUsage `json:"by_model"`
}

// handleUsageSummary reads usage.jsonl and returns aggregated token usage.
//
//	GET /api/usage/summary?days=30
func (h *Handler) handleUsageSummary(w http.ResponseWriter, r *http.Request) {
	days := 30
	if d := r.URL.Query().Get("days"); d != "" {
		if v, err := strconv.Atoi(d); err == nil && v > 0 {
			days = v
		}
	}

	workspace, err := h.workspaceDir()
	if err != nil {
		http.Error(w, "failed to resolve workspace", http.StatusInternalServerError)
		return
	}

	logPath := filepath.Join(workspace, "usage.jsonl")
	f, err := os.Open(logPath)
	if err != nil {
		// File doesn't exist yet — return empty summary
		w.Header().Set("Content-Type", "application/json")
		json.NewEncoder(w).Encode(usageSummaryResponse{
			Daily:   []dailyUsage{},
			ByModel: []modelUsage{},
		})
		return
	}
	defer f.Close()

	cutoff := time.Now().UTC().AddDate(0, 0, -days)

	dailyMap := make(map[string]*dailyUsage)
	modelMap := make(map[string]*modelUsage)
	var totals usageTotals

	scanner := bufio.NewScanner(f)
	scanner.Buffer(make([]byte, 0, 64*1024), 1024*1024)
	for scanner.Scan() {
		var entry usageEntry
		if err := json.Unmarshal(scanner.Bytes(), &entry); err != nil {
			continue
		}

		ts, err := time.Parse(time.RFC3339, entry.Timestamp)
		if err != nil {
			continue
		}
		if ts.Before(cutoff) {
			continue
		}

		totals.PromptTokens += entry.PromptTokens
		totals.CompletionTokens += entry.CompletionTokens
		totals.TotalTokens += entry.TotalTokens
		totals.RequestCount++

		date := ts.Format("2006-01-02")
		d, ok := dailyMap[date]
		if !ok {
			d = &dailyUsage{Date: date}
			dailyMap[date] = d
		}
		d.PromptTokens += entry.PromptTokens
		d.CompletionTokens += entry.CompletionTokens
		d.TotalTokens += entry.TotalTokens
		d.RequestCount++

		m, ok := modelMap[entry.Model]
		if !ok {
			m = &modelUsage{Model: entry.Model}
			modelMap[entry.Model] = m
		}
		m.PromptTokens += entry.PromptTokens
		m.CompletionTokens += entry.CompletionTokens
		m.TotalTokens += entry.TotalTokens
		m.RequestCount++
	}

	daily := make([]dailyUsage, 0, len(dailyMap))
	for _, d := range dailyMap {
		daily = append(daily, *d)
	}
	sort.Slice(daily, func(i, j int) bool { return daily[i].Date < daily[j].Date })

	byModel := make([]modelUsage, 0, len(modelMap))
	for _, m := range modelMap {
		byModel = append(byModel, *m)
	}
	sort.Slice(byModel, func(i, j int) bool { return byModel[i].TotalTokens > byModel[j].TotalTokens })

	w.Header().Set("Content-Type", "application/json")
	json.NewEncoder(w).Encode(usageSummaryResponse{
		Totals:  totals,
		Daily:   daily,
		ByModel: byModel,
	})
}
