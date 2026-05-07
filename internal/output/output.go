package output

import (
	"embed"
	"fmt"
	"log/slog"
	"net/smtp"
	"os"
	"path/filepath"
	"strings"
	"time"

	"html/template"

	"news_agent/internal/article"
)

//go:embed *.html
var templateFS embed.FS

var funcMap = template.FuncMap{
	"add": func(a, b int) int { return a + b },
}

type ReportData struct {
	LunarDate    string
	Date         string
	Categories   map[string][]article.Analysis
	TotalNews    int
	TotalSources int
	TotalTopics  int
	MorningBrief string
	EveningEssay string
}

func RenderReport(analyses []article.Analysis, editorial article.Editorial, reportDir string) (string, error) {
	tmpl := template.Must(template.New("daily.html").Funcs(funcMap).ParseFS(templateFS, "daily.html"))

	now := time.Now()
	date := now.Format("2006-01-02")
	gregorian := now.Format("2006年01月02日")
	lunar := lunarStr(now)

	categories := make(map[string][]article.Analysis)
	sourceSet := make(map[string]bool)
	for _, a := range analyses {
		cat := a.Category
		if cat == "" {
			cat = "综合"
		}
		categories[cat] = append(categories[cat], a)
		for _, s := range a.Sources {
			sourceSet[s.Name] = true
		}
	}

	totalNews := 0
	for _, a := range analyses {
		totalNews += a.ArticleCount
	}

	data := ReportData{
		LunarDate:    lunar,
		Date:         gregorian,
		Categories:   categories,
		TotalNews:    totalNews,
		TotalSources: len(sourceSet),
		TotalTopics:  len(analyses),
		MorningBrief: editorial.MorningBrief,
		EveningEssay: editorial.EveningEssay,
	}

	if err := os.MkdirAll(reportDir, 0755); err != nil {
		return "", fmt.Errorf("create report dir: %w", err)
	}

	filename := filepath.Join(reportDir, fmt.Sprintf("daily_report_%s.html", date))
	f, err := os.Create(filename)
	if err != nil {
		return "", fmt.Errorf("create report file: %w", err)
	}
	defer f.Close()

	if err := tmpl.Execute(f, data); err != nil {
		return "", fmt.Errorf("execute template: %w", err)
	}

	slog.Info("Report saved", "path", filename)
	return filename, nil
}

func SendEmail(htmlPath string) error {
	sender := os.Getenv("EMAIL_SENDER")
	password := os.Getenv("EMAIL_PASSWORD")
	host := os.Getenv("EMAIL_SMTP_HOST")
	receiverStr := os.Getenv("EMAIL_RECEIVER")

	if host == "" {
		host = "smtp.qq.com"
	}
	if sender == "" || password == "" || receiverStr == "" {
		slog.Warn("Email config incomplete")
		return nil
	}

	receivers := strings.Split(receiverStr, ",")
	var to []string
	for _, r := range receivers {
		r = strings.TrimSpace(r)
		if r != "" {
			to = append(to, r)
		}
	}
	if len(to) == 0 {
		return nil
	}

	htmlBody, err := os.ReadFile(htmlPath)
	if err != nil {
		return fmt.Errorf("read html: %w", err)
	}

	today := time.Now().Format("2006-01-02")
	subject := fmt.Sprintf("=?utf-8?B?%s?=", base64Encode(fmt.Sprintf("日知录 - %s", today)))

	msg := fmt.Sprintf(
		"From: %s\r\nTo: %s\r\nSubject: %s\r\nMIME-Version: 1.0\r\nContent-Type: text/html; charset=utf-8\r\n\r\n%s",
		sender, strings.Join(to, ","), subject, string(htmlBody),
	)

	auth := smtp.PlainAuth("", sender, password, strings.Split(host, ":")[0])
	addr := fmt.Sprintf("%s:587", host)
	if strings.Contains(host, ":") {
		addr = host
	}

	if err := smtp.SendMail(addr, auth, sender, to, []byte(msg)); err != nil {
		return fmt.Errorf("send mail: %w", err)
	}

	slog.Info("Email sent", "recipients", len(to))
	return nil
}

func base64Encode(s string) string {
	return base64EncodeString(s)
}

func base64EncodeString(s string) string {
	enc := "ABCDEFGHIJKLMNOPQRSTUVWXYZabcdefghijklmnopqrstuvwxyz0123456789+/"
	var result strings.Builder
	bytes := []byte(s)
	for i := 0; i < len(bytes); i += 3 {
		b0 := bytes[i]
		b1 := byte(0)
		b2 := byte(0)
		if i+1 < len(bytes) {
			b1 = bytes[i+1]
		}
		if i+2 < len(bytes) {
			b2 = bytes[i+2]
		}
		result.WriteByte(enc[b0>>2])
		result.WriteByte(enc[((b0&3)<<4)|(b1>>4)])
		if i+1 < len(bytes) {
			result.WriteByte(enc[((b1&15)<<2)|(b2>>6)])
		} else {
			result.WriteByte('=')
		}
		if i+2 < len(bytes) {
			result.WriteByte(enc[b2&63])
		} else {
			result.WriteByte('=')
		}
	}
	return result.String()
}

var (
	lunarMonths = []string{"正月", "二月", "三月", "四月", "五月", "六月", "七月", "八月", "九月", "十月", "冬月", "腊月"}
	lunarDays   = []string{"初一", "初二", "初三", "初四", "初五", "初六", "初七", "初八", "初九", "初十",
		"十一", "十二", "十三", "十四", "十五", "十六", "十七", "十八", "十九", "二十",
		"廿一", "廿二", "廿三", "廿四", "廿五", "廿六", "廿七", "廿八", "廿九", "三十"}
	tiangan = []string{"甲", "乙", "丙", "丁", "戊", "己", "庚", "辛", "壬", "癸"}
	dizhi   = []string{"子", "丑", "寅", "卯", "辰", "巳", "午", "未", "申", "酉", "戌", "亥"}
	zodiac  = []string{"鼠", "牛", "虎", "兔", "龙", "蛇", "马", "羊", "猴", "鸡", "狗", "猪"}
)

func lunarStr(dt time.Time) string {
	y := dt.Year()
	tgIdx := (y - 4) % 10
	if tgIdx < 0 {
		tgIdx += 10
	}
	dzIdx := (y - 4) % 12
	if dzIdx < 0 {
		dzIdx += 12
	}
	zIdx := (y - 4) % 12
	if zIdx < 0 {
		zIdx += 12
	}
	return fmt.Sprintf("%s%s年（%s）%d月%d日", tiangan[tgIdx], dizhi[dzIdx], zodiac[zIdx], dt.Month(), dt.Day())
}
