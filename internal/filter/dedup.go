package filter

import (
	"news_agent/internal/article"
	"news_agent/internal/storage"
	"news_agent/internal/textutil"
)

func DedupByURL(articles []article.Article, store *storage.Store) []article.Article {
	seen, err := store.RecentURLs(7)
	if err != nil {
		seen = make(map[string]bool)
	}

	var result []article.Article
	for _, a := range articles {
		if !seen[a.URL] {
			result = append(result, a)
			seen[a.URL] = true
		}
	}
	return result
}

func DedupSimilarTitles(articles []article.Article, threshold float64) []article.Article {
	var result []article.Article
	for _, a := range articles {
		dup := false
		for i := range result {
			sim := textutil.HeadlineSimilarity(a.Title, result[i].Title)
			if sim >= threshold {
				dup = true
				if len([]rune(a.Content)) > len([]rune(result[i].Content)) {
					result[i].Content = a.Content
				}
				break
			}
		}
		if !dup {
			result = append(result, a)
		}
	}
	return result
}
