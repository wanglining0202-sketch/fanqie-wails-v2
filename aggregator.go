package main

import (
	"fmt"
	"io"
	"net/http"
	"net/url"
	"regexp"
	"strings"
	"time"
)

// ── 聚合站下载（全本，含VIP内容）──
// 69shu.com / biquge / 等聚合站有起点+飞卢全本

const (
	AGG_BASE = "https://www.69shu.com"
)

type AggregatorClient struct {
	client *http.Client
}

func NewAggregatorClient() *AggregatorClient {
	return &AggregatorClient{client: &http.Client{Timeout: 30 * time.Second}}
}

func (c *AggregatorClient) get(u string) (string, error) {
	req, _ := http.NewRequest("GET", u, nil)
	req.Header.Set("User-Agent", UA)
	resp, err := c.client.Do(req)
	if err != nil { return "", err }
	defer resp.Body.Close()
	body, _ := io.ReadAll(io.LimitReader(resp.Body, 2<<20))
	return string(body), nil
}

// SearchAgg 聚合站搜索
func (c *AggregatorClient) SearchAgg(keyword string) []BookResult {
	searchURL := fmt.Sprintf("%s/search?key=%s", AGG_BASE, url.QueryEscape(keyword))
	html, err := c.get(searchURL)
	if err != nil { return nil }

	// 69shu 搜索结果: <li><a href="/book/12345/"><h3>书名</h3></a><p>作者</p>
	itemRe := regexp.MustCompile(`<li[^>]*>\s*<a[^>]*href="(/book/\d+/)"[^>]*>\s*<h3[^>]*>(.*?)</h3>\s*</a>\s*<p[^>]*>(.*?)</p>`)
	matches := itemRe.FindAllStringSubmatch(html, -1)

	var results []BookResult
	for _, m := range matches {
		bookID := strings.TrimPrefix(strings.TrimSuffix(m[1], "/"), "/book/")
		results = append(results, BookResult{
			BookID: bookID,
			Title:  strings.TrimSpace(m[2]),
			Author: strings.TrimSpace(m[3]),
			Source: "69shu",
		})
	}
	return results
}

// GetAggInfo 聚合站书籍详情（全本章节列表）
func (c *AggregatorClient) GetAggInfo(bookID string) (*BookInfo, error) {
	// 69shu 书籍目录页: /book/12345/
	infoURL := fmt.Sprintf("%s/book/%s/", AGG_BASE, bookID)
	html, err := c.get(infoURL)
	if err != nil { return nil, err }

	titleRe := regexp.MustCompile(`<h1[^>]*>(.*?)</h1>`)
	authorRe := regexp.MustCompile(`作者[：:]\s*(.*?)<`)
	descRe := regexp.MustCompile(`<div[^>]*class="[^"]*intro[^"]*"[^>]*>(.*?)</div>`)

	title, author, desc := "", "", ""
	if m := titleRe.FindStringSubmatch(html); m != nil { title = strings.TrimSpace(m[1]) }
	if m := authorRe.FindStringSubmatch(html); m != nil { author = strings.TrimSpace(m[1]) }
	if m := descRe.FindStringSubmatch(html); m != nil { desc = stripTags(strings.TrimSpace(m[1])) }

	// 章节列表: <li><a href="/txt/12345/章节id.html">章节名</a></li>
	var chapters []Chapter
	chRe := regexp.MustCompile(`<a[^>]*href="(/txt/\d+/(\d+)\.html)"[^>]*>(.*?)</a>`)
	for _, m := range chRe.FindAllStringSubmatch(html, -1) {
		chapters = append(chapters, Chapter{
			ItemID: m[2],
			Title:  strings.TrimSpace(m[3]),
		})
	}

	return &BookInfo{
		Found:        title != "",
		Source:       "69shu",
		BookID:       bookID,
		Title:        title,
		Author:       author,
		Description:  desc,
		ChapterCount: len(chapters),
		Chapters:     chapters,
	}, nil
}

// FetchAggChapter 聚合站章节内容（全本无加密）
func (c *AggregatorClient) FetchAggChapter(bookID, chapterID string) string {
	chURL := fmt.Sprintf("%s/txt/%s/%s.html", AGG_BASE, bookID, chapterID)
	html, err := c.get(chURL)
	if err != nil { return "" }

	// 正文在 <div class="txtnav"> 中
	contentRe := regexp.MustCompile(`<div[^>]*class="[^"]*txtnav[^"]*"[^>]*>(.*?)</div>`)
	m := contentRe.FindStringSubmatch(html)
	if m == nil { return "" }

	content := m[1]
	content = stripTags(content)
	content = strings.ReplaceAll(content, "&nbsp;", " ")
	content = strings.ReplaceAll(content, "&lt;", "<")
	content = strings.ReplaceAll(content, "&gt;", ">")
	content = strings.ReplaceAll(content, "&amp;", "&")
	content = strings.ReplaceAll(content, "69书吧", "")
	content = strings.ReplaceAll(content, "www.69shu.com", "")
	content = regexp.MustCompile(`\n{3,}`).ReplaceAllString(content, "\n\n")

	if cnCount(content) >= 100 {
		return strings.TrimSpace(content)
	}
	return ""
}

// DownloadAgg 聚合站全本下载
func (c *AggregatorClient) DownloadAgg(bookID, outputDir string) (*DownloadResult, error) {
	mkdir(outputDir)

	info, err := c.GetAggInfo(bookID)
	if err != nil || !info.Found { return &DownloadResult{Success: false, Error: "未找到书籍"}, nil }

	safeTitle := safeFilename(info.Title)
	outputPath := fmt.Sprintf("%s/%s.txt", outputDir, safeTitle)
	startTime := now()

	var results []ChapterResult
	downloaded, totalChars := 0, 0
	var failedItems []string

	for i, ch := range info.Chapters {
		content := c.FetchAggChapter(bookID, ch.ItemID)
		if content != "" {
			results = append(results, ChapterResult{Title: ch.Title, Content: content})
			downloaded++
			totalChars += cnCount(content)
		} else {
			failedItems = append(failedItems, ch.Title)
		}
		if i%10 == 0 { msSleep(300) }
	}

	writeResults(outputPath, info.Title, info.Author, results, totalChars)

	return &DownloadResult{
		Success:       downloaded > 0,
		Title:         info.Title,
		Path:          outputPath,
		CNChars:       totalChars,
		TotalChapters: len(info.Chapters),
		Downloaded:    downloaded,
		FailedCount:   len(failedItems),
		Failed:        failedItems,
		ElapsedSec:    since(startTime),
		Method:        "aggregator_69shu",
	}, nil
}
