package main

import (
	"fmt"
	"io"
	"net/http"
	"net/url"
	"regexp"
	"strings"

	"golang.org/x/text/encoding/simplifiedchinese"
	"golang.org/x/text/transform"
)

const FL_BASE = "https://b.faloo.com"

// ── 飞卢小说 ──

type FeiluClient struct {
	client *http.Client
}

func NewFeiluClient() *FeiluClient {
	return &FeiluClient{client: &http.Client{Timeout: 30e9}}
}

func (c *FeiluClient) get(u string) (string, error) {
	req, _ := http.NewRequest("GET", u, nil)
	req.Header.Set("User-Agent", UA)
	resp, err := c.client.Do(req)
	if err != nil {
		return "", err
	}
	defer resp.Body.Close()
	// 飞卢用 GBK 编码
	reader := transform.NewReader(resp.Body, simplifiedchinese.GBK.NewDecoder())
	body, _ := io.ReadAll(io.LimitReader(reader, 2<<20))
	return string(body), nil
}

// SearchFeilu 搜索飞卢书籍
func (c *FeiluClient) SearchFeilu(keyword string) []BookResult {
	searchURL := fmt.Sprintf("%s/search.html?k=%s", FL_BASE, url.QueryEscape(keyword))
	html, err := c.get(searchURL)
	if err != nil {
		return nil
	}

	// 搜索结果: <a href="/123456.html" target="_blank">书名</a>
	linkRe := regexp.MustCompile(`<a[^>]*href="(/\d+\.html)"[^>]*target="_blank"[^>]*>(.*?)</a>`)
	authorRe := regexp.MustCompile(`作者[：:]([^<]+)<`)

	matches := linkRe.FindAllStringSubmatch(html, -1)
	var results []BookResult
	for _, m := range matches {
		bookID := strings.TrimPrefix(strings.TrimSuffix(m[1], ".html"), "/")
		title := strings.TrimSpace(m[2])
		if title == "" || bookID == "" { continue }

		// 尝试提取作者
		author := ""
		authorSection := html[strings.Index(html, m[0]):]
		if am := authorRe.FindStringSubmatch(authorSection); am != nil {
			author = strings.TrimSpace(am[1])
		}

		results = append(results, BookResult{
			BookID: bookID,
			Title:  title,
			Author: author,
			Source: "feilu",
		})
	}
	return results
}

// GetFeiluInfo 获取飞卢书籍详情 + 章节目录
func (c *FeiluClient) GetFeiluInfo(bookID string) (*BookInfo, error) {
	infoURL := fmt.Sprintf("%s/%s.html", FL_BASE, bookID)
	html, err := c.get(infoURL)
	if err != nil {
		return nil, err
	}

	// 书名
	titleRe := regexp.MustCompile(`<h1[^>]*>(.*?)</h1>`)
	title := ""
	if m := titleRe.FindStringSubmatch(html); m != nil {
		title = strings.TrimSpace(m[1])
	}

	// 作者
	authorRe := regexp.MustCompile(`作者[：:]\s*(.*?)[<\n]`)
	author := ""
	if m := authorRe.FindStringSubmatch(html); m != nil {
		author = strings.TrimSpace(m[1])
	}

	// 简介
	descRe := regexp.MustCompile(`<div[^>]*id="BookInfo"[^>]*>([\s\S]*?)</div>`)
	desc := ""
	if m := descRe.FindStringSubmatch(html); m != nil {
		desc = stripTags(strings.TrimSpace(m[1]))
		if len(desc) > 500 { desc = desc[:500] }
	}

	// 章节目录
	var chapters []Chapter
	chRe := regexp.MustCompile(`<a[^>]*href="/(\d+)\.html"[^>]*title="([^"]*)"`)
	for _, m := range chRe.FindAllStringSubmatch(html, -1) {
		chapters = append(chapters, Chapter{
			ItemID: m[1],
			Title:  strings.TrimSpace(m[2]),
		})
	}

	status := "连载中"
	if strings.Contains(html, "完结") || strings.Contains(html, "已完结") {
		status = "完结"
	}

	return &BookInfo{
		Found:        title != "",
		Source:       "feilu",
		BookID:       bookID,
		Title:        title,
		Author:       author,
		Status:       status,
		Description:  desc,
		ChapterCount: len(chapters),
		Chapters:     chapters,
	}, nil
}

// FetchFeiluChapter 获取单章内容
func (c *FeiluClient) FetchFeiluChapter(chapterID string) string {
	chURL := fmt.Sprintf("%s/%s.html", FL_BASE, chapterID)
	html, err := c.get(chURL)
	if err != nil {
		return ""
	}

	// 正文在 <div class="noveContent"> 中
	contentRe := regexp.MustCompile(`<div[^>]*class="[^"]*noveContent[^"]*"[^>]*>(.*?)</div>`)
	m := contentRe.FindStringSubmatch(html)
	if m == nil {
		// 尝试备用匹配
		contentRe2 := regexp.MustCompile(`<div[^>]*id="content"[^>]*>(.*?)</div>`)
		m = contentRe2.FindStringSubmatch(html)
	}
	if m == nil { return "" }

	content := m[1]
	content = stripTags(content)
	content = strings.ReplaceAll(content, "&nbsp;", " ")
	content = strings.ReplaceAll(content, "&lt;", "<")
	content = strings.ReplaceAll(content, "&gt;", ">")
	content = regexp.MustCompile(`\n{3,}`).ReplaceAllString(content, "\n\n")
	content = strings.ReplaceAll(content, "飞卢小说", "")
	content = strings.ReplaceAll(content, "飞卢小说网", "")

	if cnCount(content) >= 100 {
		return strings.TrimSpace(content)
	}
	// VIP 章节可能返回空
	return ""
}

// DownloadFeilu 下载飞卢全书
func (c *FeiluClient) DownloadFeilu(bookID, outputDir string) (*DownloadResult, error) {
	mkdir(outputDir)

	info, err := c.GetFeiluInfo(bookID)
	if err != nil || !info.Found {
		return &DownloadResult{Success: false, Error: "未找到书籍"}, nil
	}

	safeTitle := safeFilename(info.Title)
	outputPath := fmt.Sprintf("%s/%s.txt", outputDir, safeTitle)

	startTime := now()
	var results []ChapterResult
	downloaded := 0
	var failedItems []string
	totalChars := 0

	for i, ch := range info.Chapters {
		content := c.FetchFeiluChapter(ch.ItemID)
		if content != "" {
			results = append(results, ChapterResult{Title: ch.Title, Content: content})
			downloaded++
			totalChars += cnCount(content)
		} else {
			failedItems = append(failedItems, fmt.Sprintf("%s(空/VIP)", ch.Title))
		}
		if i%5 == 0 { msSleep(500) }
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
		Method:        "feilu_direct",
	}, nil
}
