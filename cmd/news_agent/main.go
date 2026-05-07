package main

import (
	"fmt"
	"log/slog"
	"os"
	"path/filepath"
	"time"

	"news_agent/internal/analyzer"
	"news_agent/internal/cluster"
	"news_agent/internal/config"
	"news_agent/internal/fetcher"
	"news_agent/internal/filter"
	"news_agent/internal/output"
	"news_agent/internal/storage"
)

func main() {
	slog.SetDefault(slog.New(slog.NewTextHandler(os.Stdout, &slog.HandlerOptions{Level: slog.LevelInfo})))
	start := time.Now()
	slog.Info("=== News Agent Started ===")

	config.LoadDotenv(".env")
	cfg, err := config.Load("config.yaml")
	if err != nil {
		slog.Error("Load config failed", "error", err)
		os.Exit(1)
	}
	sources := cfg.FlattenSources()

	dbPath := filepath.Join("data", "news_data.db")
	store, err := storage.New(dbPath)
	if err != nil {
		slog.Error("Init storage failed", "error", err)
		os.Exit(1)
	}
	defer store.Close()

	slog.Info("[1/8] Fetching", "sources", len(sources))
	articles, err := fetcher.FetchAll(sources)
	if err != nil {
		slog.Error("Fetch failed", "error", err)
	}
	slog.Info("Fetched", "count", len(articles))

	slog.Info("[2/8] Deduplicating...")
	articles = filter.DedupByURL(articles, store)
	articles = filter.DedupSimilarTitles(articles, 0.65)
	slog.Info("After dedup", "count", len(articles))

	slog.Info("[3/8] Filtering by recency...")
	articles = filter.FilterByRecency(articles, 5)
	slog.Info("After recency", "count", len(articles))

	slog.Info("[4/8] Filtering by relevance...")
	articles = filter.FilterByKeywords(
		articles,
		cfg.Preferences.Keywords.HighPriority,
		cfg.Preferences.Keywords.Exclude,
		cfg.Summary.MaxNewsPerDay,
		cfg.Summary.MinNewsPerCategory,
	)
	slog.Info("After relevance", "count", len(articles))

	for _, a := range articles {
		store.SaveNews(a.Title, a.URL, a.Source, a.Category, a.Content, a.PublishedAt)
	}

	if len(articles) == 0 {
		slog.Info("No articles after filtering, exiting.")
		return
	}

	slog.Info("[5/8] Clustering...")
	clusters := cluster.Cluster(articles, cfg.Summary.ClusterThreshold)
	slog.Info("Formed clusters", "count", len(clusters))

	slog.Info("[6/8] Analyzing", "provider", cfg.LLM.Provider, "model", cfg.LLM.Model)
	apiKey := os.Getenv("DEEPSEEK_API_KEY")
	client := analyzer.NewClient(apiKey, cfg.LLM.APIBase, cfg.LLM.Model)
	analyses := analyzer.AnalyzeAll(client, clusters)
	slog.Info("Analyzed", "topics", len(analyses))

	slog.Info("[7/8] Generating editorial...")
	editorial := analyzer.GenerateEditorial(client, analyses)
	slog.Info("Editorial generated")

	slog.Info("[8/8] Rendering report...")
	reportDir := cfg.Output.ReportDir
	reportPath, err := output.RenderReport(analyses, editorial, reportDir)
	if err != nil {
		slog.Error("Render failed", "error", err)
		os.Exit(1)
	}

	today := time.Now().Format("2006-01-02")
	store.SaveReport(today, reportPath, len(analyses))

	if cfg.Output.EmailEnabled {
		slog.Info("Sending email...")
		if err := output.SendEmail(reportPath); err != nil {
			slog.Warn("Email failed", "error", err)
		}
	}

	elapsed := time.Since(start)
	slog.Info(fmt.Sprintf("=== Done in %.1fs ===", elapsed.Seconds()))
	fmt.Printf("\n日报已生成：%s\n", reportPath)
}
