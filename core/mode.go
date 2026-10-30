// 性能模式的逻辑模型与「逻辑模式 <-> 固件原始值」映射。
//
// 不同模具的固件对三个模式的编号并不一致，所以原始值单独抽出来，
// 允许通过配置文件覆盖。
package core

import (
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"strings"
)

// Mode 是逻辑上的性能模式，与固件原始值无关
type Mode int

const (
	ModeTurbo   Mode = iota // 性能 / 狂暴
	ModeBalance             // 均衡
	ModeSilence             // 静音
)

// String 返回英文标识
func (m Mode) String() string {
	switch m {
	case ModeTurbo:
		return "turbo"
	case ModeBalance:
		return "balance"
	case ModeSilence:
		return "silence"
	}
	return "unknown"
}

// Label 返回中文名
func (m Mode) Label() string {
	switch m {
	case ModeTurbo:
		return "性能模式"
	case ModeBalance:
		return "均衡模式"
	case ModeSilence:
		return "静音模式"
	}
	return "未知模式"
}

// ParseMode 解析用户输入的模式名
func ParseMode(s string) (Mode, error) {
	switch strings.ToLower(strings.TrimSpace(s)) {
	case "turbo", "t", "performance", "perf", "high", "狂暴", "性能":
		return ModeTurbo, nil
	case "balance", "b", "balanced", "normal", "均衡":
		return ModeBalance, nil
	case "silence", "s", "silent", "quiet", "low", "powersave", "静音", "安静":
		return ModeSilence, nil
	}
	return -1, fmt.Errorf("未知模式 %q（可用: turbo / balance / silence）", s)
}

// ============================================================
// 映射
// ============================================================

// Mapping 描述逻辑模式与固件原始值的对应关系
type Mapping struct {
	Turbo   uint32 `json:"turbo"`
	Balance uint32 `json:"balance"`
	Silence uint32 `json:"silence"`
}

// DefaultMapping 是无界14 Pro 固件使用的映射
//
//	0 = turbo, 1 = balance, 2 = silence
//
// 依据：DSDT 里 FNQS() 把 ITSM 映射到 THMP 热表，
// THMP[0]=65W/54W, THMP[1]=54W/50W, THMP[3]=45W/25W，
// 即 ITSM 0/1/2 依次对应 65W/54W/45W 三档功耗墙。
var DefaultMapping = Mapping{Turbo: 0, Balance: 1, Silence: 2}

// DefaultECOffset 是 ITSM 字段在 EC RAM 中的偏移
//
// 依据：DSDT 中 OperationRegion(ERAM) 的 Field 定义，
// Offset(0xE0) 之后依次是 DGPU/SSDK/KBBL/BPWM 各 8 位，ITSM 紧随其后 => 0xE4。
const DefaultECOffset = 0xE4

// Raw 返回逻辑模式对应的固件值
func (m Mapping) Raw(mode Mode) uint32 {
	switch mode {
	case ModeTurbo:
		return m.Turbo
	case ModeBalance:
		return m.Balance
	case ModeSilence:
		return m.Silence
	}
	return 0xFFFFFFFF
}

// FromRaw 把固件值翻译回逻辑模式
func (m Mapping) FromRaw(v uint32) (Mode, bool) {
	switch v {
	case m.Turbo:
		return ModeTurbo, true
	case m.Balance:
		return ModeBalance, true
	case m.Silence:
		return ModeSilence, true
	}
	return -1, false
}

// Valid 检查映射是否构成 0/1/2 的一个排列
func (m Mapping) Valid() bool {
	seen := map[uint32]bool{}
	for _, v := range []uint32{m.Turbo, m.Balance, m.Silence} {
		if v > 2 || seen[v] {
			return false
		}
		seen[v] = true
	}
	return len(seen) == 3
}

// ============================================================
// 配置
// ============================================================

// OSDConfig 是后台提示程序的设置
type OSDConfig struct {
	IntervalMs  int  `json:"interval_ms,omitempty"`  // 轮询间隔
	StayMs      int  `json:"stay_ms,omitempty"`      // 弹窗停留时长
	ShowSensors bool `json:"show_sensors,omitempty"` // 弹窗里附带温度/转速
}

// Config 是持久化配置
type Config struct {
	Mapping Mapping `json:"mapping"`
	// ECOffset 是性能模式字段在 EC RAM 中的偏移（字节），nil 表示尚未校准
	ECOffset *int      `json:"ec_offset,omitempty"`
	OSD      OSDConfig `json:"osd,omitempty"`
}

// DefaultConfig 返回默认配置
func DefaultConfig() Config {
	off := DefaultECOffset
	return Config{
		Mapping:  DefaultMapping,
		ECOffset: &off,
		OSD:      OSDConfig{IntervalMs: 300, StayMs: 1800},
	}
}

// ConfigPath 返回配置文件路径
func ConfigPath() string {
	dir, err := os.UserConfigDir()
	if err != nil || dir == "" {
		dir = "."
	}
	return filepath.Join(dir, "mechrevo-perfmode", "config.json")
}

// LoadConfig 读取配置，文件不存在时返回默认配置
func LoadConfig() Config {
	cfg := DefaultConfig()
	data, err := os.ReadFile(ConfigPath())
	if err != nil {
		return cfg
	}
	var loaded Config
	if err := json.Unmarshal(data, &loaded); err != nil {
		return cfg
	}
	if loaded.Mapping.Valid() {
		cfg.Mapping = loaded.Mapping
	}
	if loaded.OSD.IntervalMs > 0 {
		cfg.OSD.IntervalMs = loaded.OSD.IntervalMs
	}
	if loaded.OSD.StayMs > 0 {
		cfg.OSD.StayMs = loaded.OSD.StayMs
	}
	cfg.OSD.ShowSensors = loaded.OSD.ShowSensors
	return cfg
}

// SaveConfig 写入配置
func SaveConfig(cfg Config) error {
	p := ConfigPath()
	if err := os.MkdirAll(filepath.Dir(p), 0o755); err != nil {
		return err
	}
	data, err := json.MarshalIndent(cfg, "", "  ")
	if err != nil {
		return err
	}
	return os.WriteFile(p, append(data, '\n'), 0o644)
}
