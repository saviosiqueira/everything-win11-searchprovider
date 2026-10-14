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

	if w.file == nil {
		var openErr error
		w.file, openErr = os.OpenFile(w.filename, os.O_APPEND|os.O_CREATE|os.O_WRONLY, 0644)
		if openErr != nil {
			return 0, openErr
		}
	}

	n, err = w.file.Write(p)
	w.currLines += bytes.Count(p, []byte{'\n'})

	if w.currLines > w.maxLines {
		w.shrink()
	}

	return n, err
}

func (w *tailLogWriter) shrink() {
	b, err := os.ReadFile(w.filename)
	if err != nil {
		w.currLines = w.keepLines
		return
	}

	lines := bytes.Split(b, []byte{'\n'})
	if len(lines) > w.keepLines {
		lines = lines[len(lines)-w.keepLines:]
	}

	tmpFilename := w.filename + ".tmp"
	newData := bytes.Join(lines, []byte{'\n'})
	if err := os.WriteFile(tmpFilename, newData, 0644); err != nil {
		w.currLines = w.keepLines
		return
	}

	if w.file != nil {
		_ = w.file.Close()
		w.file = nil
	}

	if err := os.Rename(tmpFilename, w.filename); err != nil {
		_ = os.Remove(tmpFilename)
		w.file, _ = os.OpenFile(w.filename, os.O_APPEND|os.O_CREATE|os.O_WRONLY, 0644)
		w.currLines = w.keepLines
		return
	}

	w.file, err = os.OpenFile(w.filename, os.O_APPEND|os.O_CREATE|os.O_WRONLY, 0644)
	if err != nil {
		w.currLines = w.keepLines
		return
	}
	w.currLines = bytes.Count(newData, []byte{'\n'})
}

func (w *tailLogWriter) Close() error {
	w.mu.Lock()
	defer w.mu.Unlock()
	if w.file != nil {
		err := w.file.Close()
		w.file = nil
		return err
	}
	return nil
}
