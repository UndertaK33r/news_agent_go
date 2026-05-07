package analyzer

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"io"
	"log/slog"
	"net/http"
	"strings"
	"time"

	"golang.org/x/sync/errgroup"

	"news_agent/internal/article"
)

const systemPrompt = `你是一位资深新闻分析师，在路透社和财新有十五年工作经验。你的分析风格犀利、简洁、有洞察力。
要求：
- 每个部分不超过80字，言之有物
- 不用"据悉""据了解"等废话
- 给出判断，不只是复述事实
- 如果有数据，点出关键数字
- 中文表述，专业名词保留英文`

type Client struct {
	hc     *http.Client
	apiKey string
	base   string
	model  string
}

func NewClient(apiKey, base, model string) *Client {
	return &Client{
		hc:     &http.Client{Timeout: 60 * time.Second},
		apiKey: apiKey,
		base:   strings.TrimRight(base, "/"),
		model:  model,
	}
}

type chatMessage struct {
	Role    string `json:"role"`
	Content string `json:"content"`
}

type chatRequest struct {
	Model    string        `json:"model"`
	Messages []chatMessage `json:"messages"`
}

type chatResponse struct {
	Choices []struct {
		Message struct {
			Content string `json:"content"`
		} `json:"message"`
	} `json:"choices"`
}

func (c *Client) chat(system, user string) (string, error) {
	req := chatRequest{
		Model: c.model,
		Messages: []chatMessage{
			{Role: "system", Content: system},
			{Role: "user", Content: user},
		},
	}

	body, err := json.Marshal(req)
	if err != nil {
		return "", err
	}

	httpReq, err := http.NewRequest("POST", c.base+"/v1/chat/completions", bytes.NewReader(body))
	if err != nil {
		return "", err
	}
	httpReq.Header.Set("Content-Type", "application/json")
	httpReq.Header.Set("Authorization", "Bearer "+c.apiKey)

	resp, err := c.hc.Do(httpReq)
	if err != nil {
		return "", err
	}
	defer resp.Body.Close()

	data, err := io.ReadAll(io.LimitReader(resp.Body, 1<<20))
	if err != nil {
		return "", err
	}

	if resp.StatusCode != 200 {
		return "", fmt.Errorf("API error %d: %s", resp.StatusCode, string(data[:min(len(data), 200)]))
	}

	var cr chatResponse
	if err := json.Unmarshal(data, &cr); err != nil {
		return "", err
	}

	if len(cr.Choices) == 0 {
		return "", fmt.Errorf("no choices in response")
	}

	return strings.TrimSpace(cr.Choices[0].Message.Content), nil
}

func AnalyzeSingle(c *Client, art article.Article) string {
	prompt := fmt.Sprintf(`请对以下这条新闻做四维拆解分析：

标题：%s
来源：%s
正文：%s

请按以下格式输出：

■ 事实
（一句话说清发生了什么，不要复述标题）

■ 动因
（为什么发生：政策推动/资本驱动/技术突破/竞争压力/外部冲击？给出你的判断）

■ 影响
（对行业、市场或普通人有何实质影响？量化说明优先级最高的一项）

■ 趋势
（这一事件指向什么更大的方向？用一句话预判后续发展）

请直接输出分析，不要任何开场白。`, art.Title, art.Source, trimText(art.Content, 3000))

	result, err := c.chat(systemPrompt, prompt)
	if err != nil {
		slog.Error("Single analysis failed", "title", art.Title, "error", err)
		return fmt.Sprintf("■ 事实\n%s\n\n■ 动因\n（分析生成失败）\n\n■ 影响\n待后续分析\n\n■ 趋势\n待后续分析", trimText(art.Content, 100))
	}
	return result
}

func AnalyzeCluster(c *Client, articles []article.Article) string {
	if len(articles) == 1 {
		return AnalyzeSingle(c, articles[0])
	}

	var sourcesText string
	for i, a := range articles {
		sourcesText += fmt.Sprintf("\n来源%d：【%s】\n标题：%s\n内容：%s\n", i+1, a.Source, a.Title, trimText(a.Content, 2000))
	}

	prompt := fmt.Sprintf(`以下 %d 篇报道来自不同媒体，讲述同一事件/话题。请做对比分析：

%s

请按以下格式输出：

■ 事实
（综合各方报道的共同核心事实，2-3句）

■ 分歧
（各媒体报道侧重点差异：A强调了什么而B没有提？如果有不一致之处，指出并给出你的判断谁更可信）

■ 语境
（这事在什么更大背景下发生？补充读者可能不知道的关键背景）

■ 走势
（根据当前信息，下一步最可能的发展是什么？给出你的预判）

请直接输出分析，不要任何开场白。`, len(articles), sourcesText)

	result, err := c.chat(systemPrompt, prompt)
	if err != nil {
		slog.Error("Cluster analysis failed", "count", len(articles), "error", err)
		var fallback string
		for _, a := range articles {
			fallback += fmt.Sprintf("  → [%s] %s\n", a.Source, a.Title)
		}
		return fmt.Sprintf("■ 事实\n（分析生成失败）\n\n以下为相关报道：\n%s", fallback)
	}

	var urls string
	for _, a := range articles {
		urls += fmt.Sprintf("  → %s：%s\n", a.Source, a.URL)
	}
	return fmt.Sprintf("%s\n\n※ 综合来源\n%s", result, urls)
}

func AnalyzeAll(c *Client, clusters [][]article.Article) []article.Analysis {
	g, _ := errgroup.WithContext(context.Background())
	g.SetLimit(5)

	results := make([]article.Analysis, len(clusters))

	for i, cluster := range clusters {
		i, cluster := i, cluster
		g.Go(func() error {
			slog.Info("Analyzing", "cluster", i+1, "total", len(clusters), "articles", len(cluster))
			text := AnalyzeCluster(c, cluster)

			var sources []article.SourceRef
			for _, a := range cluster {
				sources = append(sources, article.SourceRef{Name: a.Source, URL: a.URL})
			}

			results[i] = article.Analysis{
				ClusterID:    i,
				Title:        cluster[0].Title,
				Category:     cluster[0].Category,
				Text:         text,
				ArticleCount: len(cluster),
				Sources:      sources,
				PublishedAt:  cluster[0].PublishedAt,
			}
			return nil
		})
	}

	g.Wait()
	return results
}

func GenerateEditorial(c *Client, analyses []article.Analysis) article.Editorial {
	if len(analyses) == 0 {
		return article.Editorial{}
	}

	var digest string
	for i, a := range analyses {
		digest += fmt.Sprintf("\n--- 条目%d ---\n%s\n%s\n", i+1, a.Title, trimText(a.Text, 800))
	}

	essayPrompt := fmt.Sprintf(`以下是你今天分析过的所有新闻条目及其分析：

%s

现在请你退后一步，以大局观撰写一篇"今日总评"。注意：
- 不要逐条复述新闻
- 找出暗线关联
- 给出判断和立场
- 语言有锐度但不哗众取宠

请分三部分输出：

【今日格局】
用2-3句话概括今天的新闻大势，点出最重要的主线。

【暗线】
找出至少2组看似不相关但内在联系的新闻，分析其关联逻辑。格式："A ←→ B：关联逻辑..."

【一言】
今天读者只需要记住的一句话，是什么？

请直接输出，不要开场白。`, digest)

	essay, err := c.chat(systemPrompt, essayPrompt)
	if err != nil {
		slog.Error("Editorial essay failed", "error", err)
		essay = "（今日总评生成失败，请稍后重试）"
	}

	morningPrompt := fmt.Sprintf(`以下是你今天分析的全部新闻。请用最多5句话概括"今天最重要的3件事"。

%s

格式：
一、[事件简述] — [一句话为什么重要]
二、[事件简述] — [一句话为什么重要]
三、[事件简述] — [一句话为什么重要]

直接输出，不要开场白。`, trimText(digest, 3000))

	morning, err := c.chat("", morningPrompt)
	if err != nil {
		slog.Error("Morning brief failed", "error", err)
		morning = "（生成失败）"
	}

	return article.Editorial{
		MorningBrief: morning,
		EveningEssay: essay,
	}
}

func trimText(s string, n int) string {
	runes := []rune(s)
	if len(runes) > n {
		return string(runes[:n])
	}
	return s
}
