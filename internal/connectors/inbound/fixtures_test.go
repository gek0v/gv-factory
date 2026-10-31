// SPDX-License-Identifier: Apache-2.0
// SPDX-FileCopyrightText: 2026 gv-factory contributors

package inbound

import (
	"crypto/hmac"
	"crypto/sha256"
	"encoding/hex"
)

// computeHMAC computes sha256=<hex> for testing payloads.
func computeHMAC(secret, body []byte) string {
	mac := hmac.New(sha256.New, secret)
	mac.Write(body)
	return "sha256=" + hex.EncodeToString(mac.Sum(nil))
}

const (
	testGitHubSecret = "gh-secret-token-12345"
	testGitLabSecret = "gl-secret-token-67890"
	testSentrySecret = "sentry-secret-token-abcde"
)

const fixtureGitHubIssueOpened = `{
  "action": "opened",
  "issue": {
    "number": 33,
    "title": "feat(connectors): multi-provider webhook ingestor",
    "created_at": "2026-10-31T20:00:00Z",
    "updated_at": "2026-10-31T20:00:00Z"
  },
  "repository": {
    "name": "gv-factory",
    "full_name": "gek0v/gv-factory"
  },
  "sender": {
    "login": "gek0v"
  }
}`

const fixtureGitHubPRReviewReq = `{
  "action": "review_requested",
  "pull_request": {
    "number": 42,
    "title": "Add webhook receiver",
    "head": {
      "ref": "feat/inbound-webhooks",
      "sha": "9f83a21"
    },
    "merged": false,
    "created_at": "2026-10-31T21:00:00Z"
  },
  "repository": {
    "full_name": "gek0v/gv-factory"
  },
  "sender": {
    "login": "octocat"
  }
}`

const fixtureGitLabMergeRequest = `{
  "object_kind": "merge_request",
  "user": {
    "username": "gitlab-user"
  },
  "project": {
    "path_with_namespace": "gitlab-org/project-x",
    "name": "project-x"
  },
  "object_attributes": {
    "iid": 105,
    "action": "open",
    "source_branch": "feature/ai-agent",
    "created_at": "2026-10-31T21:15:00Z"
  }
}`

const fixtureGitLabPipeline = `{
  "object_kind": "pipeline",
  "project": {
    "path_with_namespace": "gitlab-org/project-x"
  },
  "user": {
    "username": "ci-runner"
  },
  "object_attributes": {
    "id": 999,
    "ref": "main",
    "status": "success",
    "finished_at": "2026-10-31T21:30:00Z"
  }
}`

const fixtureSentryAlert = `{
  "action": "triggered",
  "actor": {
    "name": "sentry-alert-rule"
  },
  "data": {
    "issue": {
      "id": "12345",
      "project": {
        "slug": "gv-factory"
      },
      "metadata": {
        "value": "Database connection timeout"
      }
    }
  }
}`
