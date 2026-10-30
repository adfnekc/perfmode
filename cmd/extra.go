// 背光控制与开机自启
package cmd

import (
	"fmt"
	"os"
	"strconv"
	"strings"

	"golang.org/x/sys/windows/registry"

	"mechrevo-perfmode/core"
)

const runKeyPath = `Software\Microsoft\Windows\CurrentVersion\Run`
const runValueName = "perfmode-osd"

// cmdBacklight 查看或设置键盘背光
func cmdBacklight(args []string) {
	ec, err := core.OpenECDirect()
	if err != nil {
		fail("打开 EC 失败: %v", err)
	}
	defer ec.Close()

	if len(args) == 0 {
		v, err := core.ReadBacklight(ec)
		if err != nil {
			fail("读取背光失败: %v", err)
		}
		fmt.Printf("键盘背光档位: %d (0=关 1=暗 2=亮)\n", v)
		return
	}

	lv, err := strconv.Atoi(strings.TrimSpace(args[0]))
	if err != nil || lv < 0 || lv >= core.BacklightLevels {
		fail("档位必须是 0、1 或 2")
	}
	if err := core.SetBacklight(ec, byte(lv)); err != nil {
		fail("设置背光失败: %v", err)
	}
	got, err := core.ReadBacklight(ec)
	if err != nil {
		fmt.Printf("已写入 %d（回读失败）\n", lv)
		return
	}
	fmt.Printf("已设置背光为 %d（回读 %d）\n", lv, got)
}

// autostartEnabled 返回是否已开启开机自启
func autostartEnabled() bool {
	k, err := registry.OpenKey(registry.CURRENT_USER, runKeyPath, registry.QUERY_VALUE)
	if err != nil {
		return false
	}
	defer k.Close()
	v, _, err := k.GetStringValue(runValueName)
	return err == nil && v != ""
}

// setAutostart 开启或关闭开机自启
func setAutostart(on bool) error {
	if on {
		exe, err := os.Executable()
		if err != nil {
			return err
		}
		k, _, err := registry.CreateKey(registry.CURRENT_USER, runKeyPath, registry.SET_VALUE)
		if err != nil {
			return err
		}
		defer k.Close()
		return k.SetStringValue(runValueName, fmt.Sprintf("%q osd", exe))
	}
	k, err := registry.OpenKey(registry.CURRENT_USER, runKeyPath, registry.SET_VALUE)
	if err != nil {
		return nil
	}
	defer k.Close()
	_ = k.DeleteValue(runValueName)
	return nil
}

// cmdAutostart 管理开机自启
func cmdAutostart(args []string) {
	if len(args) == 0 {
		fail("用法: %s autostart <on|off|status>", appName)
	}
	switch strings.ToLower(args[0]) {
	case "on":
		exe, err := os.Executable()
		if err != nil {
			fail("取程序路径失败: %v", err)
		}
		k, _, err := registry.CreateKey(registry.CURRENT_USER, runKeyPath, registry.SET_VALUE)
		if err != nil {
			fail("打开注册表失败: %v", err)
		}
		defer k.Close()
		if err := k.SetStringValue(runValueName, fmt.Sprintf("%q osd", exe)); err != nil {
			fail("写入注册表失败: %v", err)
		}
		fmt.Printf("已开启开机自启:\n  %s osd\n", exe)

	case "off":
		k, err := registry.OpenKey(registry.CURRENT_USER, runKeyPath, registry.SET_VALUE)
		if err == nil {
			k.DeleteValue(runValueName)
			k.Close()
		}
		fmt.Println("已关闭开机自启")

	case "status":
		k, err := registry.OpenKey(registry.CURRENT_USER, runKeyPath, registry.QUERY_VALUE)
		if err != nil {
			fmt.Println("开机自启: 关闭")
			return
		}
		defer k.Close()
		v, _, err := k.GetStringValue(runValueName)
		if err != nil || v == "" {
			fmt.Println("开机自启: 关闭")
			return
		}
		fmt.Printf("开机自启: 开启\n  %s\n", v)

	default:
		fail("用法: %s autostart <on|off|status>", appName)
	}
}
