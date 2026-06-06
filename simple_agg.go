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

// ── 简化聚合搜索 ──
// 使用经过验证的站点，POST + GET 多策略搜索

type SimpleAggregator struct {
	client *http.Client
}

func NewSimpleAggregator() *SimpleAggregator {
	return &SimpleAggregator{client: &http.Client{Timeout: 15 * time.Second}}
}

func (sa *SimpleAggregator) get(u string) (string, error) {
	req, _ := http.NewRequest("GET", u, nil)
	req.Header.Set("User-Agent", "Mozilla/5.0 (Windows NT 10.0; Win64; x64) AppleWebKit/537.36")
	req.Header.Set("Accept-Language", "zh-CN,zh;q=0.9")
	resp, err := sa.client.Do(req)
	if err != nil { return "", err }
	defer resp.Body.Close()
	body, _ := io.ReadAll(io.LimitReader(resp.Body, 1<<20))
	return string(body), nil
}

func (sa *SimpleAggregator) post(u string, bodyStr string) (string, error) {
	req, _ := http.NewRequest("POST", u, strings.NewReader(bodyStr))
	req.Header.Set("User-Agent", "Mozilla/5.0 (Windows NT 10.0; Win64; x64) AppleWebKit/537.36")
	req.Header.Set("Content-Type", "application/x-www-form-urlencoded")
	resp, err := sa.client.Do(req)
	if err != nil { return "", err }
	defer resp.Body.Close()
	body, _ := io.ReadAll(io.LimitReader(resp.Body, 1<<20))
	return string(body), nil
}

// Search 多策略搜索
func (sa *SimpleAggregator) Search(keyword string) []BookResult {
	// 策略1: 百度小说 API (最稳定)
	results := sa.baiduSearch(keyword)
	if len(results) > 0 { return results }

	// 策略2: 推书君 API
	results = sa.tuishujunSearch(keyword)
	if len(results) > 0 { return results }

	// 策略3: 通用 POST 搜索
	results = sa.genericSearch(keyword)
	return results
}

func (sa *SimpleAggregator) baiduSearch(keyword string) []BookResult {
	u := fmt.Sprintf("https://dushu.baidu.com/api/getSearchResultData?page=1&count=10&query=%s", url.QueryEscape(keyword))
	html, err := sa.get(u)
	if err != nil { return nil }
	
	// 提取书名和ID
	var results []BookResult
	re := regexp.MustCompile(`"sTitle":"([^"]+)","nid":(\d+)`)
	for _, m := range re.FindAllStringSubmatch(html, -1) {
		results = append(results, BookResult{
			BookID: m[2], Title: m[1], Source: "baidu",
		})
	}
	return results
}

func (sa *SimpleAggregator) tuishujunSearch(keyword string) []BookResult {
	u := fmt.Sprintf("https://pre-api.tuishujun.com/api/searchBook?search_value=%s&sort_field=hot_value&page=1&pageSize=20", url.QueryEscape(keyword))
	html, err := sa.get(u)
	if err != nil { return nil }
	
	var results []BookResult
	re := regexp.MustCompile(`"title":"([^"]+)","book_id":(\d+)`)
	for _, m := range re.FindAllStringSubmatch(html, -1) {
		results = append(results, BookResult{
			BookID: m[2], Title: m[1], Source: "tuishujun",
		})
	}
	return results
}

func (sa *SimpleAggregator) genericSearch(keyword string) []BookResult {
	// 多个站点 + GET/POST 策略
	type searchConfig struct {
		url    string
		isPost bool
		body   string
	}
	
	configs := []searchConfig{
		{fmt.Sprintf("https://www.ibooktxt.net/search/?searchkey=%s", url.QueryEscape(keyword)), false, ""},
		{fmt.Sprintf("https://www.69shu.com/modules/article/search.php?searchkey=%s", url.QueryEscape(keyword)), false, ""},
		{fmt.Sprintf("https://www.biquge5200.net/modules/article/search.php?searchkey=%s", url.QueryEscape(keyword)), false, ""},
		{"https://www.69shu.com/modules/article/search.php", true, fmt.Sprintf("searchkey=%s&searchtype=articlename", url.QueryEscape(keyword))},
		{"https://www.biquge5200.net/modules/article/search.php", true, fmt.Sprintf("searchkey=%s", url.QueryEscape(keyword))},
	}
	
	for _, cfg := range configs {
		var html string
		var err error
		if cfg.isPost {
			html, err = sa.post(cfg.url, cfg.body)
		} else {
			html, err = sa.get(cfg.url)
		}
		if err != nil || len(html) < 1000 { continue }
		
		// 通用提取: <a href="...数字.html">标题</a>
		re := regexp.MustCompile(`<a[^>]*href="[^"]*/(\d+)\.html"[^>]*>([^<]{3,80})</a>`)
		matches := re.FindAllStringSubmatch(html, -1)
		if len(matches) < 3 { continue }
		
		var results []BookResult
		for _, m := range matches {
			results = append(results, BookResult{
				BookID: m[1], Title: strings.TrimSpace(m[2]), Source: "aggregator",
			})
			if len(results) >= 20 { break }
		}
		return results
	}
	return nil
}

// GetBookInfo 获取书籍信息
func (sa *SimpleAggregator) GetBookInfo(bookID string) (*BookInfo, error) {
	sites := []string{
		"https://www.ibooktxt.net",
		"https://www.69shu.com",
		"https://www.biquge5200.net",
	}
	for _, base := range sites {
		info, err := sa.getInfoOne(base, bookID)
		if err == nil && info.Found { return info, nil }
	}
	return nil, fmt.Errorf("未找到")
}

func (sa *SimpleAggregator) getInfoOne(baseURL, bookID string) (*BookInfo, error) {
	paths := []string{
		fmt.Sprintf("%s/book/%s/", baseURL, bookID),
		fmt.Sprintf("%s/txt/%s/", baseURL, bookID),
		fmt.Sprintf("%s/%s/", baseURL, bookID),
	}
	for _, u := range paths {
		html, err := sa.get(u)
		if err != nil { continue }
		title, author := "", ""
		if m := regexp.MustCompile(`<h1[^>]*>(.*?)</h1>`).FindStringSubmatch(html); m != nil { title = strings.TrimSpace(m[1]) }
		if m := regexp.MustCompile(`作者[：:]\s*(.*?)[<\n]`).FindStringSubmatch(html); m != nil { author = strings.TrimSpace(m[1]) }
		
		var chapters []Chapter
		chRe := regexp.MustCompile(`<a[^>]*href="[^"]*/(\d+)\.html"[^>]*>([^<]{2,80})</a>`)
		for _, m := range chRe.FindAllStringSubmatch(html, -1) {
			chapters = append(chapters, Chapter{ItemID: m[1], Title: strings.TrimSpace(m[2])})
		}
		if title != "" && len(chapters) > 0 {
			return &BookInfo{Found: true, Source: baseURL, BookID: bookID, Title: title, Author: author, ChapterCount: len(chapters), Chapters: chapters}, nil
		}
	}
	return nil, fmt.Errorf("未找到")
}

// FetchChapter 获取章节
func (sa *SimpleAggregator) FetchChapter(bookID, chapterID string) string {
	sites := []string{"https://www.ibooktxt.net", "https://www.69shu.com", "https://www.biquge5200.net"}
	for _, base := range sites {
		for _, p := range []string{"/txt/%s/%s.html", "/%s/%s.html", "/book/%s/%s.html"} {
			u := fmt.Sprintf(base+p, bookID, chapterID)
			html, _ := sa.get(u)
			if c := extractChapterContent(html); c != "" { return c }
		}
	}
	return ""
}

// Download 下载全书
func (sa *SimpleAggregator) Download(bookID, outputDir string) (*DownloadResult, error) {
	mkdir(outputDir)
	info, err := sa.GetBookInfo(bookID)
	if err != nil || !info.Found { return &DownloadResult{Success: false, Error: "未找到书籍"}, nil }
	
	safeTitle := safeFilename(info.Title)
	outputPath := fmt.Sprintf("%s/%s.txt", outputDir, safeTitle)
	st := now()
	
	var results []ChapterResult
	downloaded, totalChars := 0, 0
	for _, ch := range info.Chapters {
		if content := sa.FetchChapter(bookID, ch.ItemID); content != "" {
			results = append(results, ChapterResult{Title: ch.Title, Content: content})
			downloaded++
			totalChars += cnCount(content)
		}
		if downloaded%5 == 0 { msSleep(200) }
	}
	writeResults(outputPath, info.Title, info.Author, results, totalChars)
	return &DownloadResult{Success: downloaded > 0, Title: info.Title, Path: outputPath, CNChars: totalChars, TotalChapters: len(info.Chapters), Downloaded: downloaded, ElapsedSec: since(st), Method: "simple_agg"}, nil
}
