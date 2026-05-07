package textutil

import (
	"regexp"
	"strings"
)

var nonAlphaRe = regexp.MustCompile(`[^\p{Han}a-zA-Z0-9]`)

func Normalize(text string) string {
	text = strings.ToLower(text)
	return nonAlphaRe.ReplaceAllString(text, "")
}

func HeadlineSimilarity(t1, t2 string) float64 {
	s1 := []rune(Normalize(t1))
	s2 := []rune(Normalize(t2))
	if len(s1) == 0 || len(s2) == 0 {
		return 0
	}
	matches := lcsLength(s1, s2)
	return 2.0 * float64(matches) / float64(len(s1)+len(s2))
}

func lcsLength(a, b []rune) int {
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
