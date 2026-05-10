# 日知录 — Go 版架构文档

## 项目概览

基于 RSS + LLM 的自动化新闻分析日报系统。Go 版对比 Python 版实现 **2.85x 性能提升**（175s → 61s），核心优化来自 LLM 分析的并行化。

| 维度 | Python | Go |
|------|--------|-----|
| 部署 | Python + 7 个 pip 包 | 单个 `news_agent.exe`（21MB） |
| 内存 | ~100MB | ~30MB |
| 抓取 | ThreadPoolExecutor(8) | errgroup × 8 |
| LLM 分析 | 串行 × 30 次 | errgroup × 5 并发 |
| 总耗时 | ~175s | **~61s** |

---

## 目录结构

```
news_agent_go/
├── cmd/
│   └── news_agent/
│       └── main.go                  # 程序入口，8 步串联
│
├── internal/
│   ├── article/
│   │   └── article.go               # Article / Analysis / Editorial 结构体
│   │
│   ├── config/
│   │   └── config.go                # YAML 解析 + .env 加载 + ${ENV} 变量替换
│   │
│   ├── fetcher/
│   │   └── rss.go                   # RSS 抓取 + 正文爬取（errgroup 并行）
│   │
│   ├── filter/
│   │   ├── dedup.go                 # URL 历史去重 + 标题相似度去重
│   │   └── relevance.go             # 时效过滤 + 关键词打分 + 分类均衡
│   │
│   ├── cluster/
│   │   └── cluster.go               # 并查集标题聚类 + 内容关键词辅助判断
│   │
│   ├── analyzer/
│   │   └── analyze.go               # DeepSeek HTTP 客户端 + 并行分析 + 全局综评
│   │
│   ├── storage/
│   │   └── sqlite.go                # SQLite 持久化（modernc.org/sqlite，纯 Go）
│   │
│   ├── output/
│   │   ├── output.go                # HTML 模板渲染 + 邮件发送 + 农历
│   │   └── daily.html               # Go template 日知录模板（古籍卷轴风）
│   │
│   └── textutil/
│       └── textutil.go              # 字符串归一化 + LCS 相似度
│
├── config.yaml                      # 新闻源 / 关键词 / LLM 参数
├── .env                             # API Key / 邮箱配置（git ignore）
├── .gitignore
├── go.mod / go.sum                  # Go 模块依赖
└── news_agent.exe                   # 编译产物
```

---

## 数据流全景

```
                       ┌──────────────────┐
                       │   config.yaml    │
                       │   .env           │
                       └────────┬─────────┘
                                ▼
┌────────────────────────────────────────────────────────────────────┐
│  main.go (8-stage pipeline)                                       │
│                                                                    │
│  [1] Fetch ──► [2] Dedup ──► [3] Recency ──► [4] Keywords        │
│      │               │              │               │              │
│      ▼               ▼              ▼               ▼              │
│   errgroup     URL + Title    5-day cutoff    Regex scoring        │
│    ×8           history                       category balanced    │
│                                                                   │
│  [5] Cluster ──► [6] Analyze ──► [7] Editorial ──► [8] Render    │
│       │               │               │                │           │
│       ▼               ▼               ▼                ▼           │
│   Union-Find     errgroup ×5     2 × LLM calls   html/template    │
│   title+content  DeepSeek API     morning +       + SMTP email     │
│                                  evening essay                     │
└────────────────────────────────────────────────────────────────────┘
```

---

## 核心数据结构

```go
// internal/article/article.go

type Article struct {
    ID          string    // MD5(URL) 前 12 位 hex
    Title       string
    URL         string
    Source      string    // 新闻源名称
    Category    string    // "科技" / "财经" / "时事"
    Content     string    // RSS 摘要或爬取全文
    PublishedAt string    // "2006-01-02 15:04:05"
    FetchedAt   string
    Score       float64   // 关键词评分
    ClusterID   int
}

type Analysis struct {
    ClusterID    int
    Title        string
    Category     string
    Text         string      // LLM 分析输出
    ArticleCount int
    Sources      []SourceRef
    PublishedAt  string
}

type SourceRef struct {
    Name string
    URL  string
}

type Editorial struct {
    MorningBrief string   // "今日三事" 开篇
    EveningEssay string   // "今日格局/暗线/一言" 总评
}
```

---

## 模块详解

### 1. Fetcher（抓取层）— `internal/fetcher/rss.go`

| 职责 | 实现 |
|------|------|
| RSS 解析 | `github.com/mmcdole/gofeed`，每个源取最多 15 条 |
| 正文爬取 | `github.com/PuerkitoBio/goquery`，body 内容 < 200 字时爬全文（上限 8000 字） |
| 并发模型 | `errgroup` + `SetLimit(8)`，10 源并行抓取 |
| 超时控制 | `http.Client{Timeout: 20s}`，5MB body 限制 |
| 日期解析 | 优先 `PublishedParsed`，回退 `UpdatedParsed`，兜底 `time.Now()` |

**关键函数签名：**

```go
func FetchAll(sources []config.SourceDef) ([]article.Article, error)
func fetchSource(src config.SourceDef) []article.Article
func fetchContent(url string) string
```

---

### 2. Filter（过滤层）

#### `dedup.go`

```go
func DedupByURL(articles []article.Article, store *storage.Store) []article.Article
  // 查询 SQLite 近 7 天 URL，内存去重

func DedupSimilarTitles(articles []article.Article, threshold float64) []article.Article
  // LCS 相似度 ≥ 0.65 的标题视为重复，保留更长内容
```

#### `relevance.go`

```go
func FilterByRecency(articles []article.Article, maxDays int) []article.Article
  // 发布日期在 maxDays 内的保留，无法解析的兜底保留

func FilterByKeywords(articles, highKw, exclKw, maxArticles, minPerCat) []article.Article
  // 正则打分（正文匹配 +2，标题匹配 +3，排除词 -3）
  // Round-robin 按分类均衡选取，每类最少 minPerCat 条
```

**关键词评分算法：**

```
score = min(bodyMatches × 2, 10)        // 正文匹配
      + titleMatches × 3                 // 标题匹配（加分 3/词）
      - excludeMatches × 3               // 排除词（扣分 3/词）
score = max(score, 0)                    // 最低 0
```

---

### 3. Cluster（聚类层）— `internal/cluster/cluster.go`

| 技术 | 说明 |
|------|------|
| 算法 | 并查集 (Union-Find) + 路径压缩 |
| 主信号 | 标题 LCS 相似度 ≥ 0.4 直接合并 |
| 辅助信号 | 标题 ≥ 0.3 时，内容关键词 Jaccard ≥ 0.25 也合并 |
| 关键词提取 | 前 2000 字词频 Top 8（≥ 3 字或 ASCII 词） |

```go
func Cluster(articles []article.Article, threshold float64) [][]article.Article
```

**复杂度：** O(n²) 两两比较，n ≤ 30 可忽略。

---

### 4. Analyzer（分析层）— `internal/analyzer/analyze.go`

#### DeepSeek 客户端

自研轻量 HTTP 客户端（不依赖 `go-openai` SDK）：

```go
type Client struct {
    hc     *http.Client
    apiKey string
    base   string   // https://api.deepseek.com
    model  string   // deepseek-chat
}

func (c *Client) chat(system, user string) (string, error)
  // POST https://api.deepseek.com/v1/chat/completions
  // OpenAI 兼容 JSON 协议
```

#### 分析 Prompt 设计

| 层级 | Prompt | 输出格式 |
|------|--------|----------|
| 单篇分析 | 四维拆解 | ■ 事实 · ■ 动因 · ■ 影响 · ■ 趋势 |
| 聚类分析 | 多源对比 | ■ 事实 · ■ 分歧 · ■ 语境 · ■ 走势 |
| 全局综评 | 大局观 | 【今日格局】·【暗线】·【一言】 |
| 早报摘要 | Top 3 | 一、… 二、… 三、… |

#### System Prompt

```
你是一位资深新闻分析师，在路透社和财新有十五年工作经验。
要求：每个部分不超过80字，言之有物；不用"据悉"等废话；
给出判断，不只是复述事实；有数据则点出关键数字。
```

#### 并行执行

```go
func AnalyzeAll(c *Client, clusters [][]article.Article) []article.Analysis
  // errgroup + SetLimit(5)，30 次调用 5 路并发 = 6 轮完成
  // 耗时从 110s（串行）降至 ~26s

func GenerateEditorial(c *Client, analyses []article.Analysis) article.Editorial
  // 2 次 LLM 调用：早报摘要 + 晚间综评
```

---

### 5. Storage（存储层）— `internal/storage/sqlite.go`

| 特性 | 实现 |
|------|------|
| 驱动 | `modernc.org/sqlite` — 纯 Go，无 CGO，跨平台 |
| 日志模式 | WAL（Write-Ahead Logging） |
| 表 | `news`（URL 唯一索引）+ `daily_reports`（日期唯一） |
| 索引 | `url`, `published_at`, `fetched_at` |

```go
type Store struct { db *sql.DB }

func New(dbPath string) (*Store, error)           // 初始化 + 建表
func (s *Store) SaveNews(...) (bool, error)        // INSERT OR IGNORE
func (s *Store) RecentURLs(days int) (map[string]bool, error)  // 用于去重
func (s *Store) SaveReport(date, path string, n int) error    // INSERT OR REPLACE
```

---

### 6. Output（输出层）— `internal/output/`

#### HTML 渲染

```go
func RenderReport(analyses []article.Analysis, editorial article.Editorial, dir string) (string, error)
```

- 模板引擎：`html/template`（Go 标准库）
- 模板文件：`//go:embed daily.html` 嵌入二进制
- 支持变量：`.Date`, `.LunarDate`, `.Categories`, `.MorningBrief`, `.EveningEssay`
- 农历：天干地支 + 生肖近似算法

#### 邮件发送

```go
func SendEmail(htmlPath string) error
```

- 协议：SMTP（`net/smtp`）
- 多收件人：逗号分隔 `EMAIL_RECEIVER=a@qq.com,b@outlook.com`
- 编码：Subject 用 Base64 支持中文
- 端口：587 + STARTTLS

---

### 7. Config（配置层）— `internal/config/config.go`

```go
type AppConfig struct {
    RSSHub      RSSHubConfig
    NewsSources map[string][]SourceDef    // 按分类名分组
    Preferences Preferences                // 关键词
    LLM         LLMConfig                  // API 参数
    Summary     SummaryConfig              // 过滤器参数
    Output      OutputConfig               // 输出路径
    Schedule    ScheduleConfig             // 调度时间（保留）
}

func Load(path string) (*AppConfig, error)   // YAML 解析 + 默认值
func LoadDotenv(path string)                  // .env → os.Setenv
func (c *AppConfig) FlattenSources() []SourceDef
```

支持的配置引用：`{rsshub}` 变量在 `Load()` 中自动替换为 `rsshub.base_url`。

---

### 8. TextUtil（工具层）— `internal/textutil/textutil.go`

```go
func Normalize(text string) string
  // 去除非 CJK/字母/数字字符 → 小写

func HeadlineSimilarity(t1, t2 string) float64
  // LCS / 平均长度 → [0, 1] 相似度
  // 等价于 Python difflib.SequenceMatcher.ratio()
```

---

## 并发模型

```
                        Python                    Go
RSS 抓取 (10源)    ThreadPoolExecutor(8)     errgroup + SetLimit(8)
正文爬取 (N篇)     ThreadPoolExecutor(8)     errgroup + SetLimit(8)
LLM 分析 (30篇)    串行 for loop             errgroup + SetLimit(5)
LLM 综评 (2次)     串行                      顺序调用
邮件                主线程                    主线程
```

Go 的 goroutine 开销极小（~2KB/个），使用 channel 进行结果收集，无需管理线程池生命周期。

---

## 依赖管理

```go
// go.mod
require (
    gopkg.in/yaml.v3 v3.0.1            // YAML 配置解析
    modernc.org/sqlite v1.50.0          // 纯 Go SQLite
    github.com/mmcdole/gofeed v1.3.0    // RSS/Atom 解析
    github.com/PuerkitoBio/goquery v1.12.0  // HTML 解析（BeautifulSoup 等价）
    golang.org/x/sync v0.20.0           // errgroup 并发控制
    golang.org/x/net v0.52.0            // 网络 + 编码
)
```

**刻意不引入的依赖：**
- `go-openai` — DeepSeek 仅需 POST JSON，自研客户端 80 行
- `gomail` — `net/smtp` 足够
- 农历库 — 天干地支近似，30 行

---

## 配置说明

### `.env`（敏感信息，不提交 Git）

```ini
DEEPSEEK_API_KEY=sk-xxx           # DeepSeek API 密钥
EMAIL_SENDER=xxx@qq.com           # QQ 邮箱
EMAIL_PASSWORD=xxx                # SMTP 授权码
EMAIL_RECEIVER=a@qq.com,b@xx.com  # 逗号分隔多收件人
```

### `config.yaml`（可调参数）

```yaml
summary:
  max_news_per_day: 30             # 日报最大条数
  cluster_threshold: 0.4           # 聚类相似度阈值
  min_news_per_category: 3         # 每类最少保障

llm:
  model: deepseek-chat             # 模型
  temperature: 0.4                  # 创造性

output:
  email_enabled: true              # 邮件开关
  report_dir: output/daily_reports # 日报保存路径
```

---

## 构建与运行

```powershell
# 构建
cd news_agent_go
go build -o news_agent.exe ./cmd/news_agent/

# 运行
.\news_agent.exe

# 输出
output/daily_reports/daily_report_2026-05-08.html
```

**单文件部署：** 将 `news_agent.exe` + `config.yaml` + `.env` 复制到任意 Windows 机器即可运行，无需安装 Go 或 Python 运行时。

---

## 定时任务

```powershell
# 安装（管理员权限）
schtasks /Create /SC DAILY /TN "NewsAgentGo" `
  /TR "E:\novel\news_agent_go\news_agent.exe" /ST 08:00 /F

# 查看
schtasks /Query /TN "NewsAgentGo"

# 手动触发
schtasks /Run /TN "NewsAgentGo"
```
