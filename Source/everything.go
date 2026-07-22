package main

import (
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

var (
	esSemaphore = make(chan struct{}, 4)
	datePattern = regexp.MustCompile(`\d{4}-\d{2}-\d{2}T\d{2}:\d{2}:\d{2}`)
)

func searchEverything(query string) []Suggestion {
	dr := searchEverythingDetailed(query, maxResults)
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
	return out
}

func searchEverythingDetailed(query string, limit int) []detailedResult {
	exe := findEsExe()
	if exe == "" {
		return nil
	}

	select {
	case esSemaphore <- struct{}{}:
		defer func() { <-esSemaphore }()
	default:
		log.Printf("[ES] concurrency limit reached, skipping query %q", query)
		return nil
	}

	ctx, cancel := context.WithTimeout(context.Background(), 2*time.Second)
	defer cancel()

	var args []string
	if inst := os.Getenv("EVERYTHING_INSTANCE"); inst != "" {
		args = append(args, "-instance", inst)
	}
	args = append(args, "-n", strconv.Itoa(limit), "-size", "-dm", "-no-digit-grouping", "-date-format", "1")
	args = append(args, strings.Fields(query)...)

	cmd := exec.CommandContext(ctx, exe, args...)
	cmd.SysProcAttr = &syscall.SysProcAttr{HideWindow: true}
	log.Printf("[ES] running: %s", strings.Join(cmd.Args, " "))
	out, err := cmd.CombinedOutput()
	if err != nil {
		if errors.Is(err, context.DeadlineExceeded) {
			log.Printf("[ES] timeout (2s) for query %q", query)
		} else {
			log.Printf("[ES] error: %v | output: %s", err, strings.TrimSpace(string(out)))
		}
		return nil
	}
	log.Printf("[ES] raw output (%d bytes): %s", len(out), strings.TrimSpace(string(out)))

	return parseSearchOutput(string(out), limit)
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
