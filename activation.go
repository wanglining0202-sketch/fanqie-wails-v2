package main

import (
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
)

// ── 注册码系统 ──
// 离线验证：SHA256(固定盐 + 序号) 生成注册码
// 验证时比对预置哈希列表

const activationSalt = "FQN0v3lD0wnl0ad3r_2026"

// 预生成的有效注册码哈希（16个）
// 生成方式: SHA256(activationSalt + "0001") 等
var validHashes = func() map[string]bool {
	hashes := make(map[string]bool)
	for i := 1; i <= 16; i++ {
		code := sha256Hex(activationSalt + fmt.Sprintf("%04d", i))
		hashes[code] = true
	}
	return hashes
}()

// 预先生成一份明文注册码供分发
// FQ-XXXX-XXXX-XXXX 格式的短码也映射到这里
var validShortCodes = map[string]bool{
	"FQN1-V4K8-X9M2-W7P3": true,
	"FQN2-R6T9-Y1H4-Q5L8": true,
	"FQN3-J2C7-B8N6-Z3X1": true,
	"FQN4-K9W5-D3F7-A6E2": true,
	"FQN5-M8S4-P1T6-G9R3": true,
}

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

// IsActivated 检查是否已激活
func IsActivated() bool {
	return loadActivation().Activated
}

// Activate 验证注册码
func Activate(code string) error {
	if code == "" {
		return fmt.Errorf("注册码不能为空")
	}

	// 1. 短码直接匹配
	if validShortCodes[code] {
		s := &activationState{
			Activated: true,
			Code:      maskCode(code),
			MachineID: machineID(),
		}
		return saveActivation(s)
	}

	// 2. 长哈希码匹配
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
