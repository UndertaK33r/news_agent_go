package config

import (
	"os"
	"strings"

	"gopkg.in/yaml.v3"
)

type RSSHubConfig struct {
	BaseURL string `yaml:"base_url"`
}

type SourceDef struct {
	Name     string `yaml:"name"`
	URL      string `yaml:"url"`
	Category string `yaml:"category"`
	Language string `yaml:"-"`
}

type KeywordsPref struct {
	HighPriority []string `yaml:"high_priority"`
	Exclude      []string `yaml:"exclude"`
}

type Preferences struct {
	Keywords KeywordsPref `yaml:"keywords"`
}

type LLMConfig struct {
	Provider    string  `yaml:"provider"`
	APIBase     string  `yaml:"api_base"`
	Model       string  `yaml:"model"`
	Temperature float64 `yaml:"temperature"`
	MaxTokens   int     `yaml:"max_tokens"`
}

type SummaryConfig struct {
	MaxNewsPerDay      int     `yaml:"max_news_per_day"`
	MinNewsPerCategory int     `yaml:"min_news_per_category"`
	ClusterThreshold   float64 `yaml:"cluster_threshold"`
}

type OutputConfig struct {
	ReportDir    string `yaml:"report_dir"`
	HTMLEnabled  bool   `yaml:"html_enabled"`
	EmailEnabled bool   `yaml:"email_enabled"`
}

type ScheduleConfig struct {
	Time string `yaml:"time"`
}

type AppConfig struct {
	RSSHub      RSSHubConfig            `yaml:"rsshub"`
	NewsSources map[string][]SourceDef  `yaml:"news_sources"`
	Preferences Preferences             `yaml:"preferences"`
	LLM         LLMConfig               `yaml:"llm"`
	Summary     SummaryConfig           `yaml:"summary"`
	Output      OutputConfig            `yaml:"output"`
	Schedule    ScheduleConfig          `yaml:"schedule"`
}

func Load(path string) (*AppConfig, error) {
	data, err := os.ReadFile(path)
	if err != nil {
		return nil, err
	}

	var cfg AppConfig
	if err := yaml.Unmarshal(data, &cfg); err != nil {
		return nil, err
	}

	if cfg.Summary.MaxNewsPerDay == 0 {
		cfg.Summary.MaxNewsPerDay = 30
	}
	if cfg.Summary.ClusterThreshold == 0 {
		cfg.Summary.ClusterThreshold = 0.4
	}
	if cfg.Output.ReportDir == "" {
		cfg.Output.ReportDir = "data/reports"
	}
	if cfg.LLM.APIBase == "" {
		cfg.LLM.APIBase = "https://api.deepseek.com"
	}
	if cfg.LLM.Model == "" {
		cfg.LLM.Model = "deepseek-chat"
	}

	for lang, sources := range cfg.NewsSources {
		for i := range sources {
			sources[i].Language = lang
			sources[i].URL = strings.ReplaceAll(sources[i].URL, "{rsshub}", cfg.RSSHub.BaseURL)
		}
		cfg.NewsSources[lang] = sources
	}

	return &cfg, nil
}

func LoadDotenv(path string) {
	data, err := os.ReadFile(path)
	if err != nil {
		return
	}
	for _, line := range strings.Split(string(data), "\n") {
		line = strings.TrimSpace(line)
		if line == "" || strings.HasPrefix(line, "#") {
			continue
		}
		parts := strings.SplitN(line, "=", 2)
		if len(parts) != 2 {
			continue
		}
		key := strings.TrimSpace(parts[0])
		value := strings.TrimSpace(parts[1])
		value = strings.Trim(value, `"'`)
		if key != "" && value != "" {
			os.Setenv(key, value)
		}
	}
}

func (c *AppConfig) FlattenSources() []SourceDef {
	var result []SourceDef
	for _, sources := range c.NewsSources {
		result = append(result, sources...)
	}
	return result
}
