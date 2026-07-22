package main

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"net/http"
	"net/url"
	"os"
	"os/exec"
	"path/filepath"
	"strconv"
	"strings"
	"syscall"
	"time"
)

func launchPath(filePath string) error {
	dir := filepath.Dir(filePath)
	cmd := exec.Command("cmd", "/c", "start", "/d", dir, "", filePath)
	cmd.Dir = dir
	cmd.SysProcAttr = &syscall.SysProcAttr{HideWindow: true}
	return cmd.Start()
}

func handleProtocolActivation(rawURI string) {
	u, err := url.Parse(rawURI)
	if err != nil {
		return
	}
	filePath := u.Query().Get("path")
	if filePath != "" {
		_ = launchPath(filePath)
	}
}

func formatSize(bytes int64) string {
	if bytes < 1024 {
		return fmt.Sprintf("%d B", bytes)
	}
	const unit = 1024
	div, exp := int64(unit), 0
	for n := bytes / unit; n >= unit; n /= unit {
		div *= unit
		exp++
	}
	return fmt.Sprintf("%.1f %cB", float64(bytes)/float64(div), "KMGTPE"[exp])
}

func toFileURL(path string) string {
	return "file:///" + strings.ReplaceAll(path, "\\", "/")
}

func serveJSON(w http.ResponseWriter, v any) {
	data, err := json.Marshal(v)
	if err != nil {
		http.Error(w, err.Error(), http.StatusInternalServerError)
		return
	}
	w.Header().Set("Content-Type", "application/json; charset=utf-8")
	w.Header().Set("Content-Length", strconv.Itoa(len(data)))
	w.Write(data)
}

func iconForExt(ext string) string {
	switch ext {
	case ".exe", ".msi", ".bat", ".cmd", ".ps1":
		return "⚙️"
	case ".txt", ".log", ".md", ".ini", ".cfg":
		return "📄"
	case ".jpg", ".jpeg", ".png", ".gif", ".bmp", ".webp", ".svg", ".ico":
		return "🖼️"
	case ".mp3", ".wav", ".flac", ".ogg", ".aac", ".m4a":
		return "🎵"
	case ".mp4", ".avi", ".mkv", ".mov", ".wmv", ".webm":
		return "🎬"
	case ".pdf":
		return "📕"
	case ".doc", ".docx":
		return "📘"
	case ".xls", ".xlsx", ".csv":
		return "📊"
	case ".ppt", ".pptx":
		return "📙"
	case ".zip", ".rar", ".7z", ".tar", ".gz":
		return "📦"
	case ".go", ".py", ".rs", ".js", ".ts", ".c", ".cpp", ".h", ".java", ".cs":
		return "💻"
	case ".dll", ".sys", ".drv":
		return "🔧"
	case ".iso", ".img":
		return "💿"
	default:
		return "📄"
	}
}

func readTextSnippet(filePath string) string {
	f, err := os.Open(filePath)
	if err != nil {
		return ""
	}
	defer f.Close()

	buf := make([]byte, 2048)
	n, _ := f.Read(buf)
	if n == 0 {
		return ""
	}
	if bytes.IndexByte(buf[:n], 0) != -1 {
		return ""
	}
	return string(buf[:n])
}

func populateFileInfo(data *PreviewData, filePath string) {
	info, err := os.Stat(filePath)
	if err == nil {
		data.FileName = info.Name()
		data.FilePath = filePath
		if info.IsDir() {
			data.IsFolder = true
		} else {
			data.FileSize = formatSize(info.Size())
		}
		data.FileDate = info.ModTime().Format("2006-01-02 15:04:05")
		data.FileExt = strings.ToLower(filepath.Ext(info.Name()))
		data.FileURL = baseURL() + "/api/open?path=" + url.QueryEscape(filePath)
		data.DirURL = baseURL() + "/api/open?path=" + url.QueryEscape(filepath.Dir(filePath))
		data.DirPath = filepath.Dir(filePath)
	} else {
		results := searchEverythingDetailed(filePath, 1)
		if len(results) > 0 {
			d := results[0]
			data.FileName = d.name
			data.FilePath = d.path

			if info, err := os.Stat(d.path); err == nil {
				if info.IsDir() {
					data.IsFolder = true
				} else {
					data.FileSize = formatSize(info.Size())
				}
				data.FileDate = info.ModTime().Format("2006-01-02 15:04:05")
			} else {
				if d.size != "" {
					if sizeInt, err := strconv.ParseInt(d.size, 10, 64); err == nil {
						data.FileSize = formatSize(sizeInt)
					} else {
						data.FileSize = d.size
					}
				}
				data.FileDate = d.date
			}

			data.FileExt = strings.ToLower(filepath.Ext(d.name))
			data.FileURL = baseURL() + "/api/open?path=" + url.QueryEscape(d.path)
			data.DirURL = baseURL() + "/api/open?path=" + url.QueryEscape(filepath.Dir(d.path))
			data.DirPath = filepath.Dir(d.path)
		}
	}
}

func populateMediaInfo(data *PreviewData) {
	exe, err := exec.LookPath("ffprobe")
	if err != nil {
		return
	}

	ctx, contextCancel := context.WithTimeout(context.Background(), 1*time.Second)
	defer contextCancel()

	cmd := exec.CommandContext(ctx, exe, "-v", "quiet", "-print_format", "json", "-show_format", "-show_streams", data.FilePath)
	cmd.SysProcAttr = &syscall.SysProcAttr{HideWindow: true}

	out, err := cmd.Output()
	if err != nil {
		return
	}

	var parsed FFprobeOutput
	if err := json.Unmarshal(out, &parsed); err != nil {
		return
	}

	for _, s := range parsed.Streams {
		if s.CodecType == "video" {
			data.MediaCodec = s.CodecName
			data.MediaResolution = fmt.Sprintf("%dx%d", s.Width, s.Height)
			break
		} else if s.CodecType == "audio" && data.MediaCodec == "" {
			data.MediaCodec = s.CodecName
		}
	}

	if parsed.Format.Duration != "" {
		if sec, err := strconv.ParseFloat(parsed.Format.Duration, 64); err == nil {
			data.MediaDuration = fmt.Sprintf("%02d:%02d", int(sec)/60, int(sec)%60)
		}
	}

	if data.MediaDuration != "" || data.MediaResolution != "" || data.MediaCodec != "" {
		data.HasMediaInfo = true
	}
}
