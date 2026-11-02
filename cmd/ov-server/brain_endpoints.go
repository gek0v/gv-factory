// SPDX-License-Identifier: Apache-2.0
// SPDX-FileCopyrightText: 2026 gv-factory contributors

package main

import (
	"crypto/sha256"
	"encoding/binary"
	"encoding/json"
	"math"
	"net/http"
	"sort"
	"strings"

	"github.com/gv-factory/gv-factory/internal/brain"
)

// BrainSearchRequest defines the payload accepted by POST /api/v1/brain/search.
type BrainSearchRequest struct {
	Query    string   `json:"query"`
	Category string   `json:"category,omitempty"` // "all" | "knowledge" | "rules" | "episodes"
	Limit    int      `json:"limit,omitempty"`
	Alpha    *float32 `json:"alpha,omitempty"`
}

// BrainSearchResultItem represents a unified search result item returned by POST /api/v1/brain/search.
type BrainSearchResultItem struct {
	Category     string  `json:"category"`
	FilePath     string  `json:"file_path,omitempty"`
	SymbolName   string  `json:"symbol_name,omitempty"`
	SymbolType   string  `json:"symbol_type,omitempty"`
	Title        string  `json:"title,omitempty"`
	Summary      string  `json:"summary"`
	Score        float64 `json:"score"`
	VectorScore  float64 `json:"vector_score,omitempty"`
	LexicalScore float64 `json:"lexical_score,omitempty"`
}

// BrainSyncADRsRequest defines the payload accepted by POST /api/v1/brain/sync/adrs.
type BrainSyncADRsRequest struct {
	Path string `json:"path,omitempty"`
}

// BrainIndexRequest defines the payload accepted by POST /api/v1/brain/index.
type BrainIndexRequest struct {
	Path       string   `json:"path,omitempty"`
	Exclusions []string `json:"exclusions,omitempty"`
	DryRun     bool     `json:"dry_run,omitempty"`
}

func computeDeterministicEmbedding(text string, dim int) []float32 {
	vec := make([]float32, dim)
	h := sha256.New()
	words := strings.Fields(strings.ToLower(text))
	for _, w := range words {
		h.Reset()
		_, _ = h.Write([]byte(w))
		sum := h.Sum(nil)
		for i := 0; i+4 <= len(sum); i += 4 {
			idx := int(binary.BigEndian.Uint32(sum[i:i+4])) % dim
			vec[idx] += 1.0
		}
	}

	var norm float64
	for _, v := range vec {
		norm += float64(v * v)
	}
	if norm > 0 {
		inv := float32(1.0 / math.Sqrt(norm))
		for i := range vec {
			vec[i] *= inv
		}
	} else if dim > 0 {
		vec[0] = 1.0
	}
	return vec
}

func truncateSummary(s string, max int) string {
	s = strings.TrimSpace(strings.ReplaceAll(s, "\n", " "))
	if len(s) > max {
		return s[:max-3] + "..."
	}
	return s
}

func writeBrainError(w http.ResponseWriter, statusCode int, message string) {
	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(statusCode)
	_ = json.NewEncoder(w).Encode(map[string]string{"error": message})
}

// registerBrainEndpoints registers all /api/v1/brain/* routes on the mux.
func registerBrainEndpoints(mux *http.ServeMux, brainSvc *brain.Service) {
	// POST /api/v1/brain/search
	mux.HandleFunc("/api/v1/brain/search", func(w http.ResponseWriter, r *http.Request) {
		if r.Method != http.MethodPost {
			http.Error(w, "Method Not Allowed", http.StatusMethodNotAllowed)
			return
		}

		if brainSvc == nil {
			writeBrainError(w, http.StatusInternalServerError, "brain service is not available")
			return
		}

		var req BrainSearchRequest
		if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
			writeBrainError(w, http.StatusBadRequest, "Invalid JSON payload: "+err.Error())
			return
		}

		query := strings.TrimSpace(req.Query)
		if query == "" {
			writeBrainError(w, http.StatusBadRequest, "query is required")
			return
		}

		cat := strings.ToLower(strings.TrimSpace(req.Category))
		if cat == "" {
			cat = "all"
		}
		if cat != "all" && cat != "knowledge" && cat != "rules" && cat != "episodes" {
			writeBrainError(w, http.StatusBadRequest, "invalid category: must be one of all, knowledge, rules, episodes")
			return
		}

		if req.Limit < 0 {
			writeBrainError(w, http.StatusBadRequest, "limit must be non-negative")
			return
		}

		if req.Alpha != nil && (*req.Alpha < 0.0 || *req.Alpha > 1.0) {
			writeBrainError(w, http.StatusBadRequest, "alpha must be between 0.0 and 1.0")
			return
		}

		limit := req.Limit
		if limit <= 0 {
			limit = 10
		}

		alpha := brain.DefaultHybridAlpha
		if req.Alpha != nil {
			alpha = *req.Alpha
		}

		vec := computeDeterministicEmbedding(query, brain.KnowledgeVectorDimensions)
		hybridQ := brain.NewHybridSearchQuery(query).WithLimit(limit).WithAlpha(alpha).WithEmbedding(vec)

		var results []BrainSearchResultItem

		if cat == "knowledge" || cat == "all" {
			knowResults, err := brainSvc.HybridSearchCodebase(r.Context(), hybridQ)
			if err != nil {
				if cat == "knowledge" {
					writeBrainError(w, http.StatusInternalServerError, err.Error())
					return
				}
			} else {
				for _, k := range knowResults {
					results = append(results, BrainSearchResultItem{
						Category:     "knowledge",
						FilePath:     k.FilePath,
						SymbolName:   k.SymbolName,
						SymbolType:   k.SymbolType,
						Summary:      truncateSummary(k.Content, 80),
						Score:        float64(k.Score),
						VectorScore:  float64(k.VectorScore),
						LexicalScore: float64(k.LexicalScore),
					})
				}
			}
		}

		if cat == "rules" || cat == "all" {
			ruleResults, err := brainSvc.HybridSearchRules(r.Context(), hybridQ)
			if err != nil {
				if cat == "rules" {
					writeBrainError(w, http.StatusInternalServerError, err.Error())
					return
				}
			} else {
				for _, r := range ruleResults {
					results = append(results, BrainSearchResultItem{
						Category:     "rules",
						SymbolName:   r.RuleCode,
						SymbolType:   r.Severity,
						Title:        r.Title,
						Summary:      truncateSummary(r.Title+": "+r.Description, 100),
						Score:        float64(r.Score),
						VectorScore:  float64(r.VectorScore),
						LexicalScore: float64(r.LexicalScore),
					})
				}
			}
		}

		if cat == "episodes" || cat == "all" {
			epResults, err := brainSvc.QuerySimilarEpisodes(r.Context(), vec, limit)
			if err != nil {
				if cat == "episodes" {
					writeBrainError(w, http.StatusInternalServerError, err.Error())
					return
				}
			} else {
				for _, ep := range epResults {
					score := float64(1.0 - ep.Distance)
					if score < 0 {
						score = 0
					}
					if score > 1 {
						score = 1
					}
					results = append(results, BrainSearchResultItem{
						Category:   "episodes",
						SymbolName: ep.RunID,
						SymbolType: ep.Outcome,
						Title:      ep.TaskName,
						Summary:    truncateSummary(ep.Summary, 100),
						Score:      score,
					})
				}
			}
		}

		sort.Slice(results, func(i, j int) bool {
			return results[i].Score > results[j].Score
		})

		if len(results) > limit {
			results = results[:limit]
		}

		if results == nil {
			results = []BrainSearchResultItem{}
		}

		w.Header().Set("Content-Type", "application/json")
		w.WriteHeader(http.StatusOK)
		_ = json.NewEncoder(w).Encode(results)
	})

	// POST /api/v1/brain/sync/adrs
	mux.HandleFunc("/api/v1/brain/sync/adrs", func(w http.ResponseWriter, r *http.Request) {
		if r.Method != http.MethodPost {
			http.Error(w, "Method Not Allowed", http.StatusMethodNotAllowed)
			return
		}

		if brainSvc == nil {
			writeBrainError(w, http.StatusInternalServerError, "brain service is not available")
			return
		}

		var req BrainSyncADRsRequest
		if r.Body != nil && r.ContentLength != 0 {
			if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
				writeBrainError(w, http.StatusBadRequest, "Invalid JSON payload: "+err.Error())
				return
			}
		}

		path := strings.TrimSpace(req.Path)
		if path == "" {
			path = "docs/adr"
		}

		result, err := brainSvc.SyncADRs(r.Context(), path)
		if err != nil {
			writeBrainError(w, http.StatusInternalServerError, err.Error())
			return
		}

		w.Header().Set("Content-Type", "application/json")
		w.WriteHeader(http.StatusOK)
		_ = json.NewEncoder(w).Encode(result)
	})

	// POST /api/v1/brain/index
	mux.HandleFunc("/api/v1/brain/index", func(w http.ResponseWriter, r *http.Request) {
		if r.Method != http.MethodPost {
			http.Error(w, "Method Not Allowed", http.StatusMethodNotAllowed)
			return
		}

		if brainSvc == nil {
			writeBrainError(w, http.StatusInternalServerError, "brain service is not available")
			return
		}

		var req BrainIndexRequest
		if r.Body != nil && r.ContentLength != 0 {
			if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
				writeBrainError(w, http.StatusBadRequest, "Invalid JSON payload: "+err.Error())
				return
			}
		}

		path := strings.TrimSpace(req.Path)
		if path == "" {
			path = "."
		}

		var opts []brain.RepoIndexerOption
		if len(req.Exclusions) > 0 {
			opts = append(opts, brain.WithAdditionalExclusions(req.Exclusions...))
		}
		if req.DryRun {
			opts = append(opts, brain.WithIndexerDryRun(true))
		}

		indexer := brain.NewRepoIndexer(brainSvc, opts...)
		res, err := indexer.Index(r.Context(), path)
		if err != nil {
			writeBrainError(w, http.StatusInternalServerError, err.Error())
			return
		}

		w.Header().Set("Content-Type", "application/json")
		w.WriteHeader(http.StatusOK)
		_ = json.NewEncoder(w).Encode(res)
	})

	// GET /api/v1/brain/stats
	mux.HandleFunc("/api/v1/brain/stats", func(w http.ResponseWriter, r *http.Request) {
		if r.Method != http.MethodGet {
			http.Error(w, "Method Not Allowed", http.StatusMethodNotAllowed)
			return
		}

		if brainSvc == nil {
			writeBrainError(w, http.StatusInternalServerError, "brain service is not available")
			return
		}

		stats, err := brainSvc.GetStats(r.Context())
		if err != nil {
			writeBrainError(w, http.StatusInternalServerError, err.Error())
			return
		}

		w.Header().Set("Content-Type", "application/json")
		w.WriteHeader(http.StatusOK)
		_ = json.NewEncoder(w).Encode(stats)
	})
}
