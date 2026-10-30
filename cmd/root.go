// Package cmd 提供命令行接口
package cmd

import (
	"bufio"
	"fmt"
	"os"
	"strconv"
	"strings"
	"time"

	"mechrevo-perfmode/core"
)

const (
	appName    = "perfmode"
	appVersion = "2.0.0"
)

// Run 是 CLI 入口
func Run() {
	args := os.Args[1:]
	if len(args) == 0 {
		printUsage()
		os.Exit(1)
	}

	sub := args[0]
	rest := args[1:]

	switch sub {
	case "status", "s", "get":
		cmdStatus()

	case "turbo", "t", "performance":
		cmdSet(core.ModeTurbo)

	case "balance", "b", "balanced", "normal":
		cmdSet(core.ModeBalance)

	case "silence", "silent", "quiet", "powersave":
		cmdSet(core.ModeSilence)

	case "set":
		if len(rest) == 0 {
			fail("用法: %s set <turbo|balance|silence>", appName)
		}
		m, err := core.ParseMode(rest[0])
		if err != nil {
			fail("%v", err)
		}
		cmdSet(m)

	case "list", "ls":
		cmdList()

	case "osd":
		cmdOSD(rest)

	case "osd-stop", "osdstop":
		cmdOSDStop()

	case "backlight", "bl":
		cmdBacklight(rest)

	case "autostart":
		cmdAutostart(rest)

	case "ec":
		cmdEC()

	case "offset":
		cmdOffset(rest)

	case "probe":
		core.Probe()

	case "calibrate", "cal":
		cmdCalibrate()

	case "watch":
		cmdWatch(rest)

	case "help", "h", "--help", "-h":
		printUsage()

	case "--version", "-v", "version":
		fmt.Printf("%s v%s\n", appName, appVersion)

	default:
		fmt.Fprintf(os.Stderr, "未知命令: %q\n\n", sub)
		printUsage()
		os.Exit(1)
	}
}

func fail(format string, a ...any) {
	fmt.Fprintf(os.Stderr, "错误: "+format+"\n", a...)
	os.Exit(1)
}

// open 打开后端
func open() (core.Backend, core.Config) {
	cfg := core.LoadConfig()
	b, err := core.OpenBackend(cfg)
	if err != nil {
		fmt.Fprintf(os.Stderr, "无法访问性能模式接口:\n  %v\n\n", err)
		fmt.Fprintln(os.Stderr, "排查步骤:")
		fmt.Fprintln(os.Stderr, "  1. 运行 perfmode probe 查看详细诊断")
		fmt.Fprintln(os.Stderr, "  2. 运行 perfmode calibrate 校准 EC 偏移")
		os.Exit(1)
	}
	return b, cfg
}

func cmdStatus() {
	b, cfg := open()
	defer b.Close()

	raw, err := b.GetRaw()
	if err != nil {
		fail("读取失败: %v", err)
	}
	mode, ok := cfg.Mapping.FromRaw(raw)
	if ok {
		fmt.Printf("当前模式: %s (%s)\n", mode.Label(), mode)
	} else {
		fmt.Printf("当前模式: 未知\n")
	}
	fmt.Printf("原始值:   %d (0x%02X)\n", raw, raw)
	fmt.Printf("接口:     %s\n", b.Name())
}

func cmdSet(m core.Mode) {
	b, cfg := open()
	defer b.Close()

	raw := cfg.Mapping.Raw(m)
	if err := b.SetRaw(raw); err != nil {
		fail("切换到 %s 失败: %v", m.Label(), err)
	}
	fmt.Printf("已切换到 %s (写入 %d)\n", m.Label(), raw)

	// 回读校验
	time.Sleep(80 * time.Millisecond)
	if got, err := b.GetRaw(); err == nil {
		if got != raw {
			fmt.Printf("警告: 回读值为 %d，与写入值不一致\n", got)
		}
	}
}

func cmdList() {
	cfg := core.LoadConfig()
	fmt.Println("可用模式:")
	fmt.Println()
	for _, m := range []core.Mode{core.ModeSilence, core.ModeBalance, core.ModeTurbo} {
		fmt.Printf("  %-8s %s  (原始值 %d)\n", m, m.Label(), cfg.Mapping.Raw(m))
	}
	fmt.Println()
	fmt.Printf("用法: %s <模式名>\n", appName)
}

// cmdEC 打印已知 EC 字段的当前值
func cmdEC() {
	ec, err := core.OpenECDirect()
	if err != nil {
		fail("打开 EC 失败: %v", err)
	}
	defer ec.Close()

	snap, err := core.ReadSensors(ec)
	if err != nil {
		fail("读取失败: %v", err)
	}
	fmt.Printf("%-6s %-4s %-6s %s\n", "偏移", "名称", "值", "说明")
	for _, f := range core.KnownECFields {
		v, ok := snap.Values[f.Offset]
		if !ok {
			continue
		}
		extra := ""
		switch f.Offset {
		case 0x9B, 0x9C, 0x9D, 0x9E:
			extra = "（见下方转速）"
		default:
			if f.Unit != "" {
				extra = fmt.Sprintf(" %s", f.Unit)
			}
		}
		fmt.Printf("0x%02X   %-4s %3d    %s%s\n", f.Offset, f.Name, v, f.Desc, extra)
	}
	fmt.Println()
	for i, rpm := range snap.Fans {
		fmt.Printf("风扇 %d: %d rpm\n", i+1, rpm)
	}
}

// cmdOffset 手动设置 EC 偏移
func cmdOffset(args []string) {
	if len(args) == 0 {
		fail("用法: %s offset <十六进制偏移>", appName)
	}
	line := strings.TrimPrefix(strings.ToLower(strings.TrimSpace(args[0])), "0x")
	off, err := strconv.ParseUint(line, 16, 8)
	if err != nil {
		fail("偏移格式错误: %v", err)
	}
	cfg := core.LoadConfig()
	v := int(off)
	cfg.ECOffset = &v
	if err := core.SaveConfig(cfg); err != nil {
		fail("保存配置失败: %v", err)
	}
	fmt.Printf("EC 偏移已设为 0x%02X (%s)\n", off, core.ConfigPath())
}

// cmdWatch 轮询监视模式变化
func cmdWatch(args []string) {
	interval := 500 * time.Millisecond
	if len(args) > 0 {
		if ms, err := strconv.Atoi(args[0]); err == nil && ms >= 50 {
			interval = time.Duration(ms) * time.Millisecond
		}
	}

	b, cfg := open()
	defer b.Close()

	last, err := b.GetRaw()
	if err != nil {
		fail("读取失败: %v", err)
	}
	label := func(v uint32) string {
		if m, ok := cfg.Mapping.FromRaw(v); ok {
			return fmt.Sprintf("%s(%d)", m.Label(), v)
		}
		return fmt.Sprintf("未知(%d)", v)
	}
	fmt.Printf("当前 %s，开始监视（Ctrl+C 退出）...\n", label(last))

	for {
		time.Sleep(interval)
		cur, err := b.GetRaw()
		if err != nil {
			continue
		}
		if cur == last {
			continue
		}
		// 过滤与 ACPI 驱动抢总线造成的脏值
		if cur > 2 {
			continue
		}
		fmt.Printf("%s  %s -> %s\n", time.Now().Format("15:04:05"), label(last), label(cur))
		last = cur
	}
}

// cmdCalibrate 引导用户定位 EC 中的性能模式字段
func cmdCalibrate() {
	ec, err := core.OpenECDirect()
	if err != nil {
		fail("打开 EC 失败: %v", err)
	}
	defer ec.Close()

	fmt.Println("=== 校准 ===")
	fmt.Println("将对比两次 EC 快照，找出随 Fn+F1 变化的字节。")
	fmt.Println()
	fmt.Println("读取第一次快照...")
	before, err := ec.Snapshot()
	if err != nil {
		fail("快照失败: %v", err)
	}

	rd := bufio.NewReader(os.Stdin)
	for round := 1; round <= 2; round++ {
		fmt.Printf("请按一次 Fn+F1 切换性能模式，然后回车 (第 %d/2 轮) ", round)
		rd.ReadString('\n')

		after, err := ec.Snapshot()
		if err != nil {
			fail("快照失败: %v", err)
		}
		fmt.Println("变化:")
		n := 0
		for i := 0; i < 256; i++ {
			if after[i] != before[i] {
				fmt.Printf("  EC[0x%02X]  %d -> %d\n", i, before[i], after[i])
				n++
			}
		}
		if n == 0 {
			fmt.Println("  （没有变化）")
		}
		before = after
	}

	fmt.Print("\n根据上面的输出，输入性能模式字段的十六进制偏移（例如 31）: ")
	line, _ := rd.ReadString('\n')
	line = strings.TrimSpace(strings.TrimPrefix(strings.ToLower(strings.TrimSpace(line)), "0x"))
	off, err := strconv.ParseUint(line, 16, 8)
	if err != nil {
		fail("偏移格式错误: %v", err)
	}

	cfg := core.LoadConfig()
	v := int(off)
	cfg.ECOffset = &v
	if err := core.SaveConfig(cfg); err != nil {
		fail("保存配置失败: %v", err)
	}
	fmt.Printf("已保存: EC 偏移 0x%02X -> %s\n", off, core.ConfigPath())
}

func printUsage() {
	fmt.Printf(`%s v%s — 机械革命无界14 Pro 性能模式切换

用法:
  %s <命令> [参数]

命令:
  status              查看当前性能模式
  turbo               切换到性能模式
  balance             切换到均衡模式
  silence             切换到静音模式
  set <模式>           设置指定模式
  list                列出可用模式
  watch [毫秒]         监视模式变化（默认 500ms）
  calibrate           校准 EC 字段偏移（首次使用需要）
  probe               诊断：打印设备和接口信息
  help                显示帮助

示例:
  %s status
  %s turbo
  %s set silence
  %s watch 300

`, appName, appVersion, appName, appName, appName, appName, appName)
}
