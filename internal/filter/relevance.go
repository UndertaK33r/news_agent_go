package filter

import (
	"regexp"
	"sort"
	"strings"
	"time"

	"news_agent/internal/article"
)

func FilterByRecency(articles []article.Article, maxDays int) []article.Article {
	cutoff := time.Now().AddDate(0, 0, -maxDays)

	var result []article.Article
	for _, a := range articles {
		pub := a.PublishedAt
		if len(pub) >= 10 {
			pub = pub[:10]
		}
		t, err := time.Parse("2006-01-02", pub)
		if err != nil {
			result = append(result, a)
			continue
		}
		if !t.Before(cutoff) {
			result = append(result, a)
		}
	}
	return result
}

type scoredItem struct {
	a     article.Article
	score float64
}

func FilterByKeywords(
	articles []article.Article,
	highKeywords, excludeKeywords []string,
	maxArticles, minPerCategory int,
) []article.Article {
	if len(highKeywords) == 0 {
		return articles
	}

	highPattern := buildPattern(highKeywords)
	excludePattern := buildPattern(excludeKeywords)

	var items []scoredItem
	for _, a := range articles {
		score := scoreArticle(a, highPattern, excludePattern, highKeywords)
		if score >= 0 {
			items = append(items, scoredItem{a: a, score: score})
		}
	}

	byCat := make(map[string][]scoredItem)
	for _, s := range items {
		cat := s.a.Category
		if cat == "" {
			cat = "综合"
		}
		byCat[cat] = append(byCat[cat], s)
	}

	for cat := range byCat {
		sort.Slice(byCat[cat], func(i, j int) bool {
			return byCat[cat][i].score > byCat[cat][j].score
		})
	}

	cats := sortedKeys(byCat)
	var result []article.Article
	used := make(map[string]bool)
	catIdx := make(map[string]int)

	for round := 0; round < minPerCategory; round++ {
		for _, cat := range cats {
			items := byCat[cat]
			idx := catIdx[cat]
			if idx < len(items) && !used[items[idx].a.URL] && len(result) < maxArticles {
				result = append(result, items[idx].a)
				used[items[idx].a.URL] = true
				catIdx[cat]++
			}
		}
	}

	for _, s := range items {
		if !used[s.a.URL] && len(result) < maxArticles {
			result = append(result, s.a)
			used[s.a.URL] = true
		}
	}

	return result
}

func buildPattern(keywords []string) *regexp.Regexp {
	if len(keywords) == 0 {
		return nil
	}
	escaped := make([]string, len(keywords))
	for i, kw := range keywords {
		escaped[i] = regexp.QuoteMeta(kw)
	}
	return regexp.MustCompile(`(?i)` + strings.Join(escaped, "|"))
}

func scoreArticle(a article.Article, highPattern, excludePattern *regexp.Regexp, highKeywords []string) float64 {
	text := a.Title + " " + a.Content
	score := 0.0

	if highPattern != nil {
		matches := len(highPattern.FindAllString(text, -1))
		score = float64(min(matches*2, 10))
	}

	for _, kw := range highKeywords {
		if strings.Contains(strings.ToLower(a.Title), strings.ToLower(kw)) {
			score += 3.0
		}
	}

	if excludePattern != nil {
		matches := len(excludePattern.FindAllString(text, -1))
		score -= float64(matches * 3)
	}

	return max(score, 0)
}

func sortedKeys(m map[string][]scoredItem) []string {
	keys := make([]string, 0, len(m))
	for k := range m {
		keys = append(keys, k)
	}
	sort.Strings(keys)
	return keys
}
