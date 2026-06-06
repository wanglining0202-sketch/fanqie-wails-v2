package main

import (
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"strings"
)

const activationSalt = "FQN0v3lD0wnl0ad3r_2026"

// 预生成的长哈希码（SHA256 盐+序号）
var validHashes = func() map[string]bool {
	hashes := make(map[string]bool)
	for i := 1; i <= 16; i++ {
		code := sha256Hex(activationSalt + fmt.Sprintf("%04d", i))
		hashes[code] = true
	}
	return hashes
}()

type activationState struct {
	Activated bool   `json:"activated"`
	Code      string `json:"code"`
	MachineID string `json:"machine_id"`
}

func activationPath() string {
	dir, _ := os.UserConfigDir()
	appDir := filepath.Join(dir, "fanqie-wails")
	os.MkdirAll(appDir, 0755)
	return filepath.Join(appDir, "activation.json")
}

func loadActivation() *activationState {
	data, err := os.ReadFile(activationPath())
	if err != nil {
		return &activationState{}
	}
	var s activationState
	if json.Unmarshal(data, &s) == nil {
		return &s
	}
	return &activationState{}
}

func saveActivation(s *activationState) error {
	data, err := json.Marshal(s)
	if err != nil {
		return err
	}
	return os.WriteFile(activationPath(), data, 0644)
}

// IsActivated 是否已激活
func IsActivated() bool {
	return loadActivation().Activated
}

// validateShortCode 验证自校验短码
// 格式: XXXX-XXXX-XXXX-XXXX (19字符含分隔符 → 16字符原始码)
// 规则: 去掉分隔符 → 16字符 → 前15位做 SHA256(盐+前缀) → 取第一个合法字符 = 校验位
func validateShortCode(raw string) bool {
	code := strings.ToUpper(strings.ReplaceAll(raw, "-", ""))
	if len(code) != 16 || !strings.HasPrefix(code, "FQN") {
		return false
	}
	prefix := code[:15]
	expected := checksumChar(prefix)
	return code[15] == expected
}

func checksumChar(prefix string) byte {
	h := sha256Hex(activationSalt + prefix)
	for _, c := range h {
		if (c >= 'A' && c <= 'Z') || (c >= '2' && c <= '9') {
			return byte(c)
		}
	}
	return 'X'
}

// Activate 验证注册码
func Activate(code string) error {
	if code == "" {
		return fmt.Errorf("注册码不能为空")
	}

	// 1. 自校验短码
	if validateShortCode(code) {
		s := &activationState{
			Activated: true,
			Code:      maskCode(code),
			MachineID: machineID(),
		}
		return saveActivation(s)
	}

	// 2. 长哈希码
	if validHashes[code] {
		s := &activationState{
			Activated: true,
			Code:      maskCode(code),
			MachineID: machineID(),
		}
		return saveActivation(s)
	}

	return fmt.Errorf("注册码无效")
}

func maskCode(code string) string {
	if len(code) <= 4 {
		return "****"
	}
	return code[:4] + "****" + code[len(code)-4:]
}

func machineID() string {
	host, _ := os.Hostname()
	hash := sha256Hex("fanqie-wails-" + host)
	return hash[:12]
}

func sha256Hex(s string) string {
	h := sha256.Sum256([]byte(s))
	return hex.EncodeToString(h[:])
}
