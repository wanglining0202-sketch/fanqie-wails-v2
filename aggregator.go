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
// 多源回退: 69shu → ibooktxt → du00

const (
	AGG1 = "https://www.69shu.com"
	AGG2 = "https://www.ibooktxt.net"
	AGG3 = "https://www.du00.co"
)

var aggSources = []string{AGG1, AGG2, AGG3}

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

// SearchAgg 多源聚合搜索
func (c *AggregatorClient) SearchAgg(keyword string) []BookResult {
	for _, baseURL := range aggSources {
		results := c.searchOne(baseURL, keyword)
		if len(results) > 0 { return results }
	}
	return nil
}

func (c *AggregatorClient) searchOne(baseURL, keyword string) []BookResult {
	searchURL := fmt.Sprintf("%s/search/?searchkey=%s", baseURL, url.QueryEscape(keyword))
	html, err := c.get(searchURL)
	if err != nil { return nil }

	// 通用匹配: <a href="/book/数字/">书名</a> 或 <a href="/编号/">书名</a>
	itemRe := regexp.MustCompile(`<a[^>]*href="(/(?:book|txt|read|html)/?\d+[^"]*)"[^>]*>\s*(?:<h3[^>]*>)?([^<]{2,60})(?:</h3>)?\s*</a>`)
	matches := itemRe.FindAllStringSubmatch(html, -1)
	
	var results []BookResult
	for _, m := range matches {
		bookID := strings.Trim(m[1], "/")
		bookID = regexp.MustCompile(`[^\d]`).ReplaceAllString(bookID, "") // 只保留数字
		title := strings.TrimSpace(m[2])
		if len(title) < 2 || bookID == "" { continue }
		results = append(results, BookResult{
			BookID: bookID,
			Title:  title,
			Source: "aggregator",
		})
		if len(results) >= 20 { break }
	}
	return results
}

// GetAggInfo 聚合站书籍详情（多源回退）
func (c *AggregatorClient) GetAggInfo(bookID string) (*BookInfo, error) {
	for _, baseURL := range aggSources {
		info, err := c.getInfoOne(baseURL, bookID)
		if err == nil && info != nil && info.Found { return info, nil }
	}
	return nil, fmt.Errorf("所有聚合站均未找到")
}

func (c *AggregatorClient) getInfoOne(baseURL, bookID string) (*BookInfo, error) {
	paths := []string{
		fmt.Sprintf("%s/book/%s/", baseURL, bookID),
		fmt.Sprintf("%s/txt/%s/", baseURL, bookID),
		fmt.Sprintf("%s/%s/", baseURL, bookID),
	}
	for _, infoURL := range paths {
		html, err := c.get(infoURL)
		if err != nil { continue }
		
		titleRe := regexp.MustCompile(`<h1[^>]*>(.*?)</h1>`)
		authorRe := regexp.MustCompile(`作者[：:]\s*(.*?)[<\n]`)
		descRe := regexp.MustCompile(`<div[^>]*class="[^"]*intro[^"]*"[^>]*>(.*?)</div>`)

		title, author, desc := "", "", ""
		if m := titleRe.FindStringSubmatch(html); m != nil { title = strings.TrimSpace(m[1]) }
		if m := authorRe.FindStringSubmatch(html); m != nil { author = strings.TrimSpace(m[1]) }
		if m := descRe.FindStringSubmatch(html); m != nil { desc = stripTags(strings.TrimSpace(m[1])) }

		// 章节列表
		var chapters []Chapter
		chRe := regexp.MustCompile(`<a[^>]*href="([^"]*/(\d+)\.html)"[^>]*>([^<]{2,80})</a>`)
		for _, m := range chRe.FindAllStringSubmatch(html, -1) {
			chapters = append(chapters, Chapter{ItemID: m[2], Title: strings.TrimSpace(m[3])})
		}
		if len(chapters) == 0 {
			chRe2 := regexp.MustCompile(`<a[^>]*href="([^"]*\d+[^"]*)"[^>]*>([^<]*(?:章|节|回|卷)[^<]*)</a>`)
			for _, m := range chRe2.FindAllStringSubmatch(html, -1) {
				id := regexp.MustCompile(`(\d+)`).FindString(m[1])
				if id != "" {
					chapters = append(chapters, Chapter{ItemID: id, Title: strings.TrimSpace(m[2])})
				}
			}
		}

		if title != "" && len(chapters) > 0 {
			return &BookInfo{
				Found: true, Source: "aggregator", BookID: bookID,
				Title: title, Author: author, Description: desc,
				ChapterCount: len(chapters), Chapters: chapters,
			}, nil
		}
	}
	return nil, fmt.Errorf("未找到")
}

// FetchAggChapter 聚合站章节内容（多源回退）
func (c *AggregatorClient) FetchAggChapter(bookID, chapterID string) string {
	for _, baseURL := range aggSources {
		paths := []string{
			fmt.Sprintf("%s/txt/%s/%s.html", baseURL, bookID, chapterID),
			fmt.Sprintf("%s/%s/%s.html", baseURL, bookID, chapterID),
			fmt.Sprintf("%s/book/%s/%s.html", baseURL, bookID, chapterID),
		}
		for _, chURL := range paths {
			html, err := c.get(chURL)
			if err != nil { continue }
			content := extractChapterContent(html)
			if content != "" { return content }
		}
	}
	return ""
}

func extractChapterContent(html string) string {
	// 多种正文容器匹配
	patterns := []string{
		`<div[^>]*class="[^"]*txtnav[^"]*"[^>]*>(.*?)</div>`,
		`<div[^>]*id="content"[^>]*>(.*?)</div>`,
		`<div[^>]*class="[^"]*content[^"]*"[^>]*>(.*?)</div>`,
		`<article[^>]*>(.*?)</article>`,
	}
	for _, pat := range patterns {
		if m := regexp.MustCompile(pat).FindStringSubmatch(html); m != nil {
			content := m[1]
			content = stripTags(content)
			content = strings.ReplaceAll(content, "&nbsp;", " ")
			content = strings.ReplaceAll(content, "&lt;", "<")
			content = strings.ReplaceAll(content, "&gt;", ">")
			content = strings.ReplaceAll(content, "&amp;", "&")
			content = regexp.MustCompile(`(?i)(69书吧|www\.69shu\.com|www\.ibooktxt\.net|顶点小说|笔趣阁|du00\.co|读零零)`).ReplaceAllString(content, "")
			content = regexp.MustCompile(`\n{3,}`).ReplaceAllString(content, "\n\n")
			if cnCount(content) >= 100 { return strings.TrimSpace(content) }
		}
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
