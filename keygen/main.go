package main

import (
	"crypto/sha256"
	"encoding/hex"
	"fmt"
	"math/rand"
	"os"
	"strconv"
	"strings"
)

const salt = "FQN0v3lD0wnl0ad3r_2026"

// 短码字符集（34个, 去掉容易混淆的 0/O/1/I/L 和校验冲突的字符）
const chars = "ABCDEFGHJKMNPQRSTUVWXYZ23456789"

func sha256Hex(s string) string {
	h := sha256.Sum256([]byte(s))
	return hex.EncodeToString(h[:])
}

// checksumChar 从 SHA256 哈希中提取一个校验字符
func checksumChar(prefix string) byte {
	h := sha256Hex(salt + prefix)
	// 取第一个 A-Z 或 2-9 的字符
	for _, c := range h {
		if (c >= 'A' && c <= 'Z') || (c >= '2' && c <= '9') {
			return byte(c)
		}
	}
	return 'X'
}

// genShortCode 生成自校验短码: FQN + 12随机 + 1校验 = 16字符
// 显示格式: XXXX-XXXX-XXXX-XXXX (19字符含分隔符)
func genShortCode() string {
	// 生成 12 个随机字符
	var raw strings.Builder
	raw.WriteString("FQN")
	for i := 0; i < 12; i++ {
		raw.WriteByte(chars[rand.Intn(len(chars))])
	}
	prefix := raw.String() // "FQN" + 12 chars = 15 chars

	// 第 16 位是校验字符
	check := checksumChar(prefix)
	full := prefix + string(check) // 16 chars total

	// 格式化为 XXXX-XXXX-XXXX-XXXX
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
	count := 5
	if len(os.Args) > 1 {
		if n, err := strconv.Atoi(os.Args[1]); err == nil && n > 0 {
			count = n
		}
	}

	fmt.Println("═════════════════════════════════════")
	fmt.Println("  番茄小说下载器 v1 — 注册码生成器")
	fmt.Println("═════════════════════════════════════")
	fmt.Println()

	fmt.Printf("── 短码（%d 个，自校验，可直接使用）──\n", count)
	fmt.Println()
	for i := 0; i < count; i++ {
		fmt.Printf("  %s\n", genShortCode())
	}

	fmt.Println()
	fmt.Printf("── SHA256 长码（%d 个）──\n", count)
	fmt.Println()
	for i := 1; i <= count; i++ {
		hash := sha256Hex(salt + fmt.Sprintf("%04d", i))
		fmt.Printf("  %s\n", hash)
	}

	fmt.Println()
	fmt.Println("短码自校验 → 任意 keygen 生成的短码都能直接激活，无需修改代码。")
	fmt.Println("长码 → 需手动添加到 activation.go 的 validHashes 中。")
	fmt.Println()
	fmt.Printf("  用法: keygen.exe [数量]  默认 5 个\n")
}
