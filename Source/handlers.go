package main

import (
	"context"
	"errors"
	"log"
	"net/http"
	"net/url"
	"strconv"
	"strings"
	"sync"
	"time"
)

var (
	searchMu           sync.Mutex
	activeSearchCancel context.CancelFunc

	openMu       sync.Mutex
	lastOpenKey  string
	lastOpenTime time.Time

	transparentGIF = []byte{
		0x47, 0x49, 0x46, 0x38, 0x39, 0x61, 0x01, 0x00, 0x01, 0x00, 0x80, 0x00,
		0x00, 0x00, 0x00, 0x00, 0xff, 0xff, 0xff, 0x21, 0xf9, 0x04, 0x01, 0x00,
		0x00, 0x00, 0x00, 0x2c, 0x00, 0x00, 0x00, 0x00, 0x01, 0x00, 0x01, 0x00,
		0x00, 0x02, 0x02, 0x44, 0x01, 0x00, 0x3b,
	}
)

func withCORS(next http.HandlerFunc) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Access-Control-Allow-Origin", "https://www.bing.com")
		w.Header().Set("Access-Control-Allow-Methods", "GET")

		reqHeaders := r.Header.Get("Access-Control-Request-Headers")
		if reqHeaders != "" {
			w.Header().Set("Access-Control-Allow-Headers", reqHeaders)
		} else {
			w.Header().Set("Access-Control-Allow-Headers", "Content-Type")
		}

		w.Header().Set("Access-Control-Allow-Credentials", "true")
		w.Header().Set("Cache-Control", "no-cache, no-store, must-revalidate")

		if r.Method == http.MethodOptions {
			w.WriteHeader(http.StatusOK)
			return
		}
		next(w, r)
	}
}

func handleSearch(w http.ResponseWriter, r *http.Request) {
	query := r.URL.Query().Get("qry")
	log.Printf("[SEARCH] qry=%q url=\"https://%s%s\"", query, r.Host, r.URL.String())
	if strings.TrimSpace(query) == "" {
		serveJSON(w, SuggestionResponse{Suggestions: []Suggestion{}})
		return
	}

	tokens := sanitizeQuery(query)
	if len(tokens) == 0 {
		serveJSON(w, SuggestionResponse{Suggestions: []Suggestion{}})
		return
	}
	cacheKey := strings.Join(tokens, " ")

	results := searchCache.get(cacheKey)
	if results != nil {
		log.Printf("[SEARCH] cache hit: %d results", len(results))
		serveJSON(w, SuggestionResponse{Suggestions: results})
		return
	}

	searchMu.Lock()
	if activeSearchCancel != nil {
		activeSearchCancel()
	}
	ctx, cancel := context.WithTimeout(r.Context(), 2500*time.Millisecond)
	activeSearchCancel = cancel
	searchMu.Unlock()

	defer func() {
		searchMu.Lock()
		cancel()
		searchMu.Unlock()
	}()

	results, err := searchEverything(ctx, query)
	if err != nil {
		if errors.Is(err, context.Canceled) || errors.Is(ctx.Err(), context.Canceled) {
			log.Printf("[SEARCH] superseded / canceled for query %q", query)
			return
		}
		log.Printf("[SEARCH] error for query %q: %v", query, err)
		serveJSON(w, SuggestionResponse{Suggestions: []Suggestion{}})
		return
	}

	if results == nil {
		results = []Suggestion{}
	}
	searchCache.set(cacheKey, results, 3*time.Second)
	log.Printf("[SEARCH] returning %d results", len(results))
	serveJSON(w, SuggestionResponse{Suggestions: results})
}

func handlePreview(w http.ResponseWriter, r *http.Request) {
	acceptLang := r.Header.Get("Accept-Language")

	filePath := r.URL.Query().Get("path")
	log.Printf("[PREVIEW] path=%q url=\"https://%s%s\"", filePath, r.Host, r.URL.String())

	data := buildPreviewData(r.Context(), filePath, acceptLang)

	var buf strings.Builder
	if err := previewTmpl.Execute(&buf, data); err != nil {
		log.Printf("preview template error: %v", err)
		http.Error(w, "preview error", http.StatusInternalServerError)
		return
	}
	html := []byte(buf.String())
	w.Header().Set("Content-Type", "text/html; charset=utf-8")
	w.Header().Set("Content-Length", strconv.Itoa(len(html)))
	w.Write(html)
}

func handleOpen(w http.ResponseWriter, r *http.Request) {
	filePath := r.URL.Query().Get("path")
	if filePath == "" {
		http.Error(w, "missing path parameter", http.StatusBadRequest)
		return
	}

	selectInExplorer := r.URL.Query().Get("select") == "1"
	isBackground := r.URL.Query().Get("bg") == "1"

	key := filePath + "|select=" + strconv.FormatBool(selectInExplorer)
	now := time.Now()

	openMu.Lock()
	isDuplicate := (key == lastOpenKey && now.Sub(lastOpenTime) < 600*time.Millisecond)
	if !isDuplicate {
		lastOpenKey = key
		lastOpenTime = now
	}
	openMu.Unlock()

	if isDuplicate {
		log.Printf("[OPEN] duplicate request within cooldown for %s (select=%t)", filePath, selectInExplorer)
	} else {
		log.Printf("[OPEN] opening file: %s (select=%t)", filePath, selectInExplorer)
		if err := launchPath(filePath, selectInExplorer); err != nil {
			log.Printf("[OPEN] error opening %s: %v", filePath, err)
			if !isBackground {
				http.Error(w, err.Error(), http.StatusInternalServerError)
				return
			}
		}
	}

	if isBackground {
		w.Header().Set("Content-Type", "image/gif")
		w.Header().Set("Cache-Control", "no-store, no-cache, must-revalidate")
		w.Header().Set("Content-Length", strconv.Itoa(len(transparentGIF)))
		w.Write(transparentGIF)
		return
	}

	w.Header().Set("Content-Type", "text/html; charset=utf-8")
	w.Write([]byte("<!DOCTYPE html><html><body><script>window.close();</script><p>Opening file...</p></body></html>"))
}

func handleFile(w http.ResponseWriter, r *http.Request) {
	filePath := r.URL.Query().Get("path")
	if filePath == "" {
		http.Error(w, "missing path parameter", http.StatusBadRequest)
		return
	}
	http.ServeFile(w, r, filePath)
}

func buildPreviewData(ctx context.Context, filePath, acceptLang string) PreviewData {
	var data PreviewData
	if strings.TrimSpace(filePath) != "" {
		populateFileInfo(ctx, &data, filePath)
	}

	if data.FileName == "" {
		data.FileName = getTranslation(acceptLang, "NoResults")
		data.FilePath = filePath
		data.FileSize = "—"
		data.FileDate = "—"
	}
	if data.IsFolder {
		data.Icon = "📁"
	} else {
		data.Icon = iconForExt(data.FileExt)
	}

	data.StrPreviewTitle = getTranslation(acceptLang, "PreviewTitle")
	data.StrPath = getTranslation(acceptLang, "Path")
	data.StrSize = getTranslation(acceptLang, "Size")
	data.StrModified = getTranslation(acceptLang, "Modified")
	data.StrOpenFile = getTranslation(acceptLang, "OpenFile")
	data.StrOpenLocation = getTranslation(acceptLang, "OpenLocation")
	data.StrResolution = getTranslation(acceptLang, "Resolution")
	data.StrDuration = getTranslation(acceptLang, "Duration")
	data.StrCodec = getTranslation(acceptLang, "Codec")

	isOfflineOrCloud := isFileOfflineOrCloud(data.FilePath)

	switch data.FileExt {
	case ".jpg", ".jpeg", ".png", ".gif", ".bmp", ".webp", ".svg", ".ico":
		data.IsImage = true
		data.ImageSrcURL = baseURL() + "/api/file?path=" + url.QueryEscape(data.FilePath)
	case ".mp4", ".webm":
		data.IsVideo = true
		data.MediaSrcURL = baseURL() + "/api/file?path=" + url.QueryEscape(data.FilePath)
		if !isOfflineOrCloud {
			populateMediaInfo(ctx, &data)
		}
	case ".mp3", ".wav", ".flac", ".m4a", ".aac", ".ogg":
		data.IsAudio = true
		data.MediaSrcURL = baseURL() + "/api/file?path=" + url.QueryEscape(data.FilePath)
	case ".mov", ".mkv", ".avi", ".flv", ".wmv":
		if !isOfflineOrCloud {
			populateMediaInfo(ctx, &data)
		}
	case ".txt", ".md", ".json", ".ini", ".bat", ".cmd", ".ps1", ".go", ".py", ".js", ".ts", ".c", ".cpp", ".h", ".cs", ".csv", ".log", ".yaml", ".yml", ".xml", ".css", ".html":
		data.IsText = true
		if !isOfflineOrCloud {
			data.TextContent = readTextSnippet(data.FilePath)
		}
	}
	return data
}
