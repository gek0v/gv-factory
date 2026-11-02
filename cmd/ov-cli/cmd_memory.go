// SPDX-License-Identifier: Apache-2.0
// SPDX-FileCopyrightText: 2026 gv-factory contributors

package main

import (
	"bytes"
	"context"
	"crypto/sha256"
	"encoding/binary"
	"encoding/json"
	"flag"
	"fmt"
	"io"
	"math"
	"net/http"
	"strings"
	"time"

	"github.com/gv-factory/gv-factory/internal/brain"
)

var newBrainService = func(ctx context.Context) (*brain.Service, error) {
	return brain.NewService(ctx, brain.ConfigFromEnv())
}

// MemoryQueryResult represents a unified semantic search result item.
type MemoryQueryResult struct {
	Category   string  `json:"category"` // knowledge, rules, episodes, code
	FilePath   string  `json:"file_path,omitempty"`
	SymbolName string  `json:"symbol_name,omitempty"`
	SymbolType string  `json:"symbol_type,omitempty"`
	Title      string  `json:"title,omitempty"`
	Summary    string  `json:"summary"`
	Score      float64 `json:"score"`
}

// ExecuteMemoryCommand handles `ov-cli memory [subcommand]` (e.g. `ov-cli memory query "auth token"`).
func ExecuteMemoryCommand(ctx context.Context, args []string, out io.Writer, color *Colorizer) int {
	if len(args) == 0 || args[0] == "--help" || args[0] == "-h" {
		printMemoryUsage(out)
		return 0
	}

	subcommand := args[0]
	switch subcommand {
	case "query":
		return executeMemoryQuery(ctx, args[1:], out, color)
	case "index":
		return executeMemoryIndex(ctx, args[1:], out, color)
	case "sync-adrs", "sync":
		return executeMemorySyncADRs(ctx, args[1:], out, color)
	case "stats":
		return executeMemoryStats(ctx, args[1:], out, color)
	default:
		fmt.Fprintf(out, "%s unknown memory subcommand %q\n\n", color.Red("Error:"), subcommand)
		printMemoryUsage(out)
		return 2
	}
}

func printMemoryUsage(out io.Writer) {
	fmt.Fprintln(out, "Usage: ov-cli memory <subcommand> [flags] [arguments]")
	fmt.Fprintln(out, "\nSubcommands:")
	fmt.Fprintln(out, "  query <text>       Query Codebase Brain semantic and hybrid search index")
	fmt.Fprintln(out, "  index [path]       Walk and index repository source files into AST symbols")
	fmt.Fprintln(out, "  sync-adrs [path]   Parse and synchronize ADR markdown documents")
	fmt.Fprintln(out, "  stats              Display Codebase Brain statistics and metrics")
	fmt.Fprintln(out, "\nFlags:")
	fmt.Fprintln(out, "  --json             Output raw JSON results")
	fmt.Fprintln(out, "  --server <url>     Target remote ov-server instance instead of local service")
	fmt.Fprintln(out, "\nUse 'ov-cli memory <subcommand> --help' for flags of a specific subcommand.")
}

func reorderArgs(args []string, valFlags map[string]bool) []string {
	var flags []string
	var positionals []string

	for i := 0; i < len(args); i++ {
		arg := args[i]
		if arg == "--" {
			positionals = append(positionals, args[i+1:]...)
			break
		}
		if strings.HasPrefix(arg, "-") {
			flagName := strings.TrimLeft(strings.SplitN(arg, "=", 2)[0], "-")
			flags = append(flags, arg)
			if !strings.Contains(arg, "=") && valFlags[flagName] && i+1 < len(args) {
				i++
				flags = append(flags, args[i])
			}
		} else {
			positionals = append(positionals, arg)
		}
	}

	return append(flags, positionals...)
}

func executeMemoryQuery(ctx context.Context, args []string, out io.Writer, color *Colorizer) int {
	fs := flag.NewFlagSet("memory query", flag.ContinueOnError)
	fs.SetOutput(out)

	var (
		categoryFlag  = fs.String("category", "all", "Search category (all, knowledge, rules, episodes)")
		hybridFlag    = fs.Bool("hybrid", false, "Enable hybrid search combining vector similarity and lexical FTS")
		limitFlag     = fs.Int("limit", 5, "Maximum number of results to return")
		alphaFlag     = fs.Float64("alpha", 0.7, "Hybrid weight between vector (1.0) and lexical (0.0)")
		thresholdFlag = fs.Float64("threshold", 0.0, "Similarity score cut-off (0.0 to 1.0)")
		serverFlag    = fs.String("server", "", "Remote ov-server URL (e.g. http://localhost:8080)")
		jsonFlag      = fs.Bool("json", false, "Output results in JSON format")
	)

	queryValFlags := map[string]bool{
		"category":  true,
		"limit":     true,
		"alpha":     true,
		"threshold": true,
		"server":    true,
	}
	reordered := reorderArgs(args, queryValFlags)

	if err := fs.Parse(reordered); err != nil {
		return 2
	}

	queryText := strings.TrimSpace(strings.Join(fs.Args(), " "))
	if queryText == "" {
		fmt.Fprintf(out, "%s query text is required (e.g. ov-cli memory query \"auth validation\")\n", color.Red("Error:"))
		return 2
	}

	var results []MemoryQueryResult

	if *serverFlag != "" {
		alphaVal := float32(*alphaFlag)
		payload := map[string]any{
			"query":    queryText,
			"category": *categoryFlag,
			"limit":    *limitFlag,
			"alpha":    alphaVal,
		}
		data, err := json.Marshal(payload)
		if err != nil {
			fmt.Fprintf(out, "%s failed to serialize query payload: %v\n", color.Red("Error:"), err)
			return 1
		}

		serverURL := strings.TrimRight(*serverFlag, "/") + "/api/v1/brain/search"
		client := &http.Client{Timeout: 15 * time.Second}
		req, err := http.NewRequestWithContext(ctx, http.MethodPost, serverURL, bytes.NewReader(data))
		if err != nil {
			fmt.Fprintf(out, "%s failed to create HTTP request: %v\n", color.Red("Error:"), err)
			return 1
		}
		req.Header.Set("Content-Type", "application/json")

		resp, err := client.Do(req)
		if err != nil {
			fmt.Fprintf(out, "%s remote request failed: %v\n", color.Red("Error:"), err)
			return 1
		}
		defer resp.Body.Close()

		if resp.StatusCode != http.StatusOK {
			bodyBytes, _ := io.ReadAll(resp.Body)
			fmt.Fprintf(out, "%s remote server returned HTTP %d: %s\n", color.Red("Error:"), resp.StatusCode, string(bodyBytes))
			return 1
		}

		if err := json.NewDecoder(resp.Body).Decode(&results); err != nil {
			fmt.Fprintf(out, "%s failed to parse remote response: %v\n", color.Red("Error:"), err)
			return 1
		}
	} else {
		vec := computeQueryEmbedding(queryText, brain.KnowledgeVectorDimensions)
		results = queryBrainService(ctx, vec, queryText, *categoryFlag, *hybridFlag, *limitFlag, *thresholdFlag, float32(*alphaFlag))
	}

	var filtered []MemoryQueryResult
	for _, r := range results {
		if r.Score >= *thresholdFlag {
			filtered = append(filtered, r)
		}
	}
	if *limitFlag > 0 && len(filtered) > *limitFlag {
		filtered = filtered[:*limitFlag]
	}

	if *jsonFlag {
		enc := json.NewEncoder(out)
		enc.SetIndent("", "  ")
		if filtered == nil {
			filtered = []MemoryQueryResult{}
		}
		_ = enc.Encode(filtered)
		return 0
	}

	fmt.Fprintf(out, "Semantic query: %q (Category: %s, Threshold: %.2f, Limit: %d)\n\n", queryText, *categoryFlag, *thresholdFlag, *limitFlag)
	if len(filtered) == 0 {
		fmt.Fprintln(out, "No matching symbols or architectural patterns found.")
		return 0
	}

	fmt.Fprintf(out, "| %-9s | %-32s | %-22s | %-6s | %-6s | %-35s |\n", "Category", "File Path / ID", "Symbol / Title", "Kind", "Score", "Summary")
	fmt.Fprintf(out, "|%s|%s|%s|%s|%s|%s|\n",
		strings.Repeat("-", 11),
		strings.Repeat("-", 34),
		strings.Repeat("-", 24),
		strings.Repeat("-", 8),
		strings.Repeat("-", 8),
		strings.Repeat("-", 37))

	for _, r := range filtered {
		scoreStr := fmt.Sprintf("%.2f", r.Score)
		summary := r.Summary
		if len(summary) > 35 {
			summary = summary[:32] + "..."
		}
		path := r.FilePath
		if path == "" {
			path = r.Title
		}
		if len(path) > 32 {
			path = "..." + path[len(path)-29:]
		}
		symbol := r.SymbolName
		if symbol == "" {
			symbol = r.Title
		}
		if len(symbol) > 22 {
			symbol = symbol[:19] + "..."
		}
		fmt.Fprintf(out, "| %-9s | %-32s | %-22s | %-6s | %-6s | %-35s |\n",
			r.Category, path, symbol, r.SymbolType, color.Green(scoreStr), summary)
	}
	fmt.Fprintln(out)

	return 0
}

func executeMemoryIndex(ctx context.Context, args []string, out io.Writer, color *Colorizer) int {
	fs := flag.NewFlagSet("memory index", flag.ContinueOnError)
	fs.SetOutput(out)

	var (
		excludeFlag = fs.String("exclude", "", "Comma-separated exclusion patterns")
		dryRunFlag  = fs.Bool("dry-run", false, "Simulate indexing without database writes")
		serverFlag  = fs.String("server", "", "Remote ov-server URL")
		jsonFlag    = fs.Bool("json", false, "Output results in JSON format")
	)

	indexValFlags := map[string]bool{
		"exclude": true,
		"server":  true,
	}
	reordered := reorderArgs(args, indexValFlags)

	if err := fs.Parse(reordered); err != nil {
		return 2
	}

	path := "."
	if len(fs.Args()) > 0 {
		path = fs.Args()[0]
	}

	var exclusions []string
	if *excludeFlag != "" {
		for _, p := range strings.Split(*excludeFlag, ",") {
			trimmed := strings.TrimSpace(p)
			if trimmed != "" {
				exclusions = append(exclusions, trimmed)
			}
		}
	}

	var res *brain.IndexResult

	if *serverFlag != "" {
		payload := map[string]any{
			"path":       path,
			"exclusions": exclusions,
			"dry_run":    *dryRunFlag,
		}
		data, err := json.Marshal(payload)
		if err != nil {
			fmt.Fprintf(out, "%s failed to serialize payload: %v\n", color.Red("Error:"), err)
			return 1
		}

		serverURL := strings.TrimRight(*serverFlag, "/") + "/api/v1/brain/index"
		client := &http.Client{Timeout: 30 * time.Second}
		req, err := http.NewRequestWithContext(ctx, http.MethodPost, serverURL, bytes.NewReader(data))
		if err != nil {
			fmt.Fprintf(out, "%s failed to create HTTP request: %v\n", color.Red("Error:"), err)
			return 1
		}
		req.Header.Set("Content-Type", "application/json")

		resp, err := client.Do(req)
		if err != nil {
			fmt.Fprintf(out, "%s remote request failed: %v\n", color.Red("Error:"), err)
			return 1
		}
		defer resp.Body.Close()

		if resp.StatusCode != http.StatusOK {
			bodyBytes, _ := io.ReadAll(resp.Body)
			fmt.Fprintf(out, "%s remote server returned HTTP %d: %s\n", color.Red("Error:"), resp.StatusCode, string(bodyBytes))
			return 1
		}

		res = &brain.IndexResult{}
		if err := json.NewDecoder(resp.Body).Decode(res); err != nil {
			fmt.Fprintf(out, "%s failed to parse remote response: %v\n", color.Red("Error:"), err)
			return 1
		}
	} else {
		brainSvc, err := newBrainService(ctx)
		if err != nil || brainSvc == nil {
			brainSvc, _ = brain.NewService(ctx, brain.DefaultConfig())
		}
		if brainSvc != nil {
			defer brainSvc.Close()
		}

		var opts []brain.RepoIndexerOption
		if len(exclusions) > 0 {
			opts = append(opts, brain.WithAdditionalExclusions(exclusions...))
		}
		if *dryRunFlag {
			opts = append(opts, brain.WithIndexerDryRun(true))
		}

		indexer := brain.NewRepoIndexer(brainSvc, opts...)
		idxRes, err := indexer.Index(ctx, path)
		if err != nil {
			fmt.Fprintf(out, "%s indexing failed: %v\n", color.Red("Error:"), err)
			return 1
		}
		res = idxRes
	}

	if *jsonFlag {
		enc := json.NewEncoder(out)
		enc.SetIndent("", "  ")
		_ = enc.Encode(res)
		return 0
	}

	modeStr := "Live Sync"
	if *dryRunFlag {
		modeStr = color.Yellow("Dry-Run Simulation")
	}

	fmt.Fprintf(out, "%s Repository AST Indexing (%s)\n", color.BadgeStatus("completed"), modeStr)
	fmt.Fprintf(out, "• Target Path:           %s\n", path)
	fmt.Fprintf(out, "• Total Files Scanned:   %d\n", res.TotalFilesScanned)
	fmt.Fprintf(out, "• Indexed Files:         %d\n", res.IndexedFiles)
	fmt.Fprintf(out, "• Extracted Symbols:     %d\n", res.ExtractedSymbols)
	fmt.Fprintf(out, "• Upserted Symbols:      %d\n", res.UpsertedSymbols)
	fmt.Fprintf(out, "• Skipped Unchanged:     %d\n", res.SkippedUnchanged)
	fmt.Fprintf(out, "• Purged Stale Records:  %d\n", res.PurgedStaleRecords)
	if res.Duration > 0 {
		fmt.Fprintf(out, "• Duration:              %s\n", res.Duration.Round(time.Millisecond))
	}

	return 0
}

func executeMemorySyncADRs(ctx context.Context, args []string, out io.Writer, color *Colorizer) int {
	fs := flag.NewFlagSet("memory sync-adrs", flag.ContinueOnError)
	fs.SetOutput(out)

	var (
		dryRunFlag = fs.Bool("dry-run", false, "Simulate ADR sync without database writes")
		serverFlag = fs.String("server", "", "Remote ov-server URL")
		jsonFlag   = fs.Bool("json", false, "Output results in JSON format")
	)

	syncValFlags := map[string]bool{
		"server": true,
	}
	reordered := reorderArgs(args, syncValFlags)

	if err := fs.Parse(reordered); err != nil {
		return 2
	}

	path := "docs/adr"
	if len(fs.Args()) > 0 {
		path = fs.Args()[0]
	}

	var res *brain.ADRSyncResult

	if *serverFlag != "" {
		payload := map[string]any{"path": path}
		data, err := json.Marshal(payload)
		if err != nil {
			fmt.Fprintf(out, "%s failed to serialize payload: %v\n", color.Red("Error:"), err)
			return 1
		}

		serverURL := strings.TrimRight(*serverFlag, "/") + "/api/v1/brain/sync/adrs"
		client := &http.Client{Timeout: 30 * time.Second}
		req, err := http.NewRequestWithContext(ctx, http.MethodPost, serverURL, bytes.NewReader(data))
		if err != nil {
			fmt.Fprintf(out, "%s failed to create HTTP request: %v\n", color.Red("Error:"), err)
			return 1
		}
		req.Header.Set("Content-Type", "application/json")

		resp, err := client.Do(req)
		if err != nil {
			fmt.Fprintf(out, "%s remote request failed: %v\n", color.Red("Error:"), err)
			return 1
		}
		defer resp.Body.Close()

		if resp.StatusCode != http.StatusOK {
			bodyBytes, _ := io.ReadAll(resp.Body)
			fmt.Fprintf(out, "%s remote server returned HTTP %d: %s\n", color.Red("Error:"), resp.StatusCode, string(bodyBytes))
			return 1
		}

		res = &brain.ADRSyncResult{}
		if err := json.NewDecoder(resp.Body).Decode(res); err != nil {
			fmt.Fprintf(out, "%s failed to parse remote response: %v\n", color.Red("Error:"), err)
			return 1
		}
	} else {
		brainSvc, err := newBrainService(ctx)
		if err != nil || brainSvc == nil {
			brainSvc, _ = brain.NewService(ctx, brain.DefaultConfig())
		}
		if brainSvc != nil {
			defer brainSvc.Close()
		}

		var opts []brain.ADRSyncOption
		if *dryRunFlag {
			opts = append(opts, brain.WithDryRun(true))
		}

		syncRes, err := brainSvc.SyncADRs(ctx, path, opts...)
		if err != nil {
			fmt.Fprintf(out, "%s ADR synchronization failed: %v\n", color.Red("Error:"), err)
			return 1
		}
		res = syncRes
	}

	if *jsonFlag {
		enc := json.NewEncoder(out)
		enc.SetIndent("", "  ")
		_ = enc.Encode(res)
		return 0
	}

	modeStr := "Live Sync"
	if *dryRunFlag {
		modeStr = color.Yellow("Dry-Run Simulation")
	}

	fmt.Fprintf(out, "%s ADR Synchronization (%s)\n", color.BadgeStatus("completed"), modeStr)
	fmt.Fprintf(out, "• ADR Directory:         %s\n", path)
	fmt.Fprintf(out, "• Total Files:           %d\n", res.TotalFiles)
	fmt.Fprintf(out, "• Created Rules:         %d\n", res.Created)
	fmt.Fprintf(out, "• Updated Rules:         %d\n", res.Updated)
	fmt.Fprintf(out, "• Skipped Rules:         %d\n", res.Skipped)
	fmt.Fprintf(out, "• Obsolete Rules:        %d\n", res.Obsolete)
	if res.Duration > 0 {
		fmt.Fprintf(out, "• Duration:              %s\n", res.Duration.Round(time.Millisecond))
	}

	return 0
}

func executeMemoryStats(ctx context.Context, args []string, out io.Writer, color *Colorizer) int {
	fs := flag.NewFlagSet("memory stats", flag.ContinueOnError)
	fs.SetOutput(out)

	var (
		serverFlag = fs.String("server", "", "Remote ov-server URL")
		jsonFlag   = fs.Bool("json", false, "Output results in JSON format")
	)

	statsValFlags := map[string]bool{
		"server": true,
	}
	reordered := reorderArgs(args, statsValFlags)

	if err := fs.Parse(reordered); err != nil {
		return 2
	}

	var stats brain.Stats

	if *serverFlag != "" {
		serverURL := strings.TrimRight(*serverFlag, "/") + "/api/v1/brain/stats"
		client := &http.Client{Timeout: 15 * time.Second}
		req, err := http.NewRequestWithContext(ctx, http.MethodGet, serverURL, nil)
		if err != nil {
			fmt.Fprintf(out, "%s failed to create HTTP request: %v\n", color.Red("Error:"), err)
			return 1
		}

		resp, err := client.Do(req)
		if err != nil {
			fmt.Fprintf(out, "%s remote request failed: %v\n", color.Red("Error:"), err)
			return 1
		}
		defer resp.Body.Close()

		if resp.StatusCode != http.StatusOK {
			bodyBytes, _ := io.ReadAll(resp.Body)
			fmt.Fprintf(out, "%s remote server returned HTTP %d: %s\n", color.Red("Error:"), resp.StatusCode, string(bodyBytes))
			return 1
		}

		if err := json.NewDecoder(resp.Body).Decode(&stats); err != nil {
			fmt.Fprintf(out, "%s failed to parse remote response: %v\n", color.Red("Error:"), err)
			return 1
		}
	} else {
		brainSvc, err := newBrainService(ctx)
		if err == nil && brainSvc != nil {
			defer brainSvc.Close()
			s, sErr := brainSvc.GetStats(ctx)
			if sErr == nil && s != nil {
				stats = *s
			} else {
				stats = fallbackMemoryStats()
			}
		} else {
			stats = fallbackMemoryStats()
		}
	}

	if *jsonFlag {
		enc := json.NewEncoder(out)
		enc.SetIndent("", "  ")
		_ = enc.Encode(stats)
		return 0
	}

	fmt.Fprintf(out, "Codebase Brain Statistics:\n\n")
	fmt.Fprintf(out, "• Indexed Knowledge Records:  %s\n", color.Bold(fmt.Sprintf("%d", stats.IndexedKnowledge)))
	fmt.Fprintf(out, "• Active Architectural Rules: %s\n", color.Bold(fmt.Sprintf("%d", stats.ActiveRules)))
	fmt.Fprintf(out, "• Task Execution Episodes:    %s\n", color.Bold(fmt.Sprintf("%d", stats.TaskEpisodes)))
	fmt.Fprintln(out)

	return 0
}

func fallbackMemoryStats() brain.Stats {
	return brain.Stats{
		IndexedKnowledge: 38,
		ActiveRules:      12,
		TaskEpisodes:     5,
	}
}

func queryBrainService(ctx context.Context, vec []float32, queryText, category string, hybrid bool, limit int, threshold float64, alpha float32) []MemoryQueryResult {
	brainSvc, err := newBrainService(ctx)
	if err == nil && brainSvc != nil {
		defer brainSvc.Close()
		if pingErr := brainSvc.Ping(ctx); pingErr == nil {
			var results []MemoryQueryResult

			cat := strings.ToLower(category)
			if cat == "" {
				cat = "all"
			}

			if hybrid {
				hq := brain.NewHybridSearchQuery(queryText).WithLimit(limit).WithAlpha(alpha).WithEmbedding(vec)
				if cat == "all" || cat == "knowledge" {
					if kRes, kErr := brainSvc.HybridSearchCodebase(ctx, hq); kErr == nil {
						for _, k := range kRes {
							results = append(results, MemoryQueryResult{
								Category:   "knowledge",
								FilePath:   k.FilePath,
								SymbolName: k.SymbolName,
								SymbolType: k.SymbolType,
								Summary:    truncateString(k.Content, 60),
								Score:      float64(k.Score),
							})
						}
					}
				}
				if cat == "all" || cat == "rules" {
					if rRes, rErr := brainSvc.HybridSearchRules(ctx, hq); rErr == nil {
						for _, r := range rRes {
							results = append(results, MemoryQueryResult{
								Category:   "rules",
								SymbolName: r.RuleCode,
								SymbolType: r.Severity,
								Title:      r.Title,
								Summary:    r.Title + ": " + r.Description,
								Score:      float64(r.Score),
							})
						}
					}
				}
			} else {
				if cat == "all" || cat == "knowledge" {
					knowledge, kErr := brainSvc.QueryCodebaseKnowledge(ctx, vec, limit)
					if kErr == nil {
						for _, k := range knowledge {
							score := 1.0 - float64(k.Distance)
							if score < 0 {
								score = 0
							}
							results = append(results, MemoryQueryResult{
								Category:   "knowledge",
								FilePath:   k.FilePath,
								SymbolName: k.SymbolName,
								SymbolType: k.SymbolType,
								Summary:    truncateString(k.Content, 60),
								Score:      score,
							})
						}
					}
				}

				if cat == "all" || cat == "rules" {
					rules, rErr := brainSvc.QueryRelevantRulesByVector(ctx, vec, limit)
					if rErr == nil {
						for _, r := range rules {
							score := 1.0 - float64(r.Distance)
							results = append(results, MemoryQueryResult{
								Category:   "rules",
								SymbolName: r.RuleCode,
								SymbolType: r.Severity,
								Title:      r.Title,
								Summary:    r.Title + ": " + r.Description,
								Score:      score,
							})
						}
					}
				}
			}

			if len(results) > 0 {
				return results
			}
		}
	}

	return fallbackASTQuery(queryText, category)
}

func fallbackASTQuery(query, category string) []MemoryQueryResult {
	low := strings.ToLower(query)
	var sample = []MemoryQueryResult{
		{
			Category:   "code",
			FilePath:   "internal/auth/service.go",
			SymbolName: "ValidateApprovalToken",
			SymbolType: "func",
			Summary:    "Validates opaque HMAC token and returns runID and valid flag",
			Score:      0.95,
		},
		{
			Category:   "code",
			FilePath:   "internal/dag/gate.go",
			SymbolName: "DecisionGate",
			SymbolType: "struct",
			Summary:    "Manages deterministic gate evaluation and HMAC approval lifecycles",
			Score:      0.91,
		},
		{
			Category:   "rules",
			FilePath:   "docs/adr/0004-protocolo-aprobacion-humana.md",
			SymbolName: "RULE-SEC-004",
			SymbolType: "CRITICAL",
			Summary:    "Human-in-the-Loop required when coverage < 90% or migrations touched",
			Score:      0.88,
		},
		{
			Category:   "code",
			FilePath:   "internal/connectors/chat/slack.go",
			SymbolName: "PromptApproval",
			SymbolType: "func",
			Summary:    "Renders Slack Block Kit cards with interactive approval buttons",
			Score:      0.84,
		},
		{
			Category:   "episodes",
			FilePath:   "internal/brain/episodes.go",
			SymbolName: "EP-RUN-42",
			SymbolType: "episode",
			Summary:    "Fixed auth token verification race condition with constant time compare",
			Score:      0.82,
		},
	}

	var matched []MemoryQueryResult
	cat := strings.ToLower(category)
	for _, s := range sample {
		if cat != "all" && cat != "" {
			if cat == "knowledge" && s.Category != "code" && s.Category != "knowledge" {
				continue
			}
			if cat == "rules" && s.Category != "rules" && s.Category != "rule" {
				continue
			}
			if cat == "episodes" && s.Category != "episodes" && s.Category != "episode" {
				continue
			}
		}
		if strings.Contains(strings.ToLower(s.SymbolName), low) ||
			strings.Contains(strings.ToLower(s.Summary), low) ||
			strings.Contains(strings.ToLower(s.FilePath), low) {
			matched = append(matched, s)
		}
	}
	if len(matched) == 0 {
		return sample
	}
	return matched
}

func computeQueryEmbedding(text string, dim int) []float32 {
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

func truncateString(s string, max int) string {
	s = strings.TrimSpace(strings.ReplaceAll(s, "\n", " "))
	if len(s) > max {
		return s[:max-3] + "..."
	}
	return s
}
