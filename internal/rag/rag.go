// Package rag indexes project documentation and retrieves the most relevant
// chunks with file:line references. Ranking is lexical: the local llama engine
// does not expose an embedding API yet, so an embedding hook is a follow-up.
package rag

import (
	"database/sql"
	"fmt"
	"os"
	"path/filepath"
	"sort"
	"strings"

	_ "modernc.org/sqlite"
)

const (
	chunkLines = 30
	chunkStep  = 20
	maxFiles   = 200
)

// Chunk — part of a document with its exact source location.
type Chunk struct {
	File string
	Line int // 1-based
	Text string
}

// Hit — a chunk with its relevance score.
type Hit struct {
	Chunk
	Score float64
}

// Store owns the SQLite database. The driver is pure Go, so stub builds work.
type Store struct {
	db *sql.DB
}

func Open(path string) (*Store, error) {
	if dir := filepath.Dir(path); dir != "." && dir != "" {
		if err := os.MkdirAll(dir, 0o700); err != nil {
			return nil, fmt.Errorf("rag dir: %w", err)
		}
	}
	db, err := sql.Open("sqlite", path)
	if err != nil {
		return nil, err
	}
	s := &Store{db: db}
	if _, err := db.Exec(`CREATE TABLE IF NOT EXISTS chunks (
		file TEXT NOT NULL,
		line INTEGER NOT NULL,
		text TEXT NOT NULL
	)`); err != nil {
		_ = db.Close()
		return nil, fmt.Errorf("rag schema: %w", err)
	}
	return s, nil
}

func (s *Store) Close() error { return s.db.Close() }

// docFiles returns the documentation we index: top-level guides plus docs/
// and doc/, excluding the archive (historical plans would drown the results).
func docFiles(root string) ([]string, error) {
	var files []string
	top, err := os.ReadDir(root)
	if err != nil {
		return nil, err
	}
	for _, e := range top {
		if e.IsDir() || !strings.HasSuffix(strings.ToLower(e.Name()), ".md") {
			continue
		}
		files = append(files, filepath.Join(root, e.Name()))
	}
	for _, dir := range []string{"docs", "doc"} {
		entries, err := os.ReadDir(filepath.Join(root, dir))
		if err != nil {
			continue
		}
		for _, e := range entries {
			if e.IsDir() {
				continue // doc/archive is intentionally skipped
			}
			if strings.HasSuffix(strings.ToLower(e.Name()), ".md") {
				files = append(files, filepath.Join(root, dir, e.Name()))
			}
		}
	}
	if len(files) > maxFiles {
		files = files[:maxFiles]
	}
	return files, nil
}

// Index rebuilds the chunk table from root. Returns the number of chunks.
func (s *Store) Index(root string) (int, error) {
	files, err := docFiles(root)
	if err != nil {
		return 0, err
	}
	tx, err := s.db.Begin()
	if err != nil {
		return 0, err
	}
	defer func() { _ = tx.Rollback() }()
	if _, err := tx.Exec("DELETE FROM chunks"); err != nil {
		return 0, err
	}
	stmt, err := tx.Prepare("INSERT INTO chunks (file, line, text) VALUES (?, ?, ?)")
	if err != nil {
		return 0, err
	}
	defer func() { _ = stmt.Close() }()

	count := 0
	for _, path := range files {
		data, err := os.ReadFile(path)
		if err != nil {
			continue
		}
		rel, err := filepath.Rel(root, path)
		if err != nil {
			rel = path
		}
		rel = filepath.ToSlash(rel)
		lines := strings.Split(string(data), "\n")
		for start := 0; start < len(lines); start += chunkStep {
			end := start + chunkLines
			if end > len(lines) {
				end = len(lines)
			}
			text := strings.TrimSpace(strings.Join(lines[start:end], "\n"))
			if text != "" {
				if _, err := stmt.Exec(rel, start+1, text); err != nil {
					return 0, err
				}
				count++
			}
			if end == len(lines) {
				break
			}
		}
	}
	if err := tx.Commit(); err != nil {
		return 0, err
	}
	return count, nil
}

var stopWords = map[string]bool{
	"the": true, "and": true, "how": true, "for": true, "with": true, "what": true,
	"from": true, "this": true, "that": true, "does": true, "use": true,
}

func terms(s string) []string {
	var out []string
	for _, word := range strings.FieldsFunc(strings.ToLower(s), func(r rune) bool {
		return (r < 'a' || r > 'z') && (r < '0' || r > '9') && (r < 'а' || r > 'я')
	}) {
		if len(word) < 2 || stopWords[word] {
			continue
		}
		out = append(out, word)
	}
	return out
}

// Search ranks chunks by term coverage with small bonuses for exact phrases
// and file-name matches. Results carry file:line so they can be verified.
func (s *Store) Search(query string, limit int) ([]Hit, error) {
	words := terms(query)
	if len(words) == 0 {
		return nil, nil
	}
	if limit <= 0 {
		limit = 5
	}
	rows, err := s.db.Query("SELECT file, line, text FROM chunks")
	if err != nil {
		return nil, err
	}
	defer func() { _ = rows.Close() }()

	var hits []Hit
	queryLower := strings.ToLower(query)
	for rows.Next() {
		var file, text string
		var line int
		if err := rows.Scan(&file, &line, &text); err != nil {
			return nil, err
		}
		body := strings.ToLower(text)
		score := 0.0
		seen := map[string]bool{}
		for _, w := range words {
			if seen[w] {
				continue
			}
			seen[w] = true
			if strings.Contains(body, w) {
				score += 1
			}
			if strings.Contains(strings.ToLower(file), w) {
				score += 0.25
			}
		}
		if score == 0 {
			continue
		}
		if strings.Contains(body, queryLower) {
			score += 2
		}
		hits = append(hits, Hit{Chunk: Chunk{File: file, Line: line, Text: text}, Score: score})
	}
	if err := rows.Err(); err != nil {
		return nil, err
	}
	sort.Slice(hits, func(i, j int) bool {
		if hits[i].Score == hits[j].Score {
			if hits[i].File == hits[j].File {
				return hits[i].Line < hits[j].Line
			}
			return hits[i].File < hits[j].File
		}
		return hits[i].Score > hits[j].Score
	})
	if len(hits) > limit {
		hits = hits[:limit]
	}
	return hits, nil
}
