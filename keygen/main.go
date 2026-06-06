package main

import (
	"crypto/sha256"
	"encoding/hex"
	"fmt"
	"math/rand"
	"os"
	"path/filepath"
	"strconv"
	"strings"
	"time"
)

const salt = "FQN0v3lD0wnl0ad3r_2026"
const chars = "ABCDEFGHJKMNPQRSTUVWXYZ23456789"

func sha256Hex(s string) string {
	h := sha256.Sum256([]byte(s))
	return hex.EncodeToString(h[:])
}

func checksumChar(prefix string) byte {
	h := sha256Hex(salt + prefix)
	for _, c := range h {
		if (c >= 'A' && c <= 'Z') || (c >= '2' && c <= '9') {
			return byte(c)
		}
	}
	return 'X'
}

func genShortCode() string {
	var raw strings.Builder
	raw.WriteString("FQN")
	for i := 0; i < 12; i++ {
		raw.WriteByte(chars[rand.Intn(len(chars))])
	}
	prefix := raw.String()
	check := checksumChar(prefix)
	full := prefix + string(check)

	var formatted strings.Builder
	for i := 0; i < 16; i++ {
		if i > 0 && i%4 == 0 {
			formatted.WriteByte('-')
		}
		formatted.WriteByte(full[i])
	}
	return formatted.String()
}

func main() {
	rand.Seed(time.Now().UnixNano())

	count := 5
	if len(os.Args) > 1 {
		if n, err := strconv.Atoi(os.Args[1]); err == nil && n > 0 {
			count = n
		}
	}

	// 输出文件：keygen 所在目录/注册码_YYYYMMDD_HHMMSS.txt
	exeDir := filepath.Dir(os.Args[0])
	ts := time.Now().Format("20060102_150405")
	outPath := filepath.Join(exeDir, fmt.Sprintf("注册码_%s.txt", ts))

	var sb strings.Builder

	sb.WriteString("═════════════════════════════════════\n")
	sb.WriteString("  老王下载 v1 — 注册码生成器\n")
	sb.WriteString(fmt.Sprintf("  生成时间: %s\n", time.Now().Format("2006-01-02 15:04:05")))
	sb.WriteString("═════════════════════════════════════\n\n")

	sb.WriteString(fmt.Sprintf("── 短码（%d 个，自校验，可直接使用）──\n\n", count))
	for i := 0; i < count; i++ {
		sb.WriteString(fmt.Sprintf("  %s\n", genShortCode()))
	}

	sb.WriteString(fmt.Sprintf("\n── SHA256 长码（%d 个）──\n\n", count))
	for i := 1; i <= count; i++ {
		hash := sha256Hex(salt + fmt.Sprintf("%04d", i))
		sb.WriteString(fmt.Sprintf("  %s\n", hash))
	}

	sb.WriteString("\n短码自校验 → 任意 keygen 生成的短码都能直接激活，无需修改代码。\n")

	output := sb.String()
	fmt.Print(output)

	if err := os.WriteFile(outPath, []byte(output), 0644); err != nil {
		fmt.Fprintf(os.Stderr, "\n写入文件失败: %s\n", err)
	} else {
		fmt.Printf("\n已保存到: %s\n", outPath)
	}
}
