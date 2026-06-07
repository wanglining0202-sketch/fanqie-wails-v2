package main

import (
	"fmt"
	"io"
	"net/http"
	"regexp"
	"strings"
	"sync"
	"time"
)

type QidianClient struct {
	client *http.Client
}

func NewQidianClient() *QidianClient {
	return &QidianClient{client: &http.Client{Timeout: 15 * time.Second}}
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

func (c *QidianClient) SearchQidian(keyword string) []BookResult {
	if regexp.MustCompile(`^\d{7,}$`).MatchString(keyword) {
		info, err := c.GetQidianInfo(keyword)
		if err == nil && info.Found {
			return []BookResult{{BookID: keyword, Title: info.Title, Author: info.Author, Source: "qidian"}}
		}
	}
	searchURL := fmt.Sprintf("https://m.qidian.com/soushu/%s.html?pageNum=1", keyword)
	html, err := c.get(searchURL)
	if err != nil { return nil }
	re := regexp.MustCompile(`<a[^>]*href="/book/(\d+)/"[^>]*>\s*<h3[^>]*>([^<]+)</h3>`)
	var results []BookResult
	for _, m := range re.FindAllStringSubmatch(html, -1) {
		results = append(results, BookResult{BookID: m[1], Title: strings.TrimSpace(m[2]), Source: "qidian"})
	}
	return results
}

func (c *QidianClient) GetQidianInfo(bookID string) (*BookInfo, error) {
	html, err := c.get("https://m.qidian.com/book/" + bookID + "/")
	if err != nil { return nil, err }
	title, author := "", ""
	if m := regexp.MustCompile(`<h1[^>]*>([^<]+)</h1>`).FindStringSubmatch(html); m != nil { title = strings.TrimSpace(m[1]) }
	if m := regexp.MustCompile(`作者[：:]\s*<a[^>]*>([^<]+)</a>`).FindStringSubmatch(html); m != nil { author = strings.TrimSpace(m[1]) }
	if author == "" {
		if m := regexp.MustCompile(`"authorName":"([^"]+)"`).FindStringSubmatch(html); m != nil { author = m[1] }
	}

	var chapters []Chapter
	catHTML, _ := c.get("https://m.qidian.com/book/" + bookID + "/catalog/")
	chRe := regexp.MustCompile(`data-cid="(\d+)"[^>]*>[^<]*(?:<div[^>]*>)?\s*<(h[23])[^>]*>([^<]+)</h[23]>`)
	for _, m := range chRe.FindAllStringSubmatch(catHTML, -1) {
		chapters = append(chapters, Chapter{ItemID: m[1], Title: strings.TrimSpace(m[3])})
	}
	return &BookInfo{Found: title != "", Source: "qidian", BookID: bookID, Title: title, Author: author, ChapterCount: len(chapters), Chapters: chapters}, nil
}

func (c *QidianClient) FetchQidianChapter(bookID, chapterID string) string {
	html, err := c.get(fmt.Sprintf("https://m.qidian.com/chapter/%s/%s/", bookID, chapterID))
	if err != nil { return "" }
	re := regexp.MustCompile(`<[^>]+class="[^"]*content[^"]*"[^>]*>(.*?)</(?:main|div)>`)
	m := re.FindStringSubmatch(html)
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

// DownloadQidianHybrid 并发混合下载 + 进度回调
func (c *QidianClient) DownloadQidianHybrid(bookID, outputDir string, onProgress func(done, total int)) (*DownloadResult, error) {
	mkdir(outputDir)
	info, err := c.GetQidianInfo(bookID)
	if err != nil || !info.Found { return &DownloadResult{Success: false, Error: "未找到书籍"}, nil }
	if len(info.Chapters) == 0 { return &DownloadResult{Success: false, Error: "无章节数据"}, nil }

	safeTitle := safeFilename(info.Title)
	outputPath := fmt.Sprintf("%s/%s.txt", outputDir, safeTitle)
	startTime := now()

	// 聚合站回退
	agg := NewSimpleAggregator()
	aggBookID := ""
	if aggResults := agg.Search(info.Title); len(aggResults) > 0 { aggBookID = aggResults[0].BookID }

	total := len(info.Chapters)
	results := make([]ChapterResult, total)
	var mu sync.Mutex
	var wg sync.WaitGroup
	sem := make(chan struct{}, 8) // 8并发
	downloaded, totalChars := 0, 0

	for i, ch := range info.Chapters {
		wg.Add(1)
		go func(idx int, chapter Chapter) {
			defer wg.Done()
			sem <- struct{}{}
			defer func() { <-sem }()

			content := c.FetchQidianChapter(bookID, chapter.ItemID)
			if content == "" && aggBookID != "" {
				content = agg.FetchChapter(aggBookID, chapter.ItemID)
			}

			mu.Lock()
			if content != "" {
				results[idx] = ChapterResult{Title: chapter.Title, Content: content}
				downloaded++
				totalChars += cnCount(content)
			}
			done := downloaded
			mu.Unlock()

			if onProgress != nil && done%10 == 0 {
				onProgress(done, total)
			}
		}(i, ch)
	}
	wg.Wait()

	// 按序写入
	var finalResults []ChapterResult
	for _, r := range results {
		if r.Content != "" { finalResults = append(finalResults, r) }
	}
	writeResults(outputPath, info.Title, info.Author, finalResults, totalChars)

	return &DownloadResult{
		Success: downloaded > 0, Title: info.Title, Path: outputPath,
		CNChars: totalChars, TotalChapters: total, Downloaded: downloaded,
		FailedCount: total - downloaded, ElapsedSec: since(startTime),
		Method: "qidian_concurrent",
	}, nil
}
