package cluster

import (
	"regexp"
	"sort"
	"strings"

	"news_agent/internal/article"
)

var wordRe = regexp.MustCompile(`[\p{Han}a-zA-Z]{2,}`)

func Cluster(articles []article.Article, threshold float64) [][]article.Article {
	n := len(articles)
	if n <= 1 {
		var result [][]article.Article
		for _, a := range articles {
			result = append(result, []article.Article{a})
		}
		return result
	}

	parent := make([]int, n)
	for i := range parent {
		parent[i] = i
	}

	var find func(int) int
	find = func(x int) int {
		for parent[x] != x {
			parent[x] = parent[parent[x]]
			x = parent[x]
		}
		return x
	}

	union := func(x, y int) {
		px, py := find(x), find(y)
		if px != py {
			parent[px] = py
		}
	}

	for i := 0; i < n; i++ {
		for j := i + 1; j < n; j++ {
			titleSim := headlineSim(articles[i].Title, articles[j].Title)
			if titleSim >= threshold {
				union(i, j)
			} else if titleSim >= 0.3 {
				contentSim := contentOverlap(articles[i].Content, articles[j].Content)
				if contentSim >= 0.25 {
					union(i, j)
				}
			}
		}
	}

	groups := make(map[int][]article.Article)
	for i := 0; i < n; i++ {
		root := find(i)
		groups[root] = append(groups[root], articles[i])
	}

	var result [][]article.Article
	for _, g := range groups {
		result = append(result, g)
	}
	return result
}

func headlineSim(t1, t2 string) float64 {
	r1 := []rune(normalize(t1))
	r2 := []rune(normalize(t2))
	if len(r1) == 0 || len(r2) == 0 {
		return 0
	}
	m := lcsLen(r1, r2)
	return 2.0 * float64(m) / float64(len(r1)+len(r2))
}

func lcsLen(a, b []rune) int {
	n, m := len(a), len(b)
	prev := make([]int, m+1)
	curr := make([]int, m+1)
	for i := 1; i <= n; i++ {
		for j := 1; j <= m; j++ {
			if a[i-1] == b[j-1] {
				curr[j] = prev[j-1] + 1
			} else {
				if curr[j-1] > prev[j] {
					curr[j] = curr[j-1]
				} else {
					curr[j] = prev[j]
				}
			}
		}
		prev, curr = curr, prev
	}
	return prev[m]
}

func contentOverlap(c1, c2 string) float64 {
	kw1 := topKeywords(c1, 8)
	kw2 := topKeywords(c2, 8)
	if len(kw1) == 0 || len(kw2) == 0 {
		return 0
	}
	inter := intersect(kw1, kw2)
	union := len(kw1) + len(kw2) - len(inter)
	if union == 0 {
		return 0
	}
	return float64(len(inter)) / float64(union)
}

func topKeywords(text string, n int) []string {
	if len([]rune(text)) > 2000 {
		text = string([]rune(text)[:2000])
	}
	words := wordRe.FindAllString(strings.ToLower(text), -1)
	freq := make(map[string]int)
	for _, w := range words {
		if len([]rune(w)) > 2 || isASCII(w) {
			freq[w]++
		}
	}
	type kv struct {
		k string
		v int
	}
	var pairs []kv
	for k, v := range freq {
		pairs = append(pairs, kv{k, v})
	}
	sort.Slice(pairs, func(i, j int) bool { return pairs[i].v > pairs[j].v })
	result := make([]string, 0, n)
	for i := 0; i < len(pairs) && i < n; i++ {
		result = append(result, pairs[i].k)
	}
	return result
}

func normalize(text string) string {
	text = strings.ToLower(text)
	return regexp.MustCompile(`[^\p{Han}a-zA-Z0-9]`).ReplaceAllString(text, "")
}

func intersect(a, b []string) []string {
	set := make(map[string]bool)
	for _, s := range a {
		set[s] = true
	}
	var result []string
	for _, s := range b {
		if set[s] {
			result = append(result, s)
		}
	}
	return result
}

func isASCII(s string) bool {
	for _, r := range s {
		if r > 127 {
			return false
		}
	}
	return true
}
