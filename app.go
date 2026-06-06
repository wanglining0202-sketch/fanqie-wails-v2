package main

import (
	"context"
	"encoding/json"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"sync"

	"github.com/wailsapp/wails/v2/pkg/runtime"
)

type App struct {
	ctx      context.Context
	client   *FanqieClient
	mu       sync.Mutex
}

func NewApp() *App {
	return &App{}
}

func (a *App) startup(ctx context.Context) {
	a.ctx = ctx
	a.client = NewFanqieClient()
}

// ── 番茄 ──

func (a *App) Search(query string) string {
	result, err := a.client.Search(query)
	if err != nil { return jsonErr(err) }
	return toJSON(result)
}

func (a *App) GetBookInfo(bookID string) string {
	result, err := a.client.GetBookInfo(bookID)
	if err != nil { return jsonErr(err) }
	return toJSON(result)
}

func (a *App) DownloadBook(bookID string, outputDir string) string {
	if outputDir == "" {
		home, _ := os.UserHomeDir()
		outputDir = filepath.Join(home, "Downloads", "LaoWang")
	}
	result, err := a.client.DownloadBook(bookID, outputDir)
	if err != nil { return jsonErr(err) }
	return toJSON(result)
}

// ── 起点 ──

func (a *App) SearchQidian(query string) string {
	qd := NewQidianClient()
	results := qd.SearchQidian(query)
	return toJSON(map[string]interface{}{"results": results, "count": len(results)})
}

func (a *App) GetQidianInfo(bookID string) string {
	qd := NewQidianClient()
	result, err := qd.GetQidianInfo(bookID)
	if err != nil { return jsonErr(err) }
	return toJSON(result)
}

func (a *App) DownloadQidian(bookID string, outputDir string) string {
	if outputDir == "" {
		home, _ := os.UserHomeDir()
		outputDir = filepath.Join(home, "Downloads", "LaoWang")
	}
	qd := NewQidianClient()
	result, err := qd.DownloadQidian(bookID, outputDir)
	if err != nil { return jsonErr(err) }
	return toJSON(result)
}

// ── 飞卢 ──

func (a *App) SearchFeilu(query string) string {
	fl := NewFeiluClient()
	results := fl.SearchFeilu(query)
	return toJSON(map[string]interface{}{"results": results, "count": len(results)})
}

func (a *App) GetFeiluInfo(bookID string) string {
	fl := NewFeiluClient()
	result, err := fl.GetFeiluInfo(bookID)
	if err != nil { return jsonErr(err) }
	return toJSON(result)
}

func (a *App) DownloadFeilu(bookID string, outputDir string) string {
	if outputDir == "" {
		home, _ := os.UserHomeDir()
		outputDir = filepath.Join(home, "Downloads", "LaoWang")
	}
	fl := NewFeiluClient()
	result, err := fl.DownloadFeilu(bookID, outputDir)
	if err != nil { return jsonErr(err) }
	return toJSON(result)
}

// ── 聚合站（搜索引擎定位）──

func (a *App) SearchAgg(query string) string {
	sa := NewSearchAggregator()
	results := sa.Search(query)
	return toJSON(map[string]interface{}{"results": results, "count": len(results)})
}

func (a *App) DownloadAgg(bookID string, outputDir string) string {
	if outputDir == "" {
		home, _ := os.UserHomeDir()
		outputDir = filepath.Join(home, "Downloads", "LaoWang")
	}
	sa := NewSearchAggregator()
	// bookID is actually keyword in agg mode
	result, err := sa.Download(bookID, outputDir)
	if err != nil { return jsonErr(err) }
	return toJSON(result)
}

// ── 通用 ──

func (a *App) GetTrending() string {
	result, err := a.client.GetTrending()
	if err != nil { return jsonErr(err) }
	return toJSON(result)
}

// SelectDirectory 打开目录选择对话框
func (a *App) SelectDirectory() string {
	dir, err := runtime.OpenDirectoryDialog(a.ctx, runtime.OpenDialogOptions{
		Title: "选择下载保存目录",
	})
	if err != nil || dir == "" {
		return ""
	}
	return dir
}

// OpenDirectory 在资源管理器中打开目录
func (a *App) OpenDirectory(path string) {
	if path == "" {
		return
	}
	// 用 Windows 资源管理器打开目录
	cmd := exec.Command("explorer", path)
	cmd.Start()
}

// ── 激活 API ──

// CheckActivation 检查是否已激活（前端启动时调用）
func (a *App) CheckActivation() string {
	return toJSON(map[string]interface{}{
		"activated": IsActivated(),
	})
}

// Activate 验证注册码并激活
func (a *App) Activate(code string) string {
	err := Activate(code)
	if err != nil {
		return jsonErr(err)
	}
	return toJSON(map[string]interface{}{
		"activated": true,
		"message":   "激活成功",
	})
}

// ── 工具函数 ──

func toJSON(v interface{}) string {
	b, err := json.Marshal(v)
	if err != nil {
		return fmt.Sprintf(`{"error":"JSON 序列化失败: %s"}`, err.Error())
	}
	return string(b)
}

func jsonErr(err error) string {
	return fmt.Sprintf(`{"error":"%s"}`, err.Error())
}
