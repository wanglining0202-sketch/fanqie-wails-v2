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

// ── 搜索引擎聚合下载 ──
// 通过 Google/Bing 搜索 "小说名 全文阅读" 定位当前可用的盗版站
// 不依赖硬编码域名，始终找最新可用的源

type SearchAggregator struct {
	client *http.Client
}

func NewSearchAggregator() *SearchAggregator {
	return &SearchAggregator{client: &http.Client{Timeout: 20 * time.Second}}
}

func (sa *SearchAggregator) get(u string) (string, error) {
	req, _ := http.NewRequest("GET", u, nil)
	req.Header.Set("User-Agent", "Mozilla/5.0 (Windows NT 10.0; Win64; x64) AppleWebKit/537.36 (KHTML, like Gecko) Chrome/120.0.0.0 Safari/537.36")
	req.Header.Set("Accept-Language", "zh-CN,zh;q=0.9")
	resp, err := sa.client.Do(req)
	if err != nil { return "", err }
	defer resp.Body.Close()
	body, _ := io.ReadAll(io.LimitReader(resp.Body, 1<<20))
	return string(body), nil
}

// findSite 搜索引擎查找书籍所在的可用站
func (sa *SearchAggregator) findSite(keyword string) (string, string) {
	query := url.QueryEscape(keyword + " 全文阅读 小说")
	
	// Bing 搜索（比 Google 在国内更稳定）
	bingURL := fmt.Sprintf("https://www.bing.com/search?q=%s&setlang=zh-cn", query)
	html, err := sa.get(bingURL)
	if err != nil {
		// 回退 Google
		googleURL := fmt.Sprintf("https://www.google.com/search?q=%s&hl=zh-CN", query)
		html, err = sa.get(googleURL)
	}
	if err != nil || len(html) < 500 { return "", "" }

	// 提取搜索结果中的链接
	linkRe := regexp.MustCompile(`<a[^>]*href="(https?://[^"]*(?:69shu|ibooktxt|biquge|du00|zwduxs|77dushu|bimidu|ibiquw|txtduo|deqixs|83zws|dbxsd|changduzw|wrlwx|ixuanshu|umiwx|hltxt|lsds|69haoshu)[^"]*)"[^>]*>`)
	m := linkRe.FindStringSubmatch(html)
	if m != nil {
		return m[1], keyword
	}
	return "", ""
}

// Search 搜索引擎聚合搜索
func (sa *SearchAggregator) Search(keyword string) []BookResult {
	siteURL, _ := sa.findSite(keyword)
	if siteURL == "" {
		// 回退到固定源
		return sa.fallbackSearch(keyword)
	}
	
	// 从找到的站点搜索
	baseURL := sa.extractBase(siteURL)
	return sa.searchOn(baseURL, keyword)
}

func (sa *SearchAggregator) extractBase(u string) string {
	u = strings.TrimPrefix(u, "https://")
	u = strings.TrimPrefix(u, "http://")
	parts := strings.SplitN(u, "/", 2)
	return "https://" + parts[0]
}

func (sa *SearchAggregator) searchOn(baseURL, keyword string) []BookResult {
	urls := []string{
		fmt.Sprintf("%s/search/?searchkey=%s", baseURL, url.QueryEscape(keyword)),
		fmt.Sprintf("%s/search?key=%s", baseURL, url.QueryEscape(keyword)),
		fmt.Sprintf("%s/ss/?searchkey=%s", baseURL, url.QueryEscape(keyword)),
	}
	for _, u := range urls {
		html, err := sa.get(u)
		if err != nil || len(html) < 1000 { continue }
		
		itemRe := regexp.MustCompile(`<a[^>]*href="(/(?:book|txt|read|html)/?\d+[^"]*)"[^>]*>\s*(?:<h3[^>]*>)?([^<]{2,60})</a>`)
		matches := itemRe.FindAllStringSubmatch(html, -1)
		if len(matches) == 0 { continue }
		
		var results []BookResult
		for _, m := range matches {
			bookID := regexp.MustCompile(`\d+`).FindString(m[1])
			if bookID == "" || len(results) >= 20 { break }
			results = append(results, BookResult{
				BookID: bookID, Title: strings.TrimSpace(m[2]),
				Source: baseURL,
			})
		}
		return results
	}
	return nil
}

func (sa *SearchAggregator) fallbackSearch(keyword string) []BookResult {
	// 固定回退源
	fallbacks := []string{
		"https://www.ibooktxt.net",
		"https://www.du00.co",
		"https://www.biquge5200.net",
	}
	for _, base := range fallbacks {
		results := sa.searchOn(base, keyword)
		if len(results) > 0 { return results }
	}
	return nil
}

// GetBookInfo 获书籍信息
func (sa *SearchAggregator) GetBookInfo(bookID, siteURL string) (*BookInfo, error) {
	baseURL := sa.extractBase(siteURL)
	paths := []string{
		fmt.Sprintf("%s/book/%s/", baseURL, bookID),
		fmt.Sprintf("%s/txt/%s/", baseURL, bookID),
		fmt.Sprintf("%s/%s/", baseURL, bookID),
	}
	for _, u := range paths {
		html, err := sa.get(u)
		if err != nil { continue }
		
		title, author := "", ""
		if m := regexp.MustCompile(`<h1[^>]*>(.*?)</h1>`).FindStringSubmatch(html); m != nil {
			title = strings.TrimSpace(m[1])
		}
		if m := regexp.MustCompile(`作者[：:]\s*(.*?)[<\n]`).FindStringSubmatch(html); m != nil {
			author = strings.TrimSpace(m[1])
		}
		
		var chapters []Chapter
		chRe := regexp.MustCompile(`<a[^>]*href="[^"]*/(\d+)\.html"[^>]*>([^<]{2,80})</a>`)
		for _, m := range chRe.FindAllStringSubmatch(html, -1) {
			chapters = append(chapters, Chapter{ItemID: m[1], Title: strings.TrimSpace(m[2])})
		}
		
		if title != "" && len(chapters) > 0 {
			return &BookInfo{
				Found: true, Source: baseURL, BookID: bookID,
				Title: title, Author: author, ChapterCount: len(chapters),
				Chapters: chapters,
			}, nil
		}
	}
	return nil, fmt.Errorf("未找到")
}

// FetchChapter 获取章节
func (sa *SearchAggregator) FetchChapter(bookID, chapterID, siteURL string) string {
	baseURL := sa.extractBase(siteURL)
	paths := []string{
		fmt.Sprintf("%s/txt/%s/%s.html", baseURL, bookID, chapterID),
		fmt.Sprintf("%s/%s/%s.html", baseURL, bookID, chapterID),
		fmt.Sprintf("%s/book/%s/%s.html", baseURL, bookID, chapterID),
	}
	for _, u := range paths {
		html, err := sa.get(u)
		if err != nil { continue }
		content := extractChapterContent(html)
		if content != "" { return content }
	}
	return ""
}

// Download 下载全书
func (sa *SearchAggregator) Download(keyword, outputDir string) (*DownloadResult, error) {
	siteURL, bookID := sa.findSite(keyword)
	if siteURL == "" {
		return &DownloadResult{Success: false, Error: "未找到可用源"}, nil
	}
	
	mkdir(outputDir)
	info, err := sa.GetBookInfo(bookID, siteURL)
	if err != nil || !info.Found {
		return &DownloadResult{Success: false, Error: "获取书籍信息失败"}, nil
	}
	
	safeTitle := safeFilename(info.Title)
	outputPath := fmt.Sprintf("%s/%s.txt", outputDir, safeTitle)
	startTime := now()
	
	var results []ChapterResult
	downloaded, totalChars := 0, 0
	
	for i, ch := range info.Chapters {
		content := sa.FetchChapter(bookID, ch.ItemID, siteURL)
		if content != "" {
			results = append(results, ChapterResult{Title: ch.Title, Content: content})
			downloaded++
			totalChars += cnCount(content)
		}
		if i%5 == 0 { msSleep(300) }
	}
	
	writeResults(outputPath, info.Title, info.Author, results, totalChars)
	return &DownloadResult{
		Success: downloaded > 0, Title: info.Title, Path: outputPath,
		CNChars: totalChars, TotalChapters: len(info.Chapters),
		Downloaded: downloaded, ElapsedSec: since(startTime), Method: "search_aggregator",
	}, nil
}
