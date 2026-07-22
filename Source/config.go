package main

import (
	"fmt"
	"os"
	"path/filepath"
)

const (
	defaultPort = 7654
	maxResults  = 10
)

var serverPort = defaultPort

func baseURL() string {
	return fmt.Sprintf("https://localhost:%d", serverPort)
}

func certDir() string {
	return filepath.Join(os.Getenv("LOCALAPPDATA"), "EverythingSearchProvider")
}

func certFile() string { return filepath.Join(certDir(), "cert.pem") }
func keyFile() string  { return filepath.Join(certDir(), "key.pem") }
func certDER() string  { return filepath.Join(certDir(), "root.cer") }
