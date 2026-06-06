package main

import (
	"fmt"
	"io"
	"net/http"
	"net/url"
	"regexp"
	"strings"
)

const (
	QD_BASE  = "https://www.qidian.com"
	QD_BOOK  = "https://book.qidian.com"
	QD_READ  = "https://read.qidian.com"
)

// ── 起点中文网 ──

type QidianClient struct {
	client *http.Client
}

func NewQidianClient() *QidianClient {
	return &QidianClient{client: &http.Client{Timeout: 30e9}}
}

func (c *QidianClient) get(u string) (*http.Response, error) {
	req, _ := http.NewRequest("GET", u, nil)
	req.Header.Set("User-Agent", UA)
	return c.client.Do(req)
}

// SearchQidian 搜索起点书籍
func (c *QidianClient) SearchQidian(keyword string) []BookResult {
	searchURL := fmt.Sprintf("%s/search?kw=%s", QD_BASE, url.QueryEscape(keyword))
	resp, err := c.get(searchURL)
	if err != nil {
		return nil
	}
	body, _ := io.ReadAll(io.LimitReader(resp.Body, 1<<20))
	html := string(body)

	// 起点搜索结果: <li data-bid="book_id"> ... <h3><a>title</a></h3> ...
	itemRe := regexp.MustCompile(`<li[^>]*data-bid="(\d+)"[^>]*>.*?<h3[^>]*><a[^>]*>(.*?)</a>.*?<a[^>]*class="[^"]*author[^"]*"[^>]*>(.*?)</a>`)
	matches := itemRe.FindAllStringSubmatch(html, -1)

	var results []BookResult
	for _, m := range matches {
		results = append(results, BookResult{
			BookID: m[1],
			Title:  strings.TrimSpace(m[2]),
			Author: strings.TrimSpace(m[3]),
			Source: "qidian",
		})
	}
	return results
}

// GetQidianInfo 获取起点书籍详情 + 免费章节目录
func (c *QidianClient) GetQidianInfo(bookID string) (*BookInfo, error) {
	infoURL := fmt.Sprintf("%s/info/%s/", QD_BOOK, bookID)
	resp, err := c.get(infoURL)
	if err != nil {
		return nil, err
	}
	body, _ := io.ReadAll(io.LimitReader(resp.Body, 1<<20))
	html := string(body)

	// 提取 __INITIAL_STATE__ (起点也用这个)
	state := extractInitState(html)

	title := ""
	author := ""
	status := "连载中"
	desc := ""
	cover := ""

	if state != nil {
		if bookInfo, ok := state["book"].(map[string]interface{}); ok {
			if v, ok := bookInfo["bookName"].(string); ok { title = v }
			if v, ok := bookInfo["authorName"].(string); ok { author = v }
			if v, ok := bookInfo["bookStatus"].(string); ok {
				if v == "完本" || v == "完结" { status = "完结" }
			}
			if v, ok := bookInfo["desc"].(string); ok { desc = v }
			if v, ok := bookInfo["bookCover"].(string); ok { cover = v }
		}
	}

	// 从 HTML 提取章节列表
	var chapters []Chapter
	chRe := regexp.MustCompile(`<a[^>]*href="//read\.qidian\.com/chapter/[^"]*/(\d+)/?"[^>]*data-cid="[^"]*"[^>]*>([^<]+)</a>`)
	for _, m := range chRe.FindAllStringSubmatch(html, -1) {
		chapters = append(chapters, Chapter{
			ItemID: m[1],
			Title:  strings.TrimSpace(m[2]),
		})
	}

	if title == "" {
		// 回退：HTML 标题
		titleM := regexp.MustCompile(`<title>(.*?)</title>`).FindStringSubmatch(html)
		if titleM != nil { title = strings.TrimSpace(titleM[1]) }
	}

	return &BookInfo{
		Found:        title != "",
		Source:       "qidian",
		BookID:       bookID,
		Title:        title,
		Author:       author,
		Status:       status,
		Description:  desc,
		ChapterCount: len(chapters),
		Chapters:     chapters,
		Cover:        cover,
	}, nil
}

// FetchQidianChapter 获取单章内容（免费章节，无加密）
func (c *QidianClient) FetchQidianChapter(bookID, chapterID string) string {
	chURL := fmt.Sprintf("%s/chapter/%s/%s/", QD_READ, bookID, chapterID)
	resp, err := c.get(chURL)
	if err != nil {
		return ""
	}
	body, _ := io.ReadAll(io.LimitReader(resp.Body, 1<<20))
	html := string(body)

	// 提取章节正文：<div class="read-content j_readContent">
	contentRe := regexp.MustCompile(`<div[^>]*class="[^"]*read-content[^"]*"[^>]*>(.*?)</div>`)
	m := contentRe.FindStringSubmatch(html)
	if m == nil { return "" }

	content := m[1]
	// 清理 HTML 标签
	content = regexp.MustCompile(`<[^>]+>`).ReplaceAllString(content, "")
	content = strings.ReplaceAll(content, "&nbsp;", " ")
	content = strings.ReplaceAll(content, "&lt;", "<")
	content = strings.ReplaceAll(content, "&gt;", ">")
	content = strings.ReplaceAll(content, "&amp;", "&")
	content = regexp.MustCompile(`\n{3,}`).ReplaceAllString(content, "\n\n")

	if cnCount(content) >= 100 {
		return strings.TrimSpace(content)
	}
	return ""
}

// ── 起点下载 ──

// DownloadQidianHybrid 混合下载：免费章节直抓 + VIP聚合站回退
func (c *QidianClient) DownloadQidianHybrid(bookID, outputDir string) (*DownloadResult, error) {
	mkdir(outputDir)

	info, err := c.GetQidianInfo(bookID)
	if err != nil || !info.Found {
		return &DownloadResult{Success: false, Error: "未找到书籍"}, nil
	}
	if len(info.Chapters) == 0 {
		return &DownloadResult{Success: false, Error: "无章节数据"}, nil
	}

	safeTitle := safeFilename(info.Title)
	outputPath := fmt.Sprintf("%s/%s.txt", outputDir, safeTitle)
	startTime := now()

	// 聚合站备用
	agg := NewSimpleAggregator()
	// 用书名在聚合站搜一下，获取 bookID（用于章节回退）
	aggBookID := ""
	if aggResults := agg.Search(info.Title); len(aggResults) > 0 {
		aggBookID = aggResults[0].BookID
	}

	var results []ChapterResult
	downloaded, totalChars := 0, 0
	freeCount, vipCount := 0, 0
	var failedItems []string

	for i, ch := range info.Chapters {
		// ① 先试起点直连（免费章节）
		content := c.FetchQidianChapter(bookID, ch.ItemID)
		if content != "" {
			results = append(results, ChapterResult{Title: ch.Title, Content: content})
			downloaded++
			totalChars += cnCount(content)
			freeCount++
		} else if aggBookID != "" {
			// ② 起点失败 → 聚合站回退（VIP章节）
			content = agg.FetchChapter(aggBookID, ch.ItemID)
			if content != "" {
				results = append(results, ChapterResult{Title: ch.Title, Content: content})
				downloaded++
				totalChars += cnCount(content)
				vipCount++
			} else {
				failedItems = append(failedItems, ch.Title)
			}
		} else {
			failedItems = append(failedItems, ch.Title)
		}
		if i%5 == 0 { msSleep(300) }
	}

	writeResults(outputPath, info.Title, info.Author, results, totalChars)

	method := fmt.Sprintf("qidian_hybrid(free=%d,vip=%d)", freeCount, vipCount)
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
		Method:        method,
	}, nil
}
