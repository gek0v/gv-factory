// SPDX-License-Identifier: Apache-2.0
// SPDX-FileCopyrightText: 2026 gv-factory contributors

package main

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"net/http"
	"os"
	"path/filepath"
	"sort"
	"strings"
	"time"

	"github.com/gv-factory/gv-factory/internal/agent"
	"github.com/gv-factory/gv-factory/internal/gateway"
)

// SandboxFileInfo describes a file located within a run's sandbox.
type SandboxFileInfo struct {
	Path     string    `json:"path"`
	Size     int64     `json:"size"`
	Modified bool      `json:"modified"`
	ModTime  time.Time `json:"mod_time,omitempty"`
}

// FileContentResponse carries the relative path and UTF-8 content of a sandbox file.
type FileContentResponse struct {
	Path    string `json:"path"`
	Content string `json:"content"`
	Size    int64  `json:"size"`
}

// UpdateFileContentRequest represents the payload for PUT /api/v1/factory/runs/{id}/files/content.
type UpdateFileContentRequest struct {
	Path    string `json:"path"`
	Content string `json:"content"`
}

// UpdateFileContentResponse is the outcome of a hot-edit operation.
type UpdateFileContentResponse struct {
	Success       bool     `json:"success"`
	Path          string   `json:"path"`
	Diff          string   `json:"diff"`
	FilesModified []string `json:"files_modified,omitempty"`
	Message       string   `json:"message,omitempty"`
}

// ErrPathTraversal is returned when a requested path attempts to escape the sandbox boundary.
var ErrPathTraversal = errors.New("path traversal detected: target path escapes sandbox boundary")

// validateSandboxPath verifies that reqPath is safe, strictly within sandboxDir, and not targeting .git internals.
// It returns the absolute path on disk and canonical forward-slash relative path within sandboxDir.
func validateSandboxPath(sandboxDir, reqPath string) (string, string, error) {
	trimmed := strings.TrimSpace(reqPath)
	if trimmed == "" {
		return "", "", errors.New("file path cannot be empty")
	}

	if strings.Contains(trimmed, "\x00") {
		return "", "", errors.New("null byte detected in path")
	}

	// Reject explicit absolute indicators
	if strings.HasPrefix(trimmed, "/") || strings.HasPrefix(trimmed, "\\") {
		return "", "", ErrPathTraversal
	}
	if filepath.IsAbs(trimmed) {
		return "", "", ErrPathTraversal
	}
	if len(trimmed) >= 2 && trimmed[1] == ':' {
		return "", "", ErrPathTraversal
	}

	// Normalize slashes
	slashPath := filepath.ToSlash(trimmed)
	parts := strings.Split(slashPath, "/")
	for _, part := range parts {
		if part == ".." {
			return "", "", ErrPathTraversal
		}
		if part == ".git" {
			return "", "", errors.New("access to git internal structures is forbidden")
		}
	}

	absSandbox, err := filepath.Abs(sandboxDir)
	if err != nil {
		return "", "", fmt.Errorf("failed to resolve sandbox absolute path: %w", err)
	}

	cleanRel := filepath.Clean(filepath.FromSlash(slashPath))
	if cleanRel == "." || cleanRel == ".." || strings.HasPrefix(cleanRel, ".."+string(filepath.Separator)) {
		return "", "", ErrPathTraversal
	}

	absTarget := filepath.Clean(filepath.Join(absSandbox, cleanRel))

	relCheck, err := filepath.Rel(absSandbox, absTarget)
	if err != nil || relCheck == ".." || strings.HasPrefix(relCheck, ".."+string(filepath.Separator)) || strings.HasPrefix(relCheck, "../") {
		return "", "", ErrPathTraversal
	}

	// If file exists, verify that any symlink does not escape sandbox
	if fi, err := os.Lstat(absTarget); err == nil {
		if fi.Mode()&os.ModeSymlink != 0 {
			resolved, err := filepath.EvalSymlinks(absTarget)
			if err != nil {
				return "", "", fmt.Errorf("failed to evaluate symlink: %w", err)
			}
			symRel, err := filepath.Rel(absSandbox, resolved)
			if err != nil || symRel == ".." || strings.HasPrefix(symRel, ".."+string(filepath.Separator)) || strings.HasPrefix(symRel, "../") {
				return "", "", ErrPathTraversal
			}
		}
	}

	canonicalRel := filepath.ToSlash(relCheck)
	return absTarget, canonicalRel, nil
}

// listSandboxFiles walks sandboxDir and returns file metadata, skipping .git internals.
func listSandboxFiles(ctx context.Context, sandboxDir string, runID string, tracker *FactoryRunTracker) ([]SandboxFileInfo, error) {
	absSandbox, err := filepath.Abs(sandboxDir)
	if err != nil {
		return nil, err
	}

	fi, err := os.Stat(absSandbox)
	if err != nil || !fi.IsDir() {
		return nil, os.ErrNotExist
	}

	modifiedSet := make(map[string]bool)

	// Populate from tracker build outcome if available
	if tracker != nil {
		if rec, ok := tracker.Get(runID); ok && rec != nil && rec.Outcome != nil && rec.Outcome.BuildResult != nil {
			for _, m := range rec.Outcome.BuildResult.FilesModified {
				cleaned := filepath.ToSlash(filepath.Clean(m))
				modifiedSet[cleaned] = true
			}
		}
	}

	// Also inspect git status if .git directory exists
	gitDir := filepath.Join(absSandbox, ".git")
	if gfi, err := os.Stat(gitDir); err == nil && gfi.IsDir() {
		statusOut, _ := runGitCmd(ctx, absSandbox, "status", "--porcelain")
		if strings.TrimSpace(statusOut) != "" {
			for _, line := range strings.Split(strings.TrimSpace(statusOut), "\n") {
				line = strings.TrimSpace(line)
				if len(line) >= 3 {
					fPath := strings.TrimSpace(line[2:])
					if idx := strings.Index(fPath, " -> "); idx != -1 {
						fPath = strings.TrimSpace(fPath[idx+4:])
					}
					fPath = strings.Trim(fPath, "\"")
					modifiedSet[filepath.ToSlash(filepath.Clean(fPath))] = true
				}
			}
		}

		diffNames, _ := runGitCmd(ctx, absSandbox, "diff", "--name-only", "HEAD~1")
		if strings.TrimSpace(diffNames) != "" {
			for _, line := range strings.Split(strings.TrimSpace(diffNames), "\n") {
				line = strings.TrimSpace(line)
				if line != "" {
					modifiedSet[filepath.ToSlash(filepath.Clean(line))] = true
				}
			}
		}
	}

	var files []SandboxFileInfo
	walkErr := filepath.Walk(absSandbox, func(path string, info os.FileInfo, err error) error {
		if err != nil {
			return err
		}
		if info.IsDir() {
			if info.Name() == ".git" {
				return filepath.SkipDir
			}
			return nil
		}

		rel, err := filepath.Rel(absSandbox, path)
		if err != nil {
			return nil
		}
		slashRel := filepath.ToSlash(rel)
		if slashRel == ".git" || strings.HasPrefix(slashRel, ".git/") {
			return nil
		}

		files = append(files, SandboxFileInfo{
			Path:     slashRel,
			Size:     info.Size(),
			Modified: modifiedSet[slashRel],
			ModTime:  info.ModTime().UTC(),
		})
		return nil
	})

	if walkErr != nil {
		return nil, walkErr
	}

	if files == nil {
		files = make([]SandboxFileInfo, 0)
	}

	sort.Slice(files, func(i, j int) bool {
		return files[i].Path < files[j].Path
	})

	return files, nil
}

// handleFactoryFiles dispatches /api/v1/factory/runs/{id}/files and /files/content
func handleFactoryFiles(
	w http.ResponseWriter,
	r *http.Request,
	runID string,
	subpath string,
	sandboxDir string,
	tracker *FactoryRunTracker,
	opts FactoryEndpointsOptions,
	gw *gateway.Gateway,
) {
	switch subpath {
	case "":
		// GET /api/v1/factory/runs/{id}/files
		if r.Method != http.MethodGet {
			http.Error(w, "Method Not Allowed", http.StatusMethodNotAllowed)
			return
		}

		files, err := listSandboxFiles(r.Context(), sandboxDir, runID, tracker)
		if err != nil {
			if errors.Is(err, os.ErrNotExist) {
				w.Header().Set("Content-Type", "application/json")
				w.WriteHeader(http.StatusNotFound)
				_ = json.NewEncoder(w).Encode(map[string]any{
					"error": fmt.Sprintf("sandbox directory for run %q not found", runID),
				})
				return
			}
			w.Header().Set("Content-Type", "application/json")
			w.WriteHeader(http.StatusInternalServerError)
			_ = json.NewEncoder(w).Encode(map[string]any{
				"error": fmt.Sprintf("failed to list files: %v", err),
			})
			return
		}

		w.Header().Set("Content-Type", "application/json")
		w.WriteHeader(http.StatusOK)
		_ = json.NewEncoder(w).Encode(files)

	case "content":
		// /api/v1/factory/runs/{id}/files/content
		switch r.Method {
		case http.MethodGet:
			relPath := r.URL.Query().Get("path")
			if strings.TrimSpace(relPath) == "" {
				w.Header().Set("Content-Type", "application/json")
				w.WriteHeader(http.StatusBadRequest)
				_ = json.NewEncoder(w).Encode(map[string]any{
					"error": "path query parameter is required",
				})
				return
			}

			absTarget, canonicalRel, err := validateSandboxPath(sandboxDir, relPath)
			if err != nil {
				w.Header().Set("Content-Type", "application/json")
				w.WriteHeader(http.StatusBadRequest)
				_ = json.NewEncoder(w).Encode(map[string]any{
					"error": err.Error(),
				})
				return
			}

			fi, err := os.Stat(absTarget)
			if err != nil {
				if os.IsNotExist(err) {
					w.Header().Set("Content-Type", "application/json")
					w.WriteHeader(http.StatusNotFound)
					_ = json.NewEncoder(w).Encode(map[string]any{
						"error": fmt.Sprintf("file %q not found", relPath),
					})
					return
				}
				w.Header().Set("Content-Type", "application/json")
				w.WriteHeader(http.StatusInternalServerError)
				_ = json.NewEncoder(w).Encode(map[string]any{
					"error": err.Error(),
				})
				return
			}

			if fi.IsDir() {
				w.Header().Set("Content-Type", "application/json")
				w.WriteHeader(http.StatusBadRequest)
				_ = json.NewEncoder(w).Encode(map[string]any{
					"error": fmt.Sprintf("path %q is a directory", relPath),
				})
				return
			}

			data, err := os.ReadFile(absTarget)
			if err != nil {
				w.Header().Set("Content-Type", "application/json")
				w.WriteHeader(http.StatusInternalServerError)
				_ = json.NewEncoder(w).Encode(map[string]any{
					"error": "failed to read file: " + err.Error(),
				})
				return
			}

			w.Header().Set("Content-Type", "application/json")
			w.WriteHeader(http.StatusOK)
			_ = json.NewEncoder(w).Encode(FileContentResponse{
				Path:    canonicalRel,
				Content: string(data),
				Size:    fi.Size(),
			})

		case http.MethodPut:
			var req UpdateFileContentRequest
			if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
				w.Header().Set("Content-Type", "application/json")
				w.WriteHeader(http.StatusBadRequest)
				_ = json.NewEncoder(w).Encode(map[string]any{
					"error": "invalid JSON payload: " + err.Error(),
				})
				return
			}

			if strings.TrimSpace(req.Path) == "" {
				w.Header().Set("Content-Type", "application/json")
				w.WriteHeader(http.StatusBadRequest)
				_ = json.NewEncoder(w).Encode(map[string]any{
					"error": "path field is required",
				})
				return
			}

			// Ensure sandbox directory exists
			if sfi, err := os.Stat(sandboxDir); err != nil || !sfi.IsDir() {
				if tracker != nil {
					if _, found := tracker.Get(runID); found {
						_ = initGitSandbox(r.Context(), sandboxDir, runID, "main")
					}
				}
				if sfi, err := os.Stat(sandboxDir); err != nil || !sfi.IsDir() {
					w.Header().Set("Content-Type", "application/json")
					w.WriteHeader(http.StatusNotFound)
					_ = json.NewEncoder(w).Encode(map[string]any{
						"error": fmt.Sprintf("sandbox directory for run %q not found", runID),
					})
					return
				}
			}

			absTarget, canonicalRel, err := validateSandboxPath(sandboxDir, req.Path)
			if err != nil {
				w.Header().Set("Content-Type", "application/json")
				w.WriteHeader(http.StatusBadRequest)
				_ = json.NewEncoder(w).Encode(map[string]any{
					"error": err.Error(),
				})
				return
			}

			// Ensure parent directory exists
			parentDir := filepath.Dir(absTarget)
			if err := os.MkdirAll(parentDir, 0755); err != nil {
				w.Header().Set("Content-Type", "application/json")
				w.WriteHeader(http.StatusInternalServerError)
				_ = json.NewEncoder(w).Encode(map[string]any{
					"error": fmt.Sprintf("failed to create directory: %v", err),
				})
				return
			}

			// Write updated content
			if err := os.WriteFile(absTarget, []byte(req.Content), 0644); err != nil {
				w.Header().Set("Content-Type", "application/json")
				w.WriteHeader(http.StatusInternalServerError)
				_ = json.NewEncoder(w).Encode(map[string]any{
					"error": fmt.Sprintf("failed to write file: %v", err),
				})
				return
			}

			// Execute git operations
			ctx := r.Context()
			newDiff := ""
			gitDir := filepath.Join(sandboxDir, ".git")
			if gfi, err := os.Stat(gitDir); err == nil && gfi.IsDir() {
				// Git configuration safeguard
				_, _ = runGitCmd(ctx, sandboxDir, "config", "user.name", "GV Factory Bot")
				_, _ = runGitCmd(ctx, sandboxDir, "config", "user.email", "bot@gv-factory.local")

				// Stage changes
				_, _ = runGitCmd(ctx, sandboxDir, "add", "-A")

				// Commit: try amend first, fallback to new commit
				_, amendErr := runGitCmd(ctx, sandboxDir, "commit", "--amend", "--no-edit")
				if amendErr != nil {
					commitMsg := fmt.Sprintf("chore(sandbox): hot edit %s", canonicalRel)
					_, _ = runGitCmd(ctx, sandboxDir, "commit", "-m", commitMsg)
				}

				baseBranch := "main"

				newDiff = extractGitDiff(ctx, sandboxDir, baseBranch)
			}

			// Update tracker and lastBuildResult.GitDiff
			var modifiedFilesList []string
			if tracker != nil {
				if rec, ok := tracker.Get(runID); ok && rec != nil {
					if rec.Outcome == nil {
						rec.Outcome = &agent.PipelineOutcome{
							TaskID:  runID,
							Success: true,
						}
					}
					if rec.Outcome.BuildResult == nil {
						rec.Outcome.BuildResult = &agent.BuildResult{
							TaskID:  runID,
							Success: true,
						}
					}

					rec.Outcome.BuildResult.GitDiff = newDiff

					alreadyPresent := false
					for _, mf := range rec.Outcome.BuildResult.FilesModified {
						if mf == canonicalRel {
							alreadyPresent = true
							break
						}
					}
					if !alreadyPresent {
						rec.Outcome.BuildResult.FilesModified = append(rec.Outcome.BuildResult.FilesModified, canonicalRel)
					}
					modifiedFilesList = rec.Outcome.BuildResult.FilesModified

					rec.UpdatedAt = time.Now().UTC()
					tracker.Set(rec)
				}
			}

			// Update pending gate tracker diff if gate_waiting
			if opts.PendingTracker != nil {
				opts.PendingTracker.UpdateGateDiff(runID, newDiff)
			}

			// Broadcast event
			broadcastStageEvent(gw, runID, "file_hot_edited", map[string]any{
				"path":      canonicalRel,
				"diff":      newDiff,
				"timestamp": time.Now().UTC().Format(time.RFC3339),
			})

			w.Header().Set("Content-Type", "application/json")
			w.WriteHeader(http.StatusOK)
			_ = json.NewEncoder(w).Encode(UpdateFileContentResponse{
				Success:       true,
				Path:          canonicalRel,
				Diff:          newDiff,
				FilesModified: modifiedFilesList,
				Message:       "File updated and diff recalculated successfully",
			})

		default:
			http.Error(w, "Method Not Allowed", http.StatusMethodNotAllowed)
		}

	default:
		http.NotFound(w, r)
	}
}
