package fetcher

import (
	"context"
	"crypto/md5"
	"fmt"
	"io"
	"log/slog"
	"net/http"
	"strings"
	"time"

	"github.com/mmcdole/gofeed"
	"github.com/PuerkitoBio/goquery"
	"golang.org/x/sync/errgroup"

	"news_agent/internal/article"
	"news_agent/internal/config"
)

const (
	userAgent       = "Mozilla/5.0 (Windows NT 10.0; Win64; x64) AppleWebKit/537.36 (KHTML, like Gecko) Chrome/125.0.0.0 Safari/537.36"
	requestTimeout  = 20 * time.Second
	maxEntries      = 15
)

var httpClient = &http.Client{Timeout: requestTimeout}

func FetchAll(sources []config.SourceDef) ([]article.Article, error) {
	g, _ := errgroup.WithContext(context.Background())
	g.SetLimit(8)

	ch := make(chan []article.Article, len(sources))

	for _, src := range sources {
		src := src
		g.Go(func() error {
			articles := fetchSource(src)
			ch <- articles
			return nil
		})
	}

	if err := g.Wait(); err != nil {
		return nil, err
	}
	close(ch)

	var all []article.Article
	for batch := range ch {
		all = append(all, batch...)
	}

	slog.Info("Fetched", "total", len(all), "sources", len(sources))

	enriched := enrichAll(all)
	return enriched, nil
}

func fetchSource(src config.SourceDef) []article.Article {
	slog.Info("Fetching", "name", src.Name, "url", src.URL)

	req, err := http.NewRequest("GET", src.URL, nil)
	if err != nil {
		slog.Warn("Create request failed", "name", src.Name, "error", err)
		return nil
	}
	req.Header.Set("User-Agent", userAgent)

	resp, err := httpClient.Do(req)
	if err != nil {
		slog.Warn("Fetch failed", "name", src.Name, "error", err)
		return nil
	}
	defer resp.Body.Close()

	body, err := io.ReadAll(io.LimitReader(resp.Body, 5<<20))
	if err != nil {
		slog.Warn("Read body failed", "name", src.Name, "error", err)
		return nil
	}

	fp := gofeed.NewParser()
	feed, err := fp.ParseString(string(body))
	if err != nil {
		slog.Warn("Parse failed", "name", src.Name, "error", err)
		return nil
	}

	var articles []article.Article
	n := min(len(feed.Items), maxEntries)
	for i := 0; i < n; i++ {
		entry := feed.Items[i]
		if entry.Title == "" || entry.Link == "" {
			continue
		}

		published := parseTime(entry)
		content := entry.Content
		if content == "" {
			content = entry.Description
		}
		if content != "" {
			doc, err := goquery.NewDocumentFromReader(strings.NewReader(content))
			if err == nil {
				content = strings.TrimSpace(doc.Text())
			}
		}

		hash := md5.Sum([]byte(entry.Link))
		articles = append(articles, article.Article{
			ID:          fmt.Sprintf("%x", hash[:6]),
			Title:       strings.TrimSpace(entry.Title),
			URL:         strings.TrimSpace(entry.Link),
			Source:      src.Name,
			Category:    src.Category,
			Content:     trimLen(content, 3000),
			PublishedAt: published,
			FetchedAt:   time.Now().Format("2006-01-02 15:04:05"),
		})
	}

	slog.Info("  -> Got", "count", len(articles), "source", src.Name)
	return articles
}

func parseTime(entry *gofeed.Item) string {
	if entry.PublishedParsed != nil {
		return entry.PublishedParsed.Format("2006-01-02 15:04:05")
	}
	if entry.UpdatedParsed != nil {
		return entry.UpdatedParsed.Format("2006-01-02 15:04:05")
	}
	return time.Now().Format("2006-01-02 15:04:05")
}

func trimLen(s string, n int) string {
	s = strings.TrimSpace(s)
	runes := []rune(s)
	if len(runes) > n {
		return string(runes[:n])
	}
	return s
}

func enrichAll(articles []article.Article) []article.Article {
	g, _ := errgroup.WithContext(context.Background())
	g.SetLimit(8)

	type idxResult struct {
		idx     int
		content string
	}
	ch := make(chan idxResult, len(articles))

	for i, a := range articles {
		if len([]rune(a.Content)) >= 200 {
			continue
		}
		i, a := i, a
		g.Go(func() error {
			content := fetchContent(a.URL)
			ch <- idxResult{idx: i, content: content}
			return nil
		})
	}

	g.Wait()
	close(ch)

	for r := range ch {
		if r.content != "" {
			articles[r.idx].Content = r.content
		}
	}
	return articles
}

func fetchContent(url string) string {
	req, err := http.NewRequest("GET", url, nil)
	if err != nil {
		return ""
	}
	req.Header.Set("User-Agent", userAgent)

	resp, err := httpClient.Do(req)
	if err != nil {
		slog.Debug("Content fetch failed", "url", url, "error", err)
		return ""
	}
	defer resp.Body.Close()

	doc, err := goquery.NewDocumentFromReader(io.LimitReader(resp.Body, 5<<20))
	if err != nil {
		return ""
	}

	doc.Find("script, style, nav, footer, header, aside, iframe").Remove()

	var body *goquery.Selection
	if sel := doc.Find("article").First(); sel.Length() > 0 {
		body = sel
	} else if sel := doc.Find("main").First(); sel.Length() > 0 {
		body = sel
	} else {
		body = doc.Find("body").First()
	}

	var paras []string
	body.Find("p").Each(func(_ int, s *goquery.Selection) {
		text := strings.TrimSpace(s.Text())
		if len([]rune(text)) > 20 {
			paras = append(paras, text)
		}
	})

	return trimLen(strings.Join(paras, "\n"), 8000)
}
