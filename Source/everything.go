package main

import (
	"bytes"
	"context"
	"errors"
	"log"
	"net/url"
	"os"
	"os/exec"
	"path/filepath"
	"regexp"
	"strconv"
	"strings"
	"syscall"
	"time"
)

const createNoWindow = 0x08000000

var (
	ErrBusy                  = errors.New("search busy")
	ErrEverythingUnavailable = errors.New("everything IPC unavailable")
	esSemaphore              = make(chan struct{}, 2)
	datePattern              = regexp.MustCompile(`\d{4}-\d{2}-\d{2}T\d{2}:\d{2}:\d{2}`)
)

type boundedBuffer struct {
	buf bytes.Buffer
	cap int
}

func (b *boundedBuffer) Write(p []byte) (n int, err error) {
	remaining := b.cap - b.buf.Len()
	if remaining <= 0 {
		return len(p), nil
	}
	if len(p) > remaining {
		b.buf.Write(p[:remaining])
		return len(p), nil
	}
	return b.buf.Write(p)
}

func (b *boundedBuffer) String() string {
	return b.buf.String()
}

func isAsciiLetter(r rune) bool {
	return (r >= 'a' && r <= 'z') || (r >= 'A' && r <= 'Z')
}

func isRootDrive(s string) bool {
	if len(s) >= 3 && ((s[0] >= 'a' && s[0] <= 'z') || (s[0] >= 'A' && s[0] <= 'Z')) && s[1] == ':' {
		for i := 2; i < len(s); i++ {
			if s[i] != '\\' {
				return false
			}
		}
		return true
	}
	return false
}

func sanitizeQuery(raw string) []string {
	runes := []rune(raw)
	if len(runes) > 256 {
		runes = runes[:256]
	}

	cleaned := make([]rune, len(runes))
	for i, r := range runes {
		// Strip control characters (< 0x20, 0x7F) -> replace with space
		if r < 0x20 || r == 0x7F {
			cleaned[i] = ' '
			continue
		}
		// Replace illegal Windows filename / Everything operator characters " < > | ? * / with space
		switch r {
		case '"', '<', '>', '|', '?', '*', '/':
			cleaned[i] = ' '
		default:
			cleaned[i] = r
		}
	}

	// Replace ':' with space, except when it is a drive letter prefix (^[A-Za-z]:) at the start of a token
	for i := 0; i < len(cleaned); i++ {
		if cleaned[i] == ':' {
			isDrivePrefix := false
			if i >= 1 && isAsciiLetter(cleaned[i-1]) {
				if i == 1 || cleaned[i-2] == ' ' {
					isDrivePrefix = true
				}
			}
			if !isDrivePrefix {
				cleaned[i] = ' '
			}
		}
	}

	rawFields := strings.Fields(string(cleaned))
	if len(rawFields) == 0 {
		return nil
	}

	seen := make(map[string]bool)
	var tokens []string
	for _, field := range rawFields {
		var token string
		if isRootDrive(field) {
			token = field[:2]
		} else {
			token = strings.TrimRight(field, `\`)
		}
		if token == "" {
			continue
		}
		if !seen[token] {
			seen[token] = true
			tokens = append(tokens, token)
			if len(tokens) == 16 {
				break
			}
		}
	}

	if len(tokens) == 0 {
		return nil
	}
	return tokens
}

func buildCommandLine(exe string, limit int, tokens []string) string {
	var sb strings.Builder
	sb.WriteByte('"')
	sb.WriteString(exe)
	sb.WriteByte('"')
	if inst := os.Getenv("EVERYTHING_INSTANCE"); inst != "" {
		inst = strings.ReplaceAll(inst, `"`, "")
		sb.WriteString(` -instance "`)
		sb.WriteString(inst)
		sb.WriteByte('"')
	}
	sb.WriteString(" -n ")
	sb.WriteString(strconv.Itoa(limit))
	sb.WriteString(" -timeout 1000 -hide-empty-search-results -size -dm -no-digit-grouping -date-format 1")
	for _, token := range tokens {
		sb.WriteString(` "`)
		sb.WriteString(token)
		sb.WriteByte('"')
	}
	return sb.String()
}

func searchEverything(ctx context.Context, query string) ([]Suggestion, error) {
	dr, err := searchEverythingDetailed(ctx, query, maxResults)
	if err != nil {
		return nil, err
	}
	out := make([]Suggestion, 0, len(dr))
	for _, r := range dr {
		previewLink := baseURL() + "/api/preview?path=" + url.QueryEscape(r.path)
		openLink := baseURL() + "/api/open?path=" + url.QueryEscape(r.path)
		out = append(out, Suggestion{
			Text: r.name,
			Attributes: SuggestionAttributes{
				URL:            openLink,
				Query:          r.name,
				PreviewPaneURL: previewLink,
				PreviewURL:     previewLink,
			},
		})
	}
	return out, nil
}

func searchEverythingDetailed(ctx context.Context, query string, limit int) ([]detailedResult, error) {
	tokens := sanitizeQuery(query)
	if len(tokens) == 0 {
		return nil, nil
	}

	exe := findEsExe()
	if exe == "" {
		return nil, ErrEverythingUnavailable
	}

	select {
	case esSemaphore <- struct{}{}:
		defer func() { <-esSemaphore }()
	case <-ctx.Done():
		return nil, ctx.Err()
	}

	cmdLine := buildCommandLine(exe, limit, tokens)
	cmd := exec.CommandContext(ctx, exe)
	cmd.SysProcAttr = &syscall.SysProcAttr{
		CmdLine:       cmdLine,
		HideWindow:    true,
		CreationFlags: createNoWindow,
	}
	cmd.WaitDelay = 250 * time.Millisecond

	var stdout boundedBuffer
	stdout.cap = 64 * 1024
	var stderr boundedBuffer
	stderr.cap = 4 * 1024

	cmd.Stdout = &stdout
	cmd.Stderr = &stderr

	start := time.Now()
	err := cmd.Run()
	duration := time.Since(start)

	if err != nil {
		if errors.Is(ctx.Err(), context.Canceled) {
			return nil, ctx.Err()
		}
		if errors.Is(ctx.Err(), context.DeadlineExceeded) {
			log.Printf("[ES] timeout for query %q after %v", query, duration)
			return nil, ctx.Err()
		}
		stderrStr := strings.TrimSpace(stderr.String())
		log.Printf("[ES] error for query %q (%v): %s", query, err, stderrStr)
		if strings.Contains(stderrStr, "IPC") {
			return nil, ErrEverythingUnavailable
		}
		return nil, err
	}

	results := parseSearchOutput(stdout.String(), limit)
	log.Printf("[ES] query=%q results=%d duration=%v", query, len(results), duration)
	return results, nil
}

func parseSearchOutput(output string, limit int) []detailedResult {
	lines := strings.Split(strings.TrimSpace(output), "\n")
	var results []detailedResult

	for _, line := range lines {
		if len(results) >= limit {
			break
		}
		line = strings.TrimSpace(line)
		if line == "" {
			continue
		}

		loc := datePattern.FindStringIndex(line)
		if loc == nil {
			continue
		}

		dr := detailedResult{}
		dr.size = strings.TrimSpace(line[:loc[0]])
		dr.date = strings.TrimSpace(line[loc[0]:loc[1]])
		pathPart := strings.TrimSpace(line[loc[1]:])
		if pathPart == "" {
			continue
		}
		dr.path = pathPart
		dr.name = filepath.Base(dr.path)
		results = append(results, dr)
	}
	return results
}

func findEsExe() string {
	exePath, err := os.Executable()
	if err != nil {
		log.Printf("Failed to get executable path: %v", err)
		return ""
	}

	targetDir := filepath.Dir(exePath)
	esPath := filepath.Join(targetDir, "es.exe")

	if _, err := os.Stat(esPath); err == nil {
		return esPath
	}

	return ""
}
