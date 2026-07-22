package main

import (
	"log"
	"net/http"
	"net/url"
	"strconv"
	"strings"
	"time"
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

	results := searchCache.get(query)
	if results != nil {
		log.Printf("[SEARCH] cache hit: %d results", len(results))
		serveJSON(w, SuggestionResponse{Suggestions: results})
		return
	}

	results = searchEverything(query)
	if results == nil {
		results = []Suggestion{}
	}
	searchCache.set(query, results, 1*time.Second)
	log.Printf("[SEARCH] returning %d results", len(results))
	serveJSON(w, SuggestionResponse{Suggestions: results})
}

func handlePreview(w http.ResponseWriter, r *http.Request) {
	acceptLang := r.Header.Get("Accept-Language")

	filePath := r.URL.Query().Get("path")
	log.Printf("[PREVIEW] path=%q url=\"https://%s%s\"", filePath, r.Host, r.URL.String())

	data := buildPreviewData(filePath, acceptLang)

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
	log.Printf("[OPEN] opening file: %s", filePath)

	if err := launchPath(filePath); err != nil {
		log.Printf("[OPEN] error opening %s: %v", filePath, err)
		http.Error(w, err.Error(), http.StatusInternalServerError)
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

func buildPreviewData(filePath, acceptLang string) PreviewData {
	var data PreviewData
	if strings.TrimSpace(filePath) != "" {
		populateFileInfo(&data, filePath)
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

	switch data.FileExt {
	case ".jpg", ".jpeg", ".png", ".gif", ".bmp", ".webp", ".svg", ".ico":
		data.IsImage = true
		data.ImageSrcURL = baseURL() + "/api/file?path=" + url.QueryEscape(data.FilePath)
	case ".mp4", ".webm":
		data.IsVideo = true
		data.MediaSrcURL = baseURL() + "/api/file?path=" + url.QueryEscape(data.FilePath)
		populateMediaInfo(&data)
	case ".mp3", ".wav", ".flac", ".m4a", ".aac", ".ogg":
		data.IsAudio = true
		data.MediaSrcURL = baseURL() + "/api/file?path=" + url.QueryEscape(data.FilePath)
	case ".mov", ".mkv", ".avi", ".flv", ".wmv":
		populateMediaInfo(&data)
	case ".txt", ".md", ".json", ".ini", ".bat", ".cmd", ".ps1", ".go", ".py", ".js", ".ts", ".c", ".cpp", ".h", ".cs", ".csv", ".log", ".yaml", ".yml", ".xml", ".css", ".html":
		data.IsText = true
		data.TextContent = readTextSnippet(data.FilePath)
	}
	return data
}
