// SPDX-License-Identifier: Apache-2.0
// SPDX-FileCopyrightText: 2026 gv-factory contributors

package inbound

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net/http"
	"net/url"
	"strings"
	"time"

	"github.com/gv-factory/gv-factory/pkg/schema"
)

// PullRequestResponse represents the essential details of a newly created Pull Request.
type PullRequestResponse struct {
	ID      int64  `json:"id"`
	Number  int    `json:"number"`
	HTMLURL string `json:"html_url"`
	URL     string `json:"url"`
	State   string `json:"state"`
	Title   string `json:"title"`
	Body    string `json:"body"`
}

// MergeRequestResponse represents the details of a newly created GitLab Merge Request.
type MergeRequestResponse struct {
	ID          int64  `json:"id"`
	IID         int    `json:"iid"`
	ProjectID   int64  `json:"project_id"`
	Title       string `json:"title"`
	Description string `json:"description"`
	State       string `json:"state"`
	WebURL      string `json:"web_url"`
}

// PullRequestCreator defines the contract for creating a pull request on external VCS.
type PullRequestCreator interface {
	CreatePullRequest(ctx context.Context, repo, head, base, title, body string) (*PullRequestResponse, error)
}

// GitHubReporterConfig configures the GitHub REST API issue reporter.
type GitHubReporterConfig struct {
	Token        string        `json:"-"`
	BaseURL      string        `json:"base_url"`
	HTTPClient   *http.Client  `json:"-"`
	MaxRetries   int           `json:"max_retries"`
	RetryBackoff time.Duration `json:"retry_backoff"`
	UserAgent    string        `json:"user_agent"`
}

// GitHubIssueReporter posts comments and creates pull requests using the GitHub REST API.
type GitHubIssueReporter struct {
	cfg        GitHubReporterConfig
	httpClient *http.Client
}

// NewGitHubIssueReporter constructs an initialized GitHubIssueReporter.
func NewGitHubIssueReporter(cfg GitHubReporterConfig) *GitHubIssueReporter {
	if cfg.BaseURL == "" {
		cfg.BaseURL = "https://api.github.com"
	}
	cfg.BaseURL = strings.TrimRight(cfg.BaseURL, "/")

	if cfg.UserAgent == "" {
		cfg.UserAgent = "gv-factory/1.0"
	}
	if cfg.MaxRetries < 0 {
		cfg.MaxRetries = 0
	} else if cfg.MaxRetries == 0 {
		cfg.MaxRetries = 3
	}
	if cfg.RetryBackoff <= 0 {
		cfg.RetryBackoff = 300 * time.Millisecond
	}

	client := cfg.HTTPClient
	if client == nil {
		client = &http.Client{
			Timeout: 30 * time.Second,
		}
	}

	return &GitHubIssueReporter{
		cfg:        cfg,
		httpClient: client,
	}
}

// PostIssueComment posts a markdown comment on a GitHub issue or pull request.
func (r *GitHubIssueReporter) PostIssueComment(ctx context.Context, repo string, issueNumber int, comment string) error {
	if strings.TrimSpace(repo) == "" {
		return errors.New("repository cannot be empty")
	}
	parts := strings.Split(repo, "/")
	if len(parts) != 2 || parts[0] == "" || parts[1] == "" {
		return fmt.Errorf("invalid repository format %q, expected 'owner/repo'", repo)
	}
	if issueNumber <= 0 {
		return fmt.Errorf("invalid issue number %d, must be greater than 0", issueNumber)
	}
	if strings.TrimSpace(comment) == "" {
		return errors.New("comment body cannot be empty")
	}

	endpoint := fmt.Sprintf("%s/repos/%s/issues/%d/comments", r.cfg.BaseURL, repo, issueNumber)
	payload := map[string]string{
		"body": comment,
	}
	bodyBytes, err := json.Marshal(payload)
	if err != nil {
		return fmt.Errorf("failed to encode comment payload: %w", err)
	}

	reqFunc := func() (*http.Request, error) {
		req, err := http.NewRequestWithContext(ctx, http.MethodPost, endpoint, bytes.NewReader(bodyBytes))
		if err != nil {
			return nil, err
		}
		r.applyHeaders(req)
		return req, nil
	}

	respBytes, err := r.executeWithRetry(ctx, reqFunc, http.StatusCreated)
	if err != nil {
		return fmt.Errorf("failed to post GitHub issue comment: %w", err)
	}
	_ = respBytes

	return nil
}

// CreatePullRequest creates a new Pull Request on GitHub.
func (r *GitHubIssueReporter) CreatePullRequest(ctx context.Context, repo, head, base, title, body string) (*PullRequestResponse, error) {
	if strings.TrimSpace(repo) == "" {
		return nil, errors.New("repository cannot be empty")
	}
	if strings.TrimSpace(head) == "" {
		return nil, errors.New("head branch cannot be empty")
	}
	if strings.TrimSpace(base) == "" {
		return nil, errors.New("base branch cannot be empty")
	}
	if strings.TrimSpace(title) == "" {
		return nil, errors.New("title cannot be empty")
	}

	endpoint := fmt.Sprintf("%s/repos/%s/pulls", r.cfg.BaseURL, repo)
	payload := map[string]string{
		"title": title,
		"head":  head,
		"base":  base,
		"body":  body,
	}
	bodyBytes, err := json.Marshal(payload)
	if err != nil {
		return nil, fmt.Errorf("failed to encode pull request payload: %w", err)
	}

	reqFunc := func() (*http.Request, error) {
		req, err := http.NewRequestWithContext(ctx, http.MethodPost, endpoint, bytes.NewReader(bodyBytes))
		if err != nil {
			return nil, err
		}
		r.applyHeaders(req)
		return req, nil
	}

	respBytes, err := r.executeWithRetry(ctx, reqFunc, http.StatusCreated)
	if err != nil {
		return nil, fmt.Errorf("failed to create GitHub pull request: %w", err)
	}

	var prResp PullRequestResponse
	if err := json.Unmarshal(respBytes, &prResp); err != nil {
		return nil, fmt.Errorf("failed to parse GitHub pull request response: %w", err)
	}

	return &prResp, nil
}

func (r *GitHubIssueReporter) applyHeaders(req *http.Request) {
	req.Header.Set("Content-Type", "application/json")
	req.Header.Set("Accept", "application/vnd.github+json")
	req.Header.Set("User-Agent", r.cfg.UserAgent)
	if r.cfg.Token != "" {
		token := r.cfg.Token
		if !strings.HasPrefix(strings.ToLower(token), "bearer ") {
			token = "Bearer " + token
		}
		req.Header.Set("Authorization", token)
	}
}

func (r *GitHubIssueReporter) executeWithRetry(
	ctx context.Context,
	reqFunc func() (*http.Request, error),
	expectedStatus int,
) ([]byte, error) {
	var lastErr error
	backoff := r.cfg.RetryBackoff

	for attempt := 0; attempt <= r.cfg.MaxRetries; attempt++ {
		if attempt > 0 {
			select {
			case <-ctx.Done():
				return nil, ctx.Err()
			case <-time.After(backoff):
				backoff *= 2
			}
		}

		req, err := reqFunc()
		if err != nil {
			return nil, err
		}

		resp, err := r.httpClient.Do(req)
		if err != nil {
			lastErr = err
			continue
		}

		respBody, readErr := io.ReadAll(resp.Body)
		_ = resp.Body.Close()
		if readErr != nil {
			lastErr = readErr
			continue
		}

		// Success
		if resp.StatusCode == expectedStatus || (expectedStatus == 0 && resp.StatusCode >= 200 && resp.StatusCode < 300) {
			return respBody, nil
		}

		// Determine if retryable (5xx status codes)
		if resp.StatusCode >= 500 {
			lastErr = fmt.Errorf("server error (HTTP %d): %s", resp.StatusCode, strings.TrimSpace(string(respBody)))
			continue
		}

		// Non-retryable client error (4xx)
		return nil, fmt.Errorf("request failed with status %d: %s", resp.StatusCode, strings.TrimSpace(string(respBody)))
	}

	return nil, fmt.Errorf("max retries exceeded: %w", lastErr)
}

// GitLabReporterConfig configures the GitLab REST API issue reporter.
type GitLabReporterConfig struct {
	Token        string        `json:"-"`
	BaseURL      string        `json:"base_url"`
	HTTPClient   *http.Client  `json:"-"`
	MaxRetries   int           `json:"max_retries"`
	RetryBackoff time.Duration `json:"retry_backoff"`
	UserAgent    string        `json:"user_agent"`
	AuthHeader   string        `json:"auth_header"` // "PRIVATE-TOKEN" or "Bearer"
}

// GitLabIssueReporter posts notes (comments) and creates merge requests on GitLab.
type GitLabIssueReporter struct {
	cfg        GitLabReporterConfig
	httpClient *http.Client
}

// NewGitLabIssueReporter constructs an initialized GitLabIssueReporter.
func NewGitLabIssueReporter(cfg GitLabReporterConfig) *GitLabIssueReporter {
	if cfg.BaseURL == "" {
		cfg.BaseURL = "https://gitlab.com/api/v4"
	}
	cfg.BaseURL = strings.TrimRight(cfg.BaseURL, "/")

	if cfg.UserAgent == "" {
		cfg.UserAgent = "gv-factory-gitlab/1.0"
	}
	if cfg.MaxRetries < 0 {
		cfg.MaxRetries = 0
	} else if cfg.MaxRetries == 0 {
		cfg.MaxRetries = 3
	}
	if cfg.RetryBackoff <= 0 {
		cfg.RetryBackoff = 300 * time.Millisecond
	}

	client := cfg.HTTPClient
	if client == nil {
		client = &http.Client{
			Timeout: 30 * time.Second,
		}
	}

	return &GitLabIssueReporter{
		cfg:        cfg,
		httpClient: client,
	}
}

// PostIssueComment posts a markdown comment (note) on a GitLab issue.
func (r *GitLabIssueReporter) PostIssueComment(ctx context.Context, repo string, issueNumber int, comment string) error {
	if strings.TrimSpace(repo) == "" {
		return errors.New("repository cannot be empty")
	}
	if issueNumber <= 0 {
		return fmt.Errorf("invalid issue number %d, must be greater than 0", issueNumber)
	}
	if strings.TrimSpace(comment) == "" {
		return errors.New("comment body cannot be empty")
	}

	escapedRepo := url.PathEscape(repo)
	endpoint := fmt.Sprintf("%s/projects/%s/issues/%d/notes", r.cfg.BaseURL, escapedRepo, issueNumber)

	payload := map[string]string{
		"body": comment,
	}
	bodyBytes, err := json.Marshal(payload)
	if err != nil {
		return fmt.Errorf("failed to encode note payload: %w", err)
	}

	reqFunc := func() (*http.Request, error) {
		req, err := http.NewRequestWithContext(ctx, http.MethodPost, endpoint, bytes.NewReader(bodyBytes))
		if err != nil {
			return nil, err
		}
		r.applyHeaders(req)
		return req, nil
	}

	respBytes, err := r.executeWithRetry(ctx, reqFunc, http.StatusCreated)
	if err != nil {
		return fmt.Errorf("failed to post GitLab note: %w", err)
	}
	_ = respBytes

	return nil
}

// CreateMergeRequest creates a new Merge Request on GitLab.
func (r *GitLabIssueReporter) CreateMergeRequest(ctx context.Context, repo, sourceBranch, targetBranch, title, description string) (*MergeRequestResponse, error) {
	if strings.TrimSpace(repo) == "" {
		return nil, errors.New("repository cannot be empty")
	}
	if strings.TrimSpace(sourceBranch) == "" {
		return nil, errors.New("source branch cannot be empty")
	}
	if strings.TrimSpace(targetBranch) == "" {
		return nil, errors.New("target branch cannot be empty")
	}
	if strings.TrimSpace(title) == "" {
		return nil, errors.New("title cannot be empty")
	}

	escapedRepo := url.PathEscape(repo)
	endpoint := fmt.Sprintf("%s/projects/%s/merge_requests", r.cfg.BaseURL, escapedRepo)

	payload := map[string]string{
		"source_branch": sourceBranch,
		"target_branch": targetBranch,
		"title":         title,
		"description":   description,
	}
	bodyBytes, err := json.Marshal(payload)
	if err != nil {
		return nil, fmt.Errorf("failed to encode merge request payload: %w", err)
	}

	reqFunc := func() (*http.Request, error) {
		req, err := http.NewRequestWithContext(ctx, http.MethodPost, endpoint, bytes.NewReader(bodyBytes))
		if err != nil {
			return nil, err
		}
		r.applyHeaders(req)
		return req, nil
	}

	respBytes, err := r.executeWithRetry(ctx, reqFunc, http.StatusCreated)
	if err != nil {
		return nil, fmt.Errorf("failed to create GitLab merge request: %w", err)
	}

	var mrResp MergeRequestResponse
	if err := json.Unmarshal(respBytes, &mrResp); err != nil {
		return nil, fmt.Errorf("failed to parse GitLab merge request response: %w", err)
	}

	return &mrResp, nil
}

// CreatePullRequest implements PullRequestCreator by wrapping CreateMergeRequest for GitLab.
func (r *GitLabIssueReporter) CreatePullRequest(ctx context.Context, repo, head, base, title, body string) (*PullRequestResponse, error) {
	mr, err := r.CreateMergeRequest(ctx, repo, head, base, title, body)
	if err != nil {
		return nil, err
	}

	return &PullRequestResponse{
		ID:      mr.ID,
		Number:  mr.IID,
		HTMLURL: mr.WebURL,
		URL:     mr.WebURL,
		State:   mr.State,
		Title:   mr.Title,
		Body:    mr.Description,
	}, nil
}

func (r *GitLabIssueReporter) applyHeaders(req *http.Request) {
	req.Header.Set("Content-Type", "application/json")
	req.Header.Set("Accept", "application/json")
	req.Header.Set("User-Agent", r.cfg.UserAgent)
	if r.cfg.Token != "" {
		req.Header.Set("PRIVATE-TOKEN", r.cfg.Token)
		if !strings.HasPrefix(strings.ToLower(r.cfg.Token), "bearer ") {
			req.Header.Set("Authorization", "Bearer "+r.cfg.Token)
		} else {
			req.Header.Set("Authorization", r.cfg.Token)
		}
	}
}

func (r *GitLabIssueReporter) executeWithRetry(
	ctx context.Context,
	reqFunc func() (*http.Request, error),
	expectedStatus int,
) ([]byte, error) {
	var lastErr error
	backoff := r.cfg.RetryBackoff

	for attempt := 0; attempt <= r.cfg.MaxRetries; attempt++ {
		if attempt > 0 {
			select {
			case <-ctx.Done():
				return nil, ctx.Err()
			case <-time.After(backoff):
				backoff *= 2
			}
		}

		req, err := reqFunc()
		if err != nil {
			return nil, err
		}

		resp, err := r.httpClient.Do(req)
		if err != nil {
			lastErr = err
			continue
		}

		respBody, readErr := io.ReadAll(resp.Body)
		_ = resp.Body.Close()
		if readErr != nil {
			lastErr = readErr
			continue
		}

		// Success
		if resp.StatusCode == expectedStatus || (expectedStatus == 0 && resp.StatusCode >= 200 && resp.StatusCode < 300) {
			return respBody, nil
		}

		// Determine if retryable (5xx status codes)
		if resp.StatusCode >= 500 {
			lastErr = fmt.Errorf("server error (HTTP %d): %s", resp.StatusCode, strings.TrimSpace(string(respBody)))
			continue
		}

		return nil, fmt.Errorf("request failed with status %d: %s", resp.StatusCode, strings.TrimSpace(string(respBody)))
	}

	return nil, fmt.Errorf("max retries exceeded: %w", lastErr)
}

// CompositeIssueReporter routes feedback comments and pull requests to GitHub or GitLab reporters based on source or repo.
type CompositeIssueReporter struct {
	github *GitHubIssueReporter
	gitlab *GitLabIssueReporter
}

// NewCompositeIssueReporter constructs a CompositeIssueReporter.
func NewCompositeIssueReporter(github *GitHubIssueReporter, gitlab *GitLabIssueReporter) *CompositeIssueReporter {
	return &CompositeIssueReporter{
		github: github,
		gitlab: gitlab,
	}
}

// PostIssueComment posts a comment using the default (GitHub) reporter.
func (c *CompositeIssueReporter) PostIssueComment(ctx context.Context, repo string, issueNumber int, comment string) error {
	if c.github != nil {
		return c.github.PostIssueComment(ctx, repo, issueNumber, comment)
	}
	if c.gitlab != nil {
		return c.gitlab.PostIssueComment(ctx, repo, issueNumber, comment)
	}
	return errors.New("no issue reporter configured")
}

// PostIssueCommentWithSource posts a comment to the appropriate provider based on the event source.
func (c *CompositeIssueReporter) PostIssueCommentWithSource(ctx context.Context, source, repo string, issueNumber int, comment string) error {
	normSource := strings.ToLower(strings.TrimSpace(source))
	switch normSource {
	case schema.SourceGitLab:
		if c.gitlab != nil {
			return c.gitlab.PostIssueComment(ctx, repo, issueNumber, comment)
		}
	default:
		if c.github != nil {
			return c.github.PostIssueComment(ctx, repo, issueNumber, comment)
		}
		if c.gitlab != nil {
			return c.gitlab.PostIssueComment(ctx, repo, issueNumber, comment)
		}
	}

	return errors.New("no issue reporter configured for source")
}

// CreatePullRequest delegates to the GitHub reporter or GitLab reporter as appropriate.
func (c *CompositeIssueReporter) CreatePullRequest(ctx context.Context, repo, head, base, title, body string) (*PullRequestResponse, error) {
	if c.github != nil {
		return c.github.CreatePullRequest(ctx, repo, head, base, title, body)
	}
	if c.gitlab != nil {
		return c.gitlab.CreatePullRequest(ctx, repo, head, base, title, body)
	}
	return nil, errors.New("no pull request creator configured")
}
