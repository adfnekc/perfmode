// 后台 OSD：监视 EC 里的性能模式，变化时弹提示；托盘图标提供菜单。
package cmd

import (
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"strconv"
	"strings"
	"sync"
	"time"
	"unsafe"

	"golang.org/x/sys/windows"

	"mechrevo-perfmode/core"
	"mechrevo-perfmode/osd"
)

var (
	user32         = windows.NewLazySystemDLL("user32.dll")
	kernel32       = windows.NewLazySystemDLL("kernel32.dll")
	procGetConsole = kernel32.NewProc("GetConsoleWindow")
	procShowWindow = user32.NewProc("ShowWindow")
	procMessageBox = user32.NewProc("MessageBoxW")
)

const mutexName = "Local\\perfmode-osd-singleton"

func pidPath() string {
	return filepath.Join(filepath.Dir(core.ConfigPath()), "osd.pid")
}

func hideConsole() {
	h, _, _ := procGetConsole.Call()
	if h != 0 {
		procShowWindow.Call(h, 0)
	}
}

func messageBox(title, text string) {
	t, _ := windows.UTF16PtrFromString(title)
	m, _ := windows.UTF16PtrFromString(text)
	procMessageBox.Call(0, uintptr(unsafe.Pointer(m)), uintptr(unsafe.Pointer(t)), 0x10)
}

// 托盘菜单 ID
const (
	menuModeBase      = 100
	menuSensorsToggle = 200
	menuCurrent       = 201
	menuBacklightBase = 210
	menuAutostart     = 300
	menuQuit          = 400
)

type osdState struct {
	mu   sync.Mutex
	raw  uint32
	last uint32
}

func (s *osdState) get() uint32 {
	s.mu.Lock()
	defer s.mu.Unlock()
	return s.raw
}

func (s *osdState) set(v uint32) {
	s.mu.Lock()
	s.raw = v
	s.last = v
	s.mu.Unlock()
}

// cmdOSD 启动后台提示程序
func cmdOSD(args []string) {
	cfg := core.LoadConfig()
	interval := time.Duration(cfg.OSD.IntervalMs) * time.Millisecond
	if interval <= 0 {
		interval = 300 * time.Millisecond
	}
	stayMs := cfg.OSD.StayMs
	if stayMs <= 0 {
		stayMs = 1800
	}
	for i := 0; i < len(args); i++ {
		switch args[i] {
		case "--interval":
			if i+1 < len(args) {
				if ms, err := strconv.Atoi(args[i+1]); err == nil && ms >= 50 {
					interval = time.Duration(ms) * time.Millisecond
				}
				i++
			}
		case "--stay":
			if i+1 < len(args) {
				if ms, err := strconv.Atoi(args[i+1]); err == nil && ms >= 300 {
					stayMs = ms
				}
				i++
			}
		}
	}

	// 单实例：已经有后台在跑就退出
	name, _ := windows.UTF16PtrFromString(mutexName)
	mtx, err := windows.CreateMutex(nil, false, name)
	if err != nil && errors.Is(err, windows.ERROR_ALREADY_EXISTS) {
		messageBox("perfmode", "后台提示程序已经在运行了。")
		return
	}
	if mtx != 0 {
		defer windows.CloseHandle(mtx)
	}

	b, err := core.OpenBackend(cfg)
	if err != nil {
		messageBox("perfmode", "无法访问 EC：\n"+err.Error())
		os.Exit(1)
	}
	defer b.Close()

	ec, err := core.OpenECDirect()
	if err != nil {
		messageBox("perfmode", "无法打开 EC：\n"+err.Error())
		os.Exit(1)
	}
	defer ec.Close()

	initial, err := b.GetRaw()
	if err != nil {
		messageBox("perfmode", "读取性能模式失败：\n"+err.Error())
		os.Exit(1)
	}
	st := &osdState{raw: initial, last: initial}

	_ = os.MkdirAll(filepath.Dir(pidPath()), 0o755)
	_ = os.WriteFile(pidPath(), []byte(strconv.Itoa(os.Getpid())), 0o644)
	defer os.Remove(pidPath())

	logPath := filepath.Join(filepath.Dir(pidPath()), "osd.log")
	if logFile, err := os.OpenFile(logPath, os.O_CREATE|os.O_WRONLY|os.O_TRUNC, 0o644); err == nil {
		defer logFile.Close()
		osd.Logf = func(format string, a ...any) { fmt.Fprintf(logFile, format+"\n", a...) }
	}

	hideConsole()

	updates := make(chan osd.Item, 4)
	notify := func(mode, title string, lines ...string) {
		select {
		case updates <- osd.Item{Mode: mode, Title: title, Lines: lines}:
		default:
		}
	}
	modeKey := func(v uint32) string {
		if m, ok := cfg.Mapping.FromRaw(v); ok {
			return m.String()
		}
		return "app"
	}

	// 轮询模式变化（去抖：连续两次读到同一新值才认）
	go func() {
		candidate, hits := uint32(0xFFFFFFFF), 0
		for {
			time.Sleep(interval)
			cur, err := b.GetRaw()
			if err != nil || cur > 2 || cur == st.get() {
				candidate, hits = 0xFFFFFFFF, 0
				continue
			}
			if cur != candidate {
				candidate, hits = cur, 1
				continue
			}
			hits++
			if hits < 2 {
				continue
			}
			candidate, hits = 0xFFFFFFFF, 0
			st.set(cur)
			m, ok := cfg.Mapping.FromRaw(cur)
			if !ok {
				continue
			}
			notify(m.String(), m.Label(), popupLines(ec, cfg, m)...)
			osd.SetTrayMode(m.String())
			refreshTip(ec, cfg, st)
		}
	}()

	// 定期刷新托盘提示
	go func() {
		for {
			time.Sleep(3 * time.Second)
			refreshTip(ec, cfg, st)
		}
	}()

	osd.MenuProvider = func() []osd.MenuItem {
		cur := st.get()
		items := []osd.MenuItem{
			{ID: menuModeBase + int(core.ModeTurbo), Text: "性能模式", Checked: cur == cfg.Mapping.Raw(core.ModeTurbo)},
			{ID: menuModeBase + int(core.ModeBalance), Text: "均衡模式", Checked: cur == cfg.Mapping.Raw(core.ModeBalance)},
			{ID: menuModeBase + int(core.ModeSilence), Text: "静音模式", Checked: cur == cfg.Mapping.Raw(core.ModeSilence)},
			{Sep: true},
		}
		if bl, err := core.ReadBacklight(ec); err == nil {
			names := []string{"背光 关", "背光 暗", "背光 亮"}
			for i, n := range names {
				items = append(items, osd.MenuItem{
					ID: menuBacklightBase + i, Text: n, Checked: int(bl) == i,
				})
			}
		}
		items = append(items,
			osd.MenuItem{Sep: true},
			osd.MenuItem{ID: menuSensorsToggle, Text: "弹窗附带温度/转速", Checked: cfg.OSD.ShowSensors},
			osd.MenuItem{ID: menuCurrent, Text: "显示当前模式"},
			osd.MenuItem{Sep: true},
		)
		text := "开机自启"
		if autostartEnabled() {
			text = "开机自启 ✓"
		}
		items = append(items,
			osd.MenuItem{ID: menuAutostart, Text: text},
			osd.MenuItem{ID: menuQuit, Text: "退出"})
		return items
	}

	osd.CommandHandler = func(id int) {
		switch {
		case id > menuModeBase && id < menuModeBase+10:
			m := core.Mode(id - menuModeBase)
			raw := cfg.Mapping.Raw(m)
			if err := b.SetRaw(raw); err != nil {
				messageBox("perfmode", "切换失败：\n"+err.Error())
				return
			}
			st.set(raw)
			notify(m.String(), m.Label(), popupLines(ec, cfg, m)...)
			osd.SetTrayMode(m.String())
			refreshTip(ec, cfg, st)

		case id >= menuBacklightBase && id < menuBacklightBase+3:
			if err := core.SetBacklight(ec, byte(id-menuBacklightBase)); err != nil {
				messageBox("perfmode", "设置背光失败：\n"+err.Error())
			}

		case id == menuSensorsToggle:
			cfg.OSD.ShowSensors = !cfg.OSD.ShowSensors
			_ = core.SaveConfig(cfg)

		case id == menuCurrent:
			if m, ok := cfg.Mapping.FromRaw(st.get()); ok {
				notify(m.String(), m.Label(), popupLines(ec, cfg, m)...)
			}

		case id == menuAutostart:
			if err := setAutostart(!autostartEnabled()); err != nil {
				messageBox("perfmode", "设置开机自启失败：\n"+err.Error())
			}

		case id == menuQuit:
			osd.Quit()
		}
	}

	osd.Init()
	refreshTip(ec, cfg, st)
	osd.Run(updates, stayMs, modeKey(initial))
}

// popupLines 生成弹窗正文
func popupLines(ec *core.ECDirect, cfg core.Config, m core.Mode) []string {
	lines := []string{modeSub(m)}
	if !cfg.OSD.ShowSensors {
		return lines
	}
	if snap, err := core.ReadSensors(ec); err == nil {
		lines = append(lines, sensorLine(snap))
	}
	return lines
}

func sensorLine(snap core.ECSnapshot) string {
	f1 := 0
	if len(snap.Fans) > 0 {
		f1 = snap.Fans[0]
	}
	return fmt.Sprintf("%d°C · %d rpm", int(snap.Values[0x18]), f1)
}

// refreshTip 更新托盘悬停提示
func refreshTip(ec *core.ECDirect, cfg core.Config, st *osdState) {
	snap, err := core.ReadSensors(ec)
	if err != nil {
		return
	}
	modeLabel := "?"
	if m, ok := cfg.Mapping.FromRaw(st.get()); ok {
		modeLabel = m.Label()
	}
	f1 := 0
	if len(snap.Fans) > 0 {
		f1 = snap.Fans[0]
	}
	osd.SetTip(fmt.Sprintf("perfmode — %s · %d°C · %d rpm", modeLabel, int(snap.Values[0x18]), f1))
}

// modeSub 生成提示框第二行
func modeSub(m core.Mode) string {
	watts := map[core.Mode]string{
		core.ModeTurbo:   "65W",
		core.ModeBalance: "54W",
		core.ModeSilence: "45W",
	}
	return fmt.Sprintf("%s · 功耗墙 %s", strings.ToUpper(m.String()), watts[m])
}

// cmdOSDStop 结束后台提示程序
func cmdOSDStop() {
	data, err := os.ReadFile(pidPath())
	if err != nil {
		fmt.Println("没有正在运行的后台提示程序")
		return
	}
	pid, err := strconv.Atoi(strings.TrimSpace(string(data)))
	if err != nil || pid <= 0 {
		fmt.Println("PID 文件损坏，已删除")
		os.Remove(pidPath())
		return
	}
	h, err := windows.OpenProcess(windows.PROCESS_TERMINATE, false, uint32(pid))
	if err != nil {
		fmt.Printf("找不到进程 %d（可能已退出）\n", pid)
		os.Remove(pidPath())
		return
	}
	defer windows.CloseHandle(h)
	if err := windows.TerminateProcess(h, 0); err != nil {
		fmt.Printf("结束进程失败: %v\n", err)
		return
	}
	os.Remove(pidPath())
	fmt.Printf("已结束后台提示程序 (PID %d)\n", pid)
}
