package storage

import (
	"database/sql"
	"fmt"
	"os"
	"path/filepath"
	"time"

	_ "modernc.org/sqlite"
)

type Store struct {
	db *sql.DB
}

func New(dbPath string) (*Store, error) {
	dir := filepath.Dir(dbPath)
	if err := os.MkdirAll(dir, 0755); err != nil {
		return nil, fmt.Errorf("create dir: %w", err)
	}

	db, err := sql.Open("sqlite", dbPath+"?_journal_mode=WAL")
	if err != nil {
		return nil, fmt.Errorf("open db: %w", err)
	}

	if err := db.Ping(); err != nil {
		return nil, fmt.Errorf("ping db: %w", err)
	}

	s := &Store{db: db}
	return s, s.init()
}

func (s *Store) Close() error {
	return s.db.Close()
}

func (s *Store) init() error {
	_, err := s.db.Exec(`
		CREATE TABLE IF NOT EXISTS news (
			id INTEGER PRIMARY KEY AUTOINCREMENT,
			title TEXT NOT NULL,
			url TEXT NOT NULL UNIQUE,
			source TEXT NOT NULL,
			category TEXT DEFAULT '',
			content TEXT DEFAULT '',
			summary TEXT DEFAULT '',
			published_at TEXT DEFAULT '',
			fetched_at TEXT DEFAULT (datetime('now','localtime')),
			score REAL DEFAULT 0,
			is_duplicate INTEGER DEFAULT 0,
			cluster_id INTEGER DEFAULT 0
		)
	`)
	if err != nil {
		return fmt.Errorf("create news table: %w", err)
	}

	_, err = s.db.Exec(`
		CREATE TABLE IF NOT EXISTS daily_reports (
			id INTEGER PRIMARY KEY AUTOINCREMENT,
			report_date TEXT NOT NULL UNIQUE,
			html_path TEXT DEFAULT '',
			news_count INTEGER DEFAULT 0,
			created_at TEXT DEFAULT (datetime('now','localtime'))
		)
	`)
	if err != nil {
		return fmt.Errorf("create daily_reports table: %w", err)
	}

	_, _ = s.db.Exec("CREATE INDEX IF NOT EXISTS idx_news_url ON news(url)")
	_, _ = s.db.Exec("CREATE INDEX IF NOT EXISTS idx_news_published ON news(published_at)")
	_, _ = s.db.Exec("CREATE INDEX IF NOT EXISTS idx_news_fetched ON news(fetched_at)")

	return nil
}

func (s *Store) SaveNews(title, url, source, category, content, publishedAt string) (bool, error) {
	result, err := s.db.Exec(
		`INSERT OR IGNORE INTO news (title, url, source, category, content, published_at, fetched_at)
		 VALUES (?, ?, ?, ?, ?, ?, datetime('now','localtime'))`,
		title, url, source, category, content, publishedAt,
	)
	if err != nil {
		return false, err
	}
	n, _ := result.RowsAffected()
	return n > 0, nil
}

func (s *Store) IsURLExists(url string) bool {
	var exists bool
	s.db.QueryRow("SELECT EXISTS(SELECT 1 FROM news WHERE url=?)", url).Scan(&exists)
	return exists
}

func (s *Store) RecentURLs(days int) (map[string]bool, error) {
	cutoff := time.Now().AddDate(0, 0, -days).Format("2006-01-02 15:04:05")
	rows, err := s.db.Query("SELECT url FROM news WHERE fetched_at >= ?", cutoff)
	if err != nil {
		return nil, err
	}
	defer rows.Close()

	seen := make(map[string]bool)
	for rows.Next() {
		var u string
		if err := rows.Scan(&u); err != nil {
			continue
		}
		seen[u] = true
	}
	return seen, rows.Err()
}

func (s *Store) SaveReport(date, htmlPath string, newsCount int) error {
	_, err := s.db.Exec(
		"INSERT OR REPLACE INTO daily_reports (report_date, html_path, news_count) VALUES (?, ?, ?)",
		date, htmlPath, newsCount,
	)
	return err
}
