package article

type Article struct {
	ID          string  `json:"id"`
	Title       string  `json:"title"`
	URL         string  `json:"url"`
	Source      string  `json:"source"`
	Category    string  `json:"category"`
	Content     string  `json:"content"`
	PublishedAt string  `json:"published_at"`
	FetchedAt   string  `json:"fetched_at"`
	Score       float64 `json:"score"`
	ClusterID   int     `json:"cluster_id"`
}

type Analysis struct {
	ClusterID    int
	Title        string
	Category     string
	Text         string
	ArticleCount int
	Sources      []SourceRef
	PublishedAt  string
}

type SourceRef struct {
	Name string
	URL  string
}

type Editorial struct {
	MorningBrief string
	EveningEssay string
}
