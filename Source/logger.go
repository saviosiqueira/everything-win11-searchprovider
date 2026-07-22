package main

import (
	"bytes"
	"os"
	"sync"
)

type tailLogWriter struct {
	filename  string
	file      *os.File
	maxLines  int
	keepLines int
	currLines int
	mu        sync.Mutex
}

func newTailLogWriter(filename string, maxLines, keepLines int) (*tailLogWriter, error) {
	b, _ := os.ReadFile(filename)
	currLines := bytes.Count(b, []byte{'\n'})

	f, err := os.OpenFile(filename, os.O_APPEND|os.O_CREATE|os.O_WRONLY, 0644)
	if err != nil {
		return nil, err
	}

	return &tailLogWriter{
		filename:  filename,
		file:      f,
		maxLines:  maxLines,
		keepLines: keepLines,
		currLines: currLines,
	}, nil
}

func (w *tailLogWriter) Write(p []byte) (n int, err error) {
	w.mu.Lock()
	defer w.mu.Unlock()

	n, err = w.file.Write(p)
	w.currLines += bytes.Count(p, []byte{'\n'})

	if w.currLines > w.maxLines {
		w.shrink()
	}

	return n, err
}

func (w *tailLogWriter) shrink() {
	w.file.Close()

	b, err := os.ReadFile(w.filename)
	if err != nil {
		w.file, _ = os.OpenFile(w.filename, os.O_APPEND|os.O_CREATE|os.O_WRONLY, 0644)
		return
	}

	lines := bytes.Split(b, []byte{'\n'})
	if len(lines) > w.keepLines {
		// Keep the last `keepLines` elements
		lines = lines[len(lines)-w.keepLines:]
	}

	w.file, err = os.OpenFile(w.filename, os.O_TRUNC|os.O_CREATE|os.O_WRONLY, 0644)
	if err != nil {
		return
	}

	newData := bytes.Join(lines, []byte{'\n'})
	w.file.Write(newData)
	w.currLines = bytes.Count(newData, []byte{'\n'})
}

func (w *tailLogWriter) Close() error {
	w.mu.Lock()
	defer w.mu.Unlock()
	if w.file != nil {
		return w.file.Close()
	}
	return nil
}
