// SPDX-License-Identifier: Apache-2.0
// SPDX-FileCopyrightText: 2026 gv-factory contributors

package brain

import (
	"bufio"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"regexp"
	"strconv"
	"strings"
)

// ADRStatus represents the normalized lifecycle state of an Architectural Decision Record.
type ADRStatus string

const (
	ADRStatusAccepted   ADRStatus = "Accepted"
	ADRStatusProposed   ADRStatus = "Proposed"
	ADRStatusSuperseded ADRStatus = "Superseded"
	ADRStatusDeprecated ADRStatus = "Deprecated"
	ADRStatusRejected   ADRStatus = "Rejected"
	ADRStatusUnknown    ADRStatus = "Unknown"
)

// ObsoleteHandlingMode defines how superseded or deprecated ADRs are handled during synchronization.
type ObsoleteHandlingMode string

const (
	// ObsoleteModeMarkInfo sets severity to INFO and synchronizes the rule (default).
	ObsoleteModeMarkInfo ObsoleteHandlingMode = "MARK_INFO"
	// ObsoleteModeSkip ignores superseded or deprecated ADRs entirely.
	ObsoleteModeSkip ObsoleteHandlingMode = "SKIP"
	// ObsoleteModeDelete removes the rule from architectural_rules if it exists.
	ObsoleteModeDelete ObsoleteHandlingMode = "DELETE"
)

// ErrInvalidADRFormat is returned when an ADR file cannot be parsed or lacks mandatory elements.
var ErrInvalidADRFormat = errors.New("brain: invalid ADR markdown format")

// ADRDocument represents a fully parsed Architectural Decision Record from Markdown.
type ADRDocument struct {
	RuleCode     string            `json:"rule_code"`
	Number       int               `json:"number,omitempty"`
	Title        string            `json:"title"`
	Status       ADRStatus         `json:"status"`
	RawStatus    string            `json:"raw_status"`
	Date         string            `json:"date,omitempty"`
	Deciders     string            `json:"deciders,omitempty"`
	Context      string            `json:"context,omitempty"`
	Decision     string            `json:"decision,omitempty"`
	Consequences string            `json:"consequences,omitempty"`
	Description  string            `json:"description"`
	Severity     string            `json:"severity"`
	FilePath     string            `json:"file_path,omitempty"`
	Metadata     map[string]string `json:"metadata,omitempty"`
}

var (
	// Regex matching H1 formats:
	// 1. # ADR 0001: Microkernel ... OR # ADR-0001: Microkernel ...
	reADRHeaderNumber = regexp.MustCompile(`(?i)^#\s*ADR\s*[-:]?\s*(\d+)\s*[:.-]\s*(.+)$`)
	// 2. # 11. Inspección Granular ...
	reADRHeaderOrdinal = regexp.MustCompile(`(?i)^#\s*(\d+)\.\s*(.+)$`)
	// 3. # ADR-NAME: Description
	reADRHeaderNamed = regexp.MustCompile(`(?i)^#\s*ADR\s*[-:]?\s*([A-Za-z0-9_-]+)\s*[:.-]\s*(.+)$`)
	// 4. Any H1: # Title
	reGenericH1 = regexp.MustCompile(`^#\s+(.+)$`)

	// Regex for metadata lines
	reStatusMeta   = regexp.MustCompile(`(?i)^[-*]?\s*\*\*(?:Estado|Status):\*\*\s*(.+)$`)
	reDateMeta     = regexp.MustCompile(`(?i)^[-*]?\s*\*\*(?:Fecha|Date):\*\*\s*(.+)$`)
	reDecidersMeta = regexp.MustCompile(`(?i)^[-*]?\s*\*\*(?:Decisores|Deciders):\*\*\s*(.+)$`)

	// Regex for sections (H2)
	reSectionH2 = regexp.MustCompile(`(?i)^##\s+(.+)$`)

	// Filename digit prefix
	reFileDigits = regexp.MustCompile(`^(\d+)`)
)

// ParseADRFile reads and parses a Markdown ADR file from disk.
func ParseADRFile(filePath string) (*ADRDocument, error) {
	content, err := os.ReadFile(filePath)
	if err != nil {
		return nil, fmt.Errorf("brain: failed to read ADR file %s: %w", filePath, err)
	}

	doc, err := ParseADRContent(filepath.Base(filePath), string(content))
	if err != nil {
		return nil, err
	}
	doc.FilePath = filePath
	return doc, nil
}

// ParseADRContent parses an ADR from its markdown content and filename.
func ParseADRContent(filename string, content string) (*ADRDocument, error) {
	cleanContent := strings.TrimSpace(content)
	if cleanContent == "" {
		return nil, fmt.Errorf("%w: file content is empty", ErrInvalidADRFormat)
	}

	scanner := bufio.NewScanner(strings.NewReader(cleanContent))
	doc := &ADRDocument{
		Metadata: make(map[string]string),
	}

	var (
		foundH1       bool
		currentSec    string
		contextLines  []string
		decisionLines []string
		conseqLines   []string
		generalLines  []string
	)

	for scanner.Scan() {
		line := scanner.Text()
		trimmed := strings.TrimSpace(line)

		// Ignore SPDX / license comments before or anywhere in headers
		if strings.HasPrefix(trimmed, "// SPDX-") || strings.HasPrefix(trimmed, "/*") || strings.HasPrefix(trimmed, "*/") || strings.HasPrefix(trimmed, "* SPDX-") {
			continue
		}

		// Look for the primary H1 title
		if !foundH1 && strings.HasPrefix(trimmed, "#") && !strings.HasPrefix(trimmed, "##") {
			if strings.Contains(trimmed, "SPDX-License-Identifier") {
				continue
			}

			if parseH1(trimmed, filename, doc) {
				foundH1 = true
				continue
			}
		}

		if !foundH1 {
			continue
		}

		// Check for metadata
		if sm := reStatusMeta.FindStringSubmatch(trimmed); len(sm) > 1 {
			doc.RawStatus = strings.TrimSpace(sm[1])
			continue
		}
		if dm := reDateMeta.FindStringSubmatch(trimmed); len(dm) > 1 {
			doc.Date = strings.TrimSpace(dm[1])
			continue
		}
		if dcm := reDecidersMeta.FindStringSubmatch(trimmed); len(dcm) > 1 {
			doc.Deciders = strings.TrimSpace(dcm[1])
			continue
		}

		// Check for H2 sections
		if secMatch := reSectionH2.FindStringSubmatch(trimmed); len(secMatch) > 1 {
			secName := strings.ToLower(strings.TrimSpace(secMatch[1]))
			if strings.Contains(secName, "context") {
				currentSec = "context"
			} else if strings.Contains(secName, "decis") {
				currentSec = "decision"
			} else if strings.Contains(secName, "consecuenc") || strings.Contains(secName, "consequenc") {
				currentSec = "consequences"
			} else {
				currentSec = "other"
			}
			continue
		}

		// Skip horizontal rules
		if trimmed == "---" || trimmed == "***" || trimmed == "___" {
			continue
		}

		// Accumulate body lines
		switch currentSec {
		case "context":
			contextLines = append(contextLines, line)
		case "decision":
			decisionLines = append(decisionLines, line)
		case "consequences":
			conseqLines = append(conseqLines, line)
		default:
			// If not in a named section yet, keep as general body (ignoring top metadata bullets)
			if !strings.HasPrefix(trimmed, "* **") && !strings.HasPrefix(trimmed, "- **") {
				generalLines = append(generalLines, line)
			}
		}
	}

	if err := scanner.Err(); err != nil {
		return nil, fmt.Errorf("brain: scan error while parsing ADR: %w", err)
	}

	if !foundH1 {
		return nil, fmt.Errorf("%w: missing H1 title in %s", ErrInvalidADRFormat, filename)
	}

	// Extract & normalize status and severity
	doc.Status, doc.Severity = normalizeStatusAndSeverity(doc.RawStatus)

	// Clean sections
	doc.Context = strings.TrimSpace(strings.Join(contextLines, "\n"))
	doc.Decision = strings.TrimSpace(strings.Join(decisionLines, "\n"))
	doc.Consequences = strings.TrimSpace(strings.Join(conseqLines, "\n"))

	// Build description
	var descParts []string
	if doc.Context != "" {
		descParts = append(descParts, "Contexto:\n"+doc.Context)
	}
	if doc.Decision != "" {
		descParts = append(descParts, "Decisión:\n"+doc.Decision)
	}
	if doc.Consequences != "" {
		descParts = append(descParts, "Consecuencias:\n"+doc.Consequences)
	}

	if len(descParts) > 0 {
		doc.Description = strings.Join(descParts, "\n\n")
	} else {
		doc.Description = strings.TrimSpace(strings.Join(generalLines, "\n"))
	}

	// Defensive title length check for PostgreSQL VARCHAR(255) using runes for UTF-8 safety
	runes := []rune(doc.Title)
	if len(runes) > 255 {
		doc.Title = string(runes[:252]) + "..."
	}

	return doc, nil
}

func parseH1(headerLine, filename string, doc *ADRDocument) bool {
	// Try H1 with ADR number: # ADR 0001: Title
	if match := reADRHeaderNumber.FindStringSubmatch(headerLine); len(match) > 2 {
		num, _ := strconv.Atoi(match[1])
		doc.Number = num
		doc.RuleCode = fmt.Sprintf("ADR-%04d", num)
		doc.Title = strings.TrimSpace(match[2])
		return true
	}

	// Try ordinal format: # 11. Title
	if match := reADRHeaderOrdinal.FindStringSubmatch(headerLine); len(match) > 2 {
		num, _ := strconv.Atoi(match[1])
		doc.Number = num
		doc.RuleCode = fmt.Sprintf("ADR-%04d", num)
		doc.Title = strings.TrimSpace(match[2])
		return true
	}

	// Try named ADR: # ADR-SEC-01: Title
	if match := reADRHeaderNamed.FindStringSubmatch(headerLine); len(match) > 2 {
		doc.RuleCode = fmt.Sprintf("ADR-%s", strings.ToUpper(strings.TrimSpace(match[1])))
		doc.Title = strings.TrimSpace(match[2])
		return true
	}

	// Generic H1: # Title -> fallback to derive RuleCode from filename
	if match := reGenericH1.FindStringSubmatch(headerLine); len(match) > 1 {
		doc.Title = strings.TrimSpace(match[1])

		// Derive code from filename prefix
		base := strings.TrimSuffix(filename, filepath.Ext(filename))
		if digitMatch := reFileDigits.FindStringSubmatch(base); len(digitMatch) > 1 {
			num, _ := strconv.Atoi(digitMatch[1])
			doc.Number = num
			doc.RuleCode = fmt.Sprintf("ADR-%04d", num)
		} else {
			cleanSlug := strings.ReplaceAll(strings.ToUpper(base), "_", "-")
			cleanSlug = strings.ReplaceAll(cleanSlug, " ", "-")
			doc.RuleCode = fmt.Sprintf("ADR-%s", cleanSlug)
		}
		return true
	}

	return false
}

func normalizeStatusAndSeverity(raw string) (ADRStatus, string) {
	lower := strings.ToLower(strings.TrimSpace(raw))

	switch {
	case strings.Contains(lower, "aceptad") || strings.Contains(lower, "accept") ||
		strings.Contains(lower, "aprobad") || strings.Contains(lower, "approv"):
		return ADRStatusAccepted, "ERROR"

	case strings.Contains(lower, "propuest") || strings.Contains(lower, "propos") ||
		strings.Contains(lower, "draft") || strings.Contains(lower, "revis"):
		return ADRStatusProposed, "INFO"

	case strings.Contains(lower, "superad") || strings.Contains(lower, "supersed") ||
		strings.Contains(lower, "sustitui"):
		return ADRStatusSuperseded, "INFO"

	case strings.Contains(lower, "deprecat") || strings.Contains(lower, "deprecad") ||
		strings.Contains(lower, "obsolet"):
		return ADRStatusDeprecated, "INFO"

	case strings.Contains(lower, "rechazad") || strings.Contains(lower, "reject"):
		return ADRStatusRejected, "INFO"

	default:
		return ADRStatusUnknown, "INFO"
	}
}
