package main

import (
	"encoding/json"
	"fmt"
	"io"
	"math/rand"
	"net/http"
	"net/url"
	"os"
	"path/filepath"
	"regexp"
	"sort"
	"strings"
	"sync"
	"time"
)

const (
	UA           = "Mozilla/5.0 (Windows NT 10.0; Win64; x64) AppleWebKit/537.36"
	BASE_FQ      = "https://fanqienovel.com"
	BASE_IX      = "https://ixdzs8.com"
	FQ_PROXY     = "https://tt.sjmyzq.cn/api/raw_full"
	FQ_MOBILE_API = "https://api5-normal-lf.fqnovel.com"
)

type FanqieClient struct {
	client *http.Client
}

func NewFanqieClient() *FanqieClient {
	return &FanqieClient{
		client: &http.Client{Timeout: 30 * time.Second},
	}
}

// ── 数据结构 ──

type BookResult struct {
	BookID  string `json:"book_id"`
	Title   string `json:"title"`
	Author  string `json:"author"`
	Source  string `json:"source"`
	Description string `json:"description,omitempty"`
}

type Chapter struct {
	ItemID  string `json:"item_id"`
	Title   string `json:"title"`
	NeedPay bool   `json:"need_pay"`
}

type BookInfo struct {
	Found        bool      `json:"found"`
	Source       string    `json:"source"`
	BookID       string    `json:"book_id"`
	Title        string    `json:"title"`
	Author       string    `json:"author"`
	Status       string    `json:"status"`
	Description  string    `json:"description"`
	ChapterCount int       `json:"chapter_count"`
	WordCount    int       `json:"word_count"`
	Chapters     []Chapter `json:"chapters"`
	Tags         string    `json:"tags"`
	Cover        string    `json:"cover,omitempty"`
}

type SearchResponse struct {
	Results []BookResult `json:"results"`
	Count   int          `json:"count"`
}

type DownloadResult struct {
	Success       bool     `json:"success"`
	Title         string   `json:"title"`
	Path          string   `json:"path"`
	CNChars       int      `json:"cn_chars"`
	TotalChapters int      `json:"total_chapters"`
	Downloaded    int      `json:"downloaded"`
	FailedCount   int      `json:"failed_count"`
	Failed        []string `json:"failed"`
	ElapsedSec    float64  `json:"elapsed_seconds"`
	Method        string   `json:"method"`
	Error         string   `json:"error,omitempty"`
}

// ── HTTP 工具 ──

func (c *FanqieClient) get(urlStr string) (*http.Response, error) {
	req, err := http.NewRequest("GET", urlStr, nil)
	if err != nil {
		return nil, err
	}
	req.Header.Set("User-Agent", UA)
	return c.client.Do(req)
}

func (c *FanqieClient) getWithRetry(urlStr string, retries int) (*http.Response, error) {
	var lastErr error
	for i := 0; i < retries; i++ {
		resp, err := c.get(urlStr)
		if err == nil {
			return resp, nil
		}
		lastErr = err
		time.Sleep(time.Duration(1<<uint(i)) * time.Second)
	}
	return nil, lastErr
}

func readBody(resp *http.Response) string {
	defer resp.Body.Close()
	body, _ := io.ReadAll(io.LimitReader(resp.Body, 5*1024*1024)) // 5MB cap
	return string(body)
}

// ── HTML INIT_STATE 提取 ──

var initStateRe = regexp.MustCompile(`window\.__INITIAL_STATE__\s*=`)

func extractInitState(html string) map[string]interface{} {
	pos := initStateRe.FindStringIndex(html)
	if pos == nil {
		return nil
	}
	start := pos[1]
	// 手动跟踪括号深度来提取 JSON
	depth := 0
	inStr := false
	escaped := false
	end := start
	for i := start; i < len(html); i++ {
		ch := html[i]
		if escaped {
			escaped = false
			continue
		}
		if ch == '\\' {
			escaped = true
			continue
		}
		if ch == '"' && !escaped {
			inStr = !inStr
			continue
		}
		if inStr {
			continue
		}
		if ch == '{' {
			depth++
		} else if ch == '}' {
			depth--
			if depth == 0 {
				end = i + 1
				break
			}
		}
	}

	jsonStr := html[start:end]
	var result map[string]interface{}
	if err := json.Unmarshal([]byte(jsonStr), &result); err != nil {
		return nil
	}
	return result
}

// ── 搜索 ──

var (
	bookPattern  = regexp.MustCompile(`<li\s+class="burl"\s+data-url="/read/(\d+)/">(.*?)</li>`)
	titlePattern = regexp.MustCompile(`<h3\s+class="bname">\s*<a[^>]*>(.*?)</a>`)
	authorPattern = regexp.MustCompile(`<span\s+class="bauthor">\s*<a[^>]*>(.*?)</a>`)
	tagStripRe    = regexp.MustCompile(`<[^>]+>`)
)

func (c *FanqieClient) Search(keyword string) (*SearchResponse, error) {
	results := c.ixdzs8Search(keyword)

	// 番茄移动端搜索
	mobileResults := c.fanqieMobileSearch(keyword)
	seen := make(map[string]bool)
	for _, r := range results {
		seen[r.BookID] = true
	}
	for _, r := range mobileResults {
		if !seen[r.BookID] {
			results = append(results, r)
		}
	}

	return &SearchResponse{Results: results, Count: len(results)}, nil
}

func (c *FanqieClient) ixdzs8Search(keyword string) []BookResult {
	searchURL := fmt.Sprintf("%s/bsearch?q=%s", BASE_IX, url.QueryEscape(keyword))
	resp, err := c.getWithRetry(searchURL, 2)
	if err != nil {
		return nil
	}
	html := readBody(resp)

	var results []BookResult
	matches := bookPattern.FindAllStringSubmatch(html, -1)
	for _, m := range matches {
		bookID := m[1]
		block := m[2]

		tm := titlePattern.FindStringSubmatch(block)
		am := authorPattern.FindStringSubmatch(block)

		title := ""
		if tm != nil {
			title = strings.TrimSpace(tagStripRe.ReplaceAllString(tm[1], ""))
		}
		author := ""
		if am != nil {
			author = strings.TrimSpace(tagStripRe.ReplaceAllString(am[1], ""))
		}

		if title != "" && bookID != "" {
			results = append(results, BookResult{
				BookID: bookID,
				Title:  title,
				Author: author,
				Source: "ixdzs8",
			})
		}
		if len(results) >= 20 {
			break
		}
	}
	return results
}

func (c *FanqieClient) fanqieMobileSearch(keyword string) []BookResult {
	params := url.Values{
		"query":          {keyword},
		"aid":            {"1967"},
		"channel":        {"0"},
		"os_version":     {"0"},
		"device_type":    {"0"},
		"device_platform": {"0"},
		"iid":            {"466614321180296"},
		"version_code":   {"999"},
	}
	searchURL := fmt.Sprintf("%s/reading/bookapi/search/page/v/?%s", FQ_MOBILE_API, params.Encode())
	resp, err := c.get(searchURL)
	if err != nil {
		return nil
	}
	body := readBody(resp)

	var data struct {
		Code int `json:"code"`
		Data []struct {
			BookID   string `json:"book_id"`
			BookName string `json:"book_name"`
			Author   string `json:"author"`
			Abstract string `json:"abstract"`
		} `json:"data"`
	}
	if err := json.Unmarshal([]byte(body), &data); err != nil || data.Code != 0 {
		return nil
	}

	var results []BookResult
	for i, item := range data.Data {
		if i >= 20 {
			break
		}
		bookID := item.BookID
		if bookID == "" {
			continue
		}
		desc := item.Abstract
		if len(desc) > 200 {
			desc = desc[:200]
		}
		results = append(results, BookResult{
			BookID:      bookID,
			Title:       item.BookName,
			Author:      item.Author,
			Description: desc,
			Source:      "fanqie_mobile",
		})
	}
	return results
}

// ── 书籍详情 ──

var chapterRe = regexp.MustCompile(`"itemId":"([^"]+)","needPay":(\d+),"title":"([^"]*)"`)

func (c *FanqieClient) GetBookInfo(bookID string) (*BookInfo, error) {
	// 1. 尝试 fanqienovel.com
	info := c.fanqieInfo(bookID)
	if info != nil && info.Found {
		return info, nil
	}

	// 2. 目录 API 回退
	if info != nil && len(info.Chapters) == 0 {
		chapters := c.directoryAPI(bookID)
		if len(chapters) > 0 {
			info.Chapters = chapters
			info.ChapterCount = len(chapters)
			info.Found = true
			return info, nil
		}
	}

	// 3. ixdzs8 回退
	if info == nil || !info.Found {
		return c.ixdzs8Info(bookID)
	}

	return info, nil
}

func (c *FanqieClient) fanqieInfo(bookID string) *BookInfo {
	url := fmt.Sprintf("%s/page/%s", BASE_FQ, bookID)
	resp, err := c.getWithRetry(url, 2)
	if err != nil {
		return nil
	}
	html := readBody(resp)
	state := extractInitState(html)
	if state == nil {
		return nil
	}

	page, _ := state["page"].(map[string]interface{})
	if page == nil {
		return nil
	}

	// 提取章节
	var chapters []Chapter
	matches := chapterRe.FindAllStringSubmatch(html, -1)
	for _, m := range matches {
		chapters = append(chapters, Chapter{
			ItemID:  m[1],
			Title:   m[3],
			NeedPay: m[2] == "1",
		})
	}

	status := "连载中"
	if s, ok := page["status"].(float64); ok && s == 2 {
		status = "完结"
	}

	desc, _ := page["abstract"].(string)
	if len(desc) > 500 {
		desc = desc[:500]
	}

	chCount := len(chapters)
	if cc, ok := page["chapterCount"].(float64); ok {
		chCount = int(cc)
	}

	wc := 0
	if w, ok := page["wordCount"].(float64); ok {
		wc = int(w)
	}

	cover := ""
	if c, ok := page["thumbUrl"].(string); ok {
		cover = c
	}

	return &BookInfo{
		Found:        true,
		Source:       "fanqie",
		BookID:       bookID,
		Title:        getStr(page, "bookName"),
		Author:       getStr(page, "author"),
		Status:       status,
		Description:  desc,
		ChapterCount: chCount,
		WordCount:    wc,
		Chapters:     chapters,
		Cover:        cover,
	}
}

func (c *FanqieClient) directoryAPI(bookID string) []Chapter {
	dirURL := fmt.Sprintf("%s/api/reader/directory/detail?bookId=%s", BASE_FQ, bookID)
	resp, err := c.get(dirURL)
	if err != nil {
		return nil
	}
	body := readBody(resp)

	var data map[string]interface{}
	if err := json.Unmarshal([]byte(body), &data); err != nil {
		return nil
	}

	// 尝试多种格式
	var rawList []interface{}
	for _, key := range []string{"chapterList", "data"} {
		if val, ok := data[key]; ok {
			switch v := val.(type) {
			case []interface{}:
				rawList = v
			case map[string]interface{}:
				for _, sub := range []string{"chapterList", "chapters", "items", "list"} {
					if sl, ok := v[sub].([]interface{}); ok {
						rawList = sl
						break
					}
				}
			}
		}
		if rawList != nil {
			break
		}
	}

	// chapterListWithVolume
	if rawList == nil {
		if clw, ok := data["chapterListWithVolume"].([]interface{}); ok {
			for _, vol := range clw {
				switch v := vol.(type) {
				case []interface{}:
					rawList = append(rawList, v...)
				case map[string]interface{}:
					for _, sub := range []string{"chapterList", "chapters"} {
						if sl, ok := v[sub].([]interface{}); ok {
							rawList = append(rawList, sl...)
							break
						}
					}
				}
			}
		}
	}

	if rawList == nil {
		return nil
	}

	var chapters []Chapter
	for _, item := range rawList {
		ch, ok := item.(map[string]interface{})
		if !ok {
			continue
		}
		itemID := ""
		for _, key := range []string{"itemId", "item_id", "id"} {
			if v := fmt.Sprint(ch[key]); v != "" && v != "<nil>" {
				itemID = v
				break
			}
		}
		title := getStr(ch, "title")
		if itemID != "" {
			chapters = append(chapters, Chapter{ItemID: itemID, Title: title})
		}
	}
	return chapters
}

func (c *FanqieClient) ixdzs8Info(bookID string) (*BookInfo, error) {
	url := fmt.Sprintf("%s/read/%s/", BASE_IX, bookID)
	resp, err := c.getWithRetry(url, 2)
	if err != nil {
		return nil, err
	}
	html := readBody(resp)

	titleM := regexp.MustCompile(`<h1[^>]*>(.*?)</h1>`).FindStringSubmatch(html)
	authorM := regexp.MustCompile(`作者[：:]\s*(.*?)[<\n]`).FindStringSubmatch(html)

	title := ""
	if titleM != nil {
		title = strings.TrimSpace(tagStripRe.ReplaceAllString(titleM[1], ""))
	}
	author := ""
	if authorM != nil {
		author = strings.TrimSpace(authorM[1])
	}

	// 提取章节
	var chapters []Chapter
	chLinkRe := regexp.MustCompile(`<a[^>]*href="/read/[^"]*/(\d+)\.html"[^>]*>(.*?)</a>`)
	for _, m := range chLinkRe.FindAllStringSubmatch(html, -1) {
		chapters = append(chapters, Chapter{
			ItemID: m[1],
			Title:  strings.TrimSpace(tagStripRe.ReplaceAllString(m[2], "")),
		})
	}

	return &BookInfo{
		Found:        title != "",
		Source:       "ixdzs8",
		BookID:       bookID,
		Title:        title,
		Author:       author,
		ChapterCount: len(chapters),
		Chapters:     chapters,
	}, nil
}

// ── 排行榜 ──

type TrendingResponse struct {
	Results map[string][]BookResult `json:"results"`
}

func (c *FanqieClient) GetTrending() (*TrendingResponse, error) {
	resp, err := c.getWithRetry(BASE_IX, 2)
	if err != nil {
		return nil, err
	}
	html := readBody(resp)

	results := make(map[string][]BookResult)
	
	rankRe := regexp.MustCompile(`<div[^>]*class="[^"]*rank-list[^"]*"[^>]*>([\s\S]*?)</div>`)
	itemRe := regexp.MustCompile(`<a[^>]*href="/read/(\d+)/"[^>]*>\s*<h3[^>]*>(.*?)</h3>\s*<span[^>]*>(.*?)</span>`)

	rankMatches := rankRe.FindAllStringSubmatch(html, -1)
	lists := []string{"hot", "new", "recommend"}
	for i, name := range lists {
		if i < len(rankMatches) {
			var books []BookResult
			for _, m := range itemRe.FindAllStringSubmatch(rankMatches[i][1], -1) {
				books = append(books, BookResult{
					BookID: m[1],
					Title:  strings.TrimSpace(tagStripRe.ReplaceAllString(m[2], "")),
					Author: strings.TrimSpace(tagStripRe.ReplaceAllString(m[3], "")),
					Source: "ixdzs8",
				})
			}
			if len(books) > 0 {
				results[name] = books
			}
		}
	}

	return &TrendingResponse{Results: results}, nil
}

// ── 下载 ──

func (c *FanqieClient) DownloadBook(bookID string, outputDir string) (*DownloadResult, error) {
	os.MkdirAll(outputDir, 0755)

	// 获取书籍信息
	info, err := c.GetBookInfo(bookID)
	if err != nil || !info.Found {
		return &DownloadResult{Success: false, Error: "未找到书籍"}, nil
	}

	chapters := info.Chapters
	if len(chapters) == 0 {
		return &DownloadResult{Success: false, Error: "无章节数据"}, nil
	}

	// 安全文件名
	safeTitle := regexp.MustCompile(`[\\/:*?"<>|]`).ReplaceAllString(info.Title, "_")
	outputPath := filepath.Join(outputDir, safeTitle+".txt")

	startTime := time.Now()

	// 并发下载
	type result struct {
		idx     int
		title   string
		content string
	}

	maxWorkers := 16
	sem := make(chan struct{}, maxWorkers)
	var wg sync.WaitGroup
	results := make([]result, len(chapters))
	var mu sync.Mutex
	var failedItems []string

	for i, ch := range chapters {
		wg.Add(1)
		go func(idx int, chapter Chapter) {
			defer wg.Done()
			sem <- struct{}{}
			defer func() { <-sem }()

			// 微小随机延迟
			time.Sleep(time.Duration(50+rand.Intn(100)) * time.Millisecond)

			content := c.fetchChapter(bookID, chapter.ItemID)
			mu.Lock()
			if content != "" {
				results[idx] = result{idx: idx, title: chapter.Title, content: content}
			} else {
				failedItems = append(failedItems, fmt.Sprintf("%s(空)", chapter.Title))
			}
			mu.Unlock()
		}(i, ch)
	}
	wg.Wait()

	// 按序写入
	sort.Slice(results, func(i, j int) bool { return results[i].idx < results[j].idx })

	f, err := os.Create(outputPath)
	if err != nil {
		return nil, err
	}
	defer f.Close()

	fmt.Fprintf(f, "《%s》作者：%s\n\n", info.Title, info.Author)
	totalChars := 0
	downloaded := 0
	for _, r := range results {
		if r.content == "" {
			continue
		}
		content := strings.TrimSpace(r.content)
		fmt.Fprintf(f, "%s\n\n%s\n\n", r.title, content)
		for _, ch := range content {
			if ch >= 0x4e00 && ch <= 0x9fff {
				totalChars++
			}
		}
		downloaded++
	}

	elapsed := time.Since(startTime).Seconds()

	return &DownloadResult{
		Success:       true,
		Title:         info.Title,
		Path:          outputPath,
		CNChars:       totalChars,
		TotalChapters: len(chapters),
		Downloaded:    downloaded,
		FailedCount:   len(failedItems),
		Failed:        failedItems,
		ElapsedSec:    elapsed,
		Method:        "proxy_api",
	}, nil
}

func (c *FanqieClient) fetchChapter(bookID, itemID string) string {
	// ① 代理 API (tt.sjmyzq.cn)
	proxyURL := fmt.Sprintf("%s?item_id=%s", FQ_PROXY, itemID)
	resp, err := c.get(proxyURL)
	if err == nil {
		body := readBody(resp)
		var data struct {
			Code int `json:"code"`
			Data struct {
				Content string `json:"content"`
			} `json:"data"`
		}
		if json.Unmarshal([]byte(body), &data) == nil && data.Code == 200 {
			content := data.Data.Content
			if content != "" {
				// 清理 HTML 标签
				content = tagStripRe.ReplaceAllString(content, "")
				content = strings.ReplaceAll(content, "\\n", "\n")
				content = strings.ReplaceAll(content, "\\t", " ")
				// 合并过多空行
				multiNL := regexp.MustCompile(`\n{3,}`)
				content = multiNL.ReplaceAllString(content, "\n\n")
				// 检查中文含量
				cnCount := 0
				for _, ch := range content {
					if ch >= 0x4e00 && ch <= 0x9fff {
						cnCount++
					}
				}
				if cnCount >= 200 {
					return strings.TrimSpace(content)
				}
			}
		}
	}

	// ② INIT_STATE 回退
	fqURL := fmt.Sprintf("%s/reader/%s?itemId=%s", BASE_FQ, bookID, itemID)
	resp, err = c.get(fqURL)
	if err == nil {
		html := readBody(resp)
		state := extractInitState(html)
		if state != nil {
			if reader, ok := state["reader"].(map[string]interface{}); ok {
				if chData, ok := reader["chapterData"].(map[string]interface{}); ok {
					if content, ok := chData["content"].(string); ok && len(content) > 200 {
						decoded := tagStripRe.ReplaceAllString(content, "\n")
						decoded = tagStripRe.ReplaceAllString(decoded, "")
						cnCount := 0
						for _, ch := range decoded {
							if ch >= 0x4e00 && ch <= 0x9fff {
								cnCount++
							}
						}
						if cnCount >= 200 {
							return strings.TrimSpace(decoded)
						}
					}
				}
			}
		}
	}

	return ""
}

// ── 工具 ──

func getStr(m map[string]interface{}, key string) string {
	if v, ok := m[key]; ok {
		switch val := v.(type) {
		case string:
			return val
		default:
			return fmt.Sprint(val)
		}
	}
	return ""
}

// ── 跨平台共享工具 ──

type ChapterResult struct {
	Title   string
	Content string
}

func cnCount(s string) int {
	n := 0
	for _, ch := range s {
		if ch >= 0x4e00 && ch <= 0x9fff { n++ }
	}
	return n
}

func stripTags(s string) string {
	return regexp.MustCompile(`<[^>]+>`).ReplaceAllString(s, "")
}

func safeFilename(s string) string {
	return regexp.MustCompile(`[\\/:*?"<>|]`).ReplaceAllString(s, "_")
}

func mkdir(path string) { _ = os.MkdirAll(path, 0755) }

func now() float64 { return float64(time.Now().UnixNano()) / 1e9 }

func since(start float64) float64 { return float64(time.Now().UnixNano())/1e9 - start }

func msSleep(ms int) { time.Sleep(time.Duration(ms) * time.Millisecond) }

func writeResults(path, title, author string, chapters []ChapterResult, totalChars int) {
	f, _ := os.Create(path)
	if f == nil { return }
	defer f.Close()
	fmt.Fprintf(f, "《%s》作者：%s\n\n", title, author)
	for _, r := range chapters {
		fmt.Fprintf(f, "%s\n\n%s\n\n", r.Title, strings.TrimSpace(r.Content))
	}
}
