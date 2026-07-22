package main

import (
	"sync"
	"time"
)

type queryCache struct {
	mu      sync.Mutex
	entries map[string]*cacheEntry
}

type cacheEntry struct {
	results   []Suggestion
	expiresAt time.Time
}

var searchCache = &queryCache{entries: make(map[string]*cacheEntry)}

func init() {
	go func() {
		for range time.Tick(5 * time.Minute) {
			searchCache.cleanup()
		}
	}()
}

func (qc *queryCache) cleanup() {
	qc.mu.Lock()
	defer qc.mu.Unlock()
	now := time.Now()
	for k, e := range qc.entries {
		if now.After(e.expiresAt) {
			delete(qc.entries, k)
		}
	}
}

func (qc *queryCache) get(query string) []Suggestion {
	qc.mu.Lock()
	defer qc.mu.Unlock()
	e, ok := qc.entries[query]
	if !ok || time.Now().After(e.expiresAt) {
		return nil
	}
	return e.results
}

func (qc *queryCache) set(query string, results []Suggestion, ttl time.Duration) {
	qc.mu.Lock()
	defer qc.mu.Unlock()
	qc.entries[query] = &cacheEntry{results: results, expiresAt: time.Now().Add(ttl)}
}
