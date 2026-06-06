package main

import (
	"fmt"
	"io"
	"net/http"
	"net/url"
	"regexp"
	"strings"
)

// ── 起点中文网（移动端）──

type QidianClient struct {
	client *http.Client
}

func NewQidianClient() *QidianClient {
	return &QidianClient{client: &http.Client{Timeout: 30e9}}
}

func (c *QidianClient) get(u string) (string, error) {
	req, _ := http.NewRequest("GET", u, nil)
	req.Header.Set("User-Agent", "Mozilla/5.0 (Linux; Android 12; Pixel 6) AppleWebKit/537.36 (KHTML, like Gecko) Chrome/120.0.0.0 Mobile Safari/537.36")
	resp, err := c.client.Do(req)
	if err != nil { return "", err }
	defer resp.Body.Close()
	body, _ := io.ReadAll(io.LimitReader(resp.Body, 2<<20))
	return string(body), nil
}

// SearchQidian 搜索起点书籍（移动端）
func (c *QidianClient) SearchQidian(keyword string) []BookResult {
	// 移动端搜索
	searchURL := fmt.Sprintf("https://m.qidian.com/soushu/%s.html?pageNum=1", url.QueryEscape(keyword))
	html, err := c.get(searchURL)
	if err != nil { return nil }

	// 提取: <a href="/book/数字/">书名</a>
	itemRe := regexp.MustCompile(`<a[^>]*href="/book/(\d+)/"[^>]*>\s*<h3[^>]*>([^<]+)</h3>`)
	matches := itemRe.FindAllStringSubmatch(html, -1)
	if len(matches) == 0 {
		// 回退: 如果直接是 book ID，跳过搜索
		if regexp.MustCompile(`^\d{7,}$`).MatchString(keyword) {
			return []BookResult{{BookID: keyword, Title: "ID:" + keyword, Source: "qidian_mobile"}}
		}
	}

	var results []BookResult
	for _, m := range matches {
		results = append(results, BookResult{
			BookID: m[1], Title: strings.TrimSpace(m[2]),
			Source: "qidian_mobile",
		})
	}
	return results
}

// GetQidianInfo 获取起点书籍详情（移动端）
func (c *QidianClient) GetQidianInfo(bookID string) (*BookInfo, error) {
	html, err := c.get("https://m.qidian.com/book/" + bookID + "/")
	if err != nil { return nil, err }

	title, author := "", ""
	if m := regexp.MustCompile(`<h1[^>]*>([^<]+)</h1>`).FindStringSubmatch(html); m != nil { title = strings.TrimSpace(m[1]) }
	if m := regexp.MustCompile(`作者[：:]\s*<a[^>]*>([^<]+)</a>`).FindStringSubmatch(html); m != nil { author = strings.TrimSpace(m[1]) }

	// 章节列表: /chapter/bookID/chapterID/
	var chapters []Chapter
	chRe := regexp.MustCompile(`href="/chapter/(\d+)/(\d+)/"[^>]*>([^<]+)</a>`)
	for _, m := range chRe.FindAllStringSubmatch(html, -1) {
		chapters = append(chapters, Chapter{
			ItemID: m[2], Title: strings.TrimSpace(m[3]),
		})
	}
	// 如果主页章节少，尝试目录页
	if len(chapters) < 10 {
		catHTML, _ := c.get("https://m.qidian.com/book/" + bookID + "/catalog/")
		for _, m := range chRe.FindAllStringSubmatch(catHTML, -1) {
			chapters = append(chapters, Chapter{
				ItemID: m[2], Title: strings.TrimSpace(m[3]),
			})
		}
	}

	return &BookInfo{
		Found: title != "", Source: "qidian_mobile", BookID: bookID,
		Title: title, Author: author, ChapterCount: len(chapters), Chapters: chapters,
	}, nil
}

// FetchQidianChapter 获取单章内容（移动端）
func (c *QidianClient) FetchQidianChapter(bookID, chapterID string) string {
	html, err := c.get(fmt.Sprintf("https://m.qidian.com/chapter/%s/%s/", bookID, chapterID))
	if err != nil { return "" }

	// 正文在 <div class="content ..."> 中
	contentRe := regexp.MustCompile(`<div[^>]*class="[^"]*content[^"]*"[^>]*>(.*?)</div>`)
	m := contentRe.FindStringSubmatch(html)
	if m == nil { return "" }

	content := m[1]
	content = regexp.MustCompile(`<p[^>]*>`).ReplaceAllString(content, "")
	content = strings.ReplaceAll(content, "</p>", "\n")
	content = stripTags(content)
	content = strings.ReplaceAll(content, "&nbsp;", " ")
	content = regexp.MustCompile(`\n{3,}`).ReplaceAllString(content, "\n\n")

	if cnCount(content) >= 100 { return strings.TrimSpace(content) }
	return ""
}

// DownloadQidianHybrid 混合下载
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

	agg := NewSimpleAggregator()
	aggBookID := ""
	if aggResults := agg.Search(info.Title); len(aggResults) > 0 {
		aggBookID = aggResults[0].BookID
	}

	var results []ChapterResult
	downloaded, totalChars := 0, 0
	freeCount, vipCount := 0, 0
	var failedItems []string

	for i, ch := range info.Chapters {
		content := c.FetchQidianChapter(bookID, ch.ItemID)
		if content != "" {
			results = append(results, ChapterResult{Title: ch.Title, Content: content})
			downloaded++; totalChars += cnCount(content); freeCount++
		} else if aggBookID != "" {
			content = agg.FetchChapter(aggBookID, ch.ItemID)
			if content != "" {
				results = append(results, ChapterResult{Title: ch.Title, Content: content})
				downloaded++; totalChars += cnCount(content); vipCount++
			} else { failedItems = append(failedItems, ch.Title) }
		} else { failedItems = append(failedItems, ch.Title) }
		if i%5 == 0 { msSleep(300) }
	}

	writeResults(outputPath, info.Title, info.Author, results, totalChars)
	return &DownloadResult{
		Success: downloaded > 0, Title: info.Title, Path: outputPath,
		CNChars: totalChars, TotalChapters: len(info.Chapters),
		Downloaded: downloaded, FailedCount: len(failedItems),
		Failed: failedItems, ElapsedSec: since(startTime),
		Method: fmt.Sprintf("qidian_hybrid(free=%d,vip=%d)", freeCount, vipCount),
	}, nil
}
