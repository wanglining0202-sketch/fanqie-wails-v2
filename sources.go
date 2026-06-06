package main

import (
	"fmt"
	"io"
	"net/http"
	"net/url"
	"regexp"
	"strings"
	"sync"
	"time"
)

// ── 多源自动回退管理器 ──
// 从1963个精校书源中筛选，按稳定性排序，自动故障转移

type SourceStatus int

const (
	SourceHealthy SourceStatus = iota
	SourceDegraded
	SourceDead
)

type NovelSource struct {
	Name    string
	BaseURL string
	Status  SourceStatus
	Fails   int
	mu      sync.Mutex
}

// 经测试筛选的稳定源（1963个中连通性最好的）
var stableSources = []NovelSource{
	{Name: "顶点小说", BaseURL: "https://www.ibooktxt.net"},
	{Name: "笔趣阁5200", BaseURL: "https://www.biquge5200.net"},
	{Name: "连尚读书", BaseURL: "https://www.lsds.cn"},
	{Name: "69书吧", BaseURL: "https://www.69shu.com"},
	{Name: "69好书", BaseURL: "https://www.69haoshu.com"},
	{Name: "77读书", BaseURL: "https://www.77dushu.com"},
	{Name: "笔迷读", BaseURL: "https://www.bimidu.com"},
	{Name: "必去小说", BaseURL: "https://www.ibiquw.info"},
	{Name: "读零零", BaseURL: "https://www.du00.co"},
}

type SourceManager struct {
	client  *http.Client
	sources []*NovelSource
	current int
	mu      sync.Mutex
}

func NewSourceManager() *SourceManager {
	sm := &SourceManager{
		client: &http.Client{Timeout: 15 * time.Second},
	}
	for i := range stableSources {
		sm.sources = append(sm.sources, &stableSources[i])
	}
	return sm
}

func (sm *SourceManager) get(u string) (string, error) {
	req, _ := http.NewRequest("GET", u, nil)
	req.Header.Set("User-Agent", UA)
	resp, err := sm.client.Do(req)
	if err != nil { return "", err }
	defer resp.Body.Close()
	body, _ := io.ReadAll(io.LimitReader(resp.Body, 2<<20))
	return string(body), nil
}

// GetActiveSource 获取当前活跃源（自动跳过失效源）
func (sm *SourceManager) GetActiveSource() *NovelSource {
	sm.mu.Lock()
	defer sm.mu.Unlock()

	start := sm.current
	for i := 0; i < len(sm.sources); i++ {
		idx := (start + i) % len(sm.sources)
		s := sm.sources[idx]
		if s.Status != SourceDead {
			sm.current = idx
			return s
		}
	}
	return sm.sources[0] // fallback
}

// MarkFail 标记源失败，超过3次自动降级
func (sm *SourceManager) MarkFail(s *NovelSource) {
	s.mu.Lock()
	defer s.mu.Unlock()
	s.Fails++
	if s.Fails >= 3 {
		s.Status = SourceDead
	} else if s.Fails >= 1 {
		s.Status = SourceDegraded
	}
}

// Search 多源搜索，自动故障转移
func (sm *SourceManager) Search(keyword string) []BookResult {
	for attempt := 0; attempt < len(sm.sources); attempt++ {
		s := sm.GetActiveSource()
		results := sm.searchOne(s, keyword)
		if len(results) > 0 {
			return results
		}
		sm.MarkFail(s)
	}
	return nil
}

func (sm *SourceManager) searchOne(s *NovelSource, keyword string) []BookResult {
	// 每个源用不同的搜索URL格式
	searchURLs := []string{
		fmt.Sprintf("%s/search/?searchkey=%s", s.BaseURL, url.QueryEscape(keyword)),
		fmt.Sprintf("%s/search?key=%s", s.BaseURL, url.QueryEscape(keyword)),
		fmt.Sprintf("%s/search.html?k=%s", s.BaseURL, url.QueryEscape(keyword)),
		fmt.Sprintf("%s/ss/?searchkey=%s", s.BaseURL, url.QueryEscape(keyword)),
	}
	var html string
	var err error
	for _, u := range searchURLs {
		html, err = sm.get(u)
		if err == nil && len(html) > 500 { break }
	}
	if err != nil || len(html) < 500 { return nil }

	itemRe := regexp.MustCompile(`<a[^>]*href="(/(?:book|txt|read|html)/?\d+[^"]*)"[^>]*>\s*(?:<h3[^>]*>)?([^<]{2,60})(?:</h3>)?\s*</a>`)
	matches := itemRe.FindAllStringSubmatch(html, -1)

	var results []BookResult
	for _, m := range matches {
		bookID := regexp.MustCompile(`\d+`).FindString(m[1])
		title := strings.TrimSpace(m[2])
		if len(title) < 2 || bookID == "" { continue }
		results = append(results, BookResult{
			BookID: bookID, Title: title, Source: s.Name,
		})
		if len(results) >= 20 { break }
	}
	return results
}

// GetBookInfo 多源获取书籍信息
func (sm *SourceManager) GetBookInfo(bookID string) (*BookInfo, error) {
	for attempt := 0; attempt < len(sm.sources); attempt++ {
		s := sm.GetActiveSource()
		info, err := sm.getInfoOne(s, bookID)
		if err == nil && info.Found { return info, nil }
		sm.MarkFail(s)
	}
	return nil, fmt.Errorf("所有源均未找到")
}

func (sm *SourceManager) getInfoOne(s *NovelSource, bookID string) (*BookInfo, error) {
	paths := []string{
		fmt.Sprintf("%s/book/%s/", s.BaseURL, bookID),
		fmt.Sprintf("%s/txt/%s/", s.BaseURL, bookID),
		fmt.Sprintf("%s/%s/", s.BaseURL, bookID),
	}
	for _, infoURL := range paths {
		html, err := sm.get(infoURL)
		if err != nil { continue }

		title, author, desc := "", "", ""
		if m := regexp.MustCompile(`<h1[^>]*>(.*?)</h1>`).FindStringSubmatch(html); m != nil {
			title = strings.TrimSpace(m[1])
		}
		if m := regexp.MustCompile(`作者[：:]\s*(.*?)[<\n]`).FindStringSubmatch(html); m != nil {
			author = strings.TrimSpace(m[1])
		}
		if m := regexp.MustCompile(`<div[^>]*class="[^"]*intro[^"]*"[^>]*>(.*?)</div>`).FindStringSubmatch(html); m != nil {
			desc = stripTags(strings.TrimSpace(m[1]))
		}

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
				Found: true, Source: s.Name, BookID: bookID,
				Title: title, Author: author, Description: desc,
				ChapterCount: len(chapters), Chapters: chapters,
			}, nil
		}
	}
	return nil, fmt.Errorf("未找到")
}

// FetchChapter 多源获取章节
func (sm *SourceManager) FetchChapter(bookID, chapterID string) string {
	for attempt := 0; attempt < len(sm.sources); attempt++ {
		s := sm.GetActiveSource()
		paths := []string{
			fmt.Sprintf("%s/txt/%s/%s.html", s.BaseURL, bookID, chapterID),
			fmt.Sprintf("%s/%s/%s.html", s.BaseURL, bookID, chapterID),
			fmt.Sprintf("%s/book/%s/%s.html", s.BaseURL, bookID, chapterID),
		}
		for _, chURL := range paths {
			html, err := sm.get(chURL)
			if err != nil { continue }
			content := extractChapterContent(html)
			if content != "" { return content }
		}
		sm.MarkFail(s)
	}
	return ""
}

// Download 多源下载
func (sm *SourceManager) Download(bookID, outputDir string) (*DownloadResult, error) {
	mkdir(outputDir)
	info, err := sm.GetBookInfo(bookID)
	if err != nil || !info.Found {
		return &DownloadResult{Success: false, Error: "未找到书籍"}, nil
	}

	safeTitle := safeFilename(info.Title)
	outputPath := fmt.Sprintf("%s/%s.txt", outputDir, safeTitle)
	startTime := now()

	var results []ChapterResult
	downloaded, totalChars := 0, 0
	var failedItems []string

	for i, ch := range info.Chapters {
		content := sm.FetchChapter(bookID, ch.ItemID)
		if content != "" {
			results = append(results, ChapterResult{Title: ch.Title, Content: content})
			downloaded++
			totalChars += cnCount(content)
		} else {
			failedItems = append(failedItems, ch.Title)
		}
		if i%10 == 0 { msSleep(200) }
	}

	writeResults(outputPath, info.Title, info.Author, results, totalChars)
	return &DownloadResult{
		Success: downloaded > 0, Title: info.Title, Path: outputPath,
		CNChars: totalChars, TotalChapters: len(info.Chapters),
		Downloaded: downloaded, FailedCount: len(failedItems),
		Failed: failedItems, ElapsedSec: since(startTime), Method: "multisource",
	}, nil
}

// HealthCheck 源健康检查
func (sm *SourceManager) HealthCheck() map[string]bool {
	results := make(map[string]bool)
	for _, s := range sm.sources {
		_, err := sm.get(s.BaseURL)
		results[s.Name] = err == nil
	}
	return results
}
