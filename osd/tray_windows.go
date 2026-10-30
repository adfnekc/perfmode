// 系统托盘图标与右键菜单
package osd

import (
	"syscall"
	"unsafe"

	"golang.org/x/sys/windows/registry"
)

var (
	procShellNotifyIcon = shell32.NewProc("Shell_NotifyIconW")
	procCreateIconFromRes = user32.NewProc("CreateIconFromResourceEx")
	procDestroyIcon     = user32.NewProc("DestroyIcon")
	procCreatePopupMenu = user32.NewProc("CreatePopupMenu")
	procAppendMenu      = user32.NewProc("AppendMenuW")
	procTrackPopupMenu  = user32.NewProc("TrackPopupMenu")
	procGetCursorPos    = user32.NewProc("GetCursorPos")
	procSetForeground   = user32.NewProc("SetForegroundWindow")
	procDestroyMenu     = user32.NewProc("DestroyMenu")
)

const (
	nimAdd    = 0
	nimModify = 1
	nimDelete = 2

	nifMessage = 0x00000001
	nifIcon    = 0x00000002
	nifTip     = 0x00000004

	wmRButtonUp = 0x0205
	wmLButtonUp = 0x0202

	mfString    = 0x00000000
	mfChecked   = 0x00000008
	mfSeparator = 0x00000800

	tpmRightBtn  = 0x0002
	tpmReturnCmd = 0x0100

	trayUID   = 1
	wmTrayMsg = wmApp + 2
)

type notifyIconData struct {
	CbSize          uint32
	HWnd            uintptr
	UID             uint32
	Flags           uint32
	CallbackMessage uint32
	HIcon           uintptr
	SzTip           [128]uint16
	DwState         uint32
	DwStateMask     uint32
	SzInfo          [256]uint16
	UTimeout        uint32
	SzInfoTitle     [64]uint16
	DwInfoFlags     uint32
	GuidItem        [16]byte
	HBalloonIcon    uintptr
}

type point struct{ X, Y int32 }

var (
	trayHIcon uintptr
	trayMode  = "app"
)

// systemUsesLightTheme 读取系统是否使用浅色任务栏
func systemUsesLightTheme() bool {
	k, err := registry.OpenKey(registry.CURRENT_USER,
		`Software\Microsoft\Windows\CurrentVersion\Themes\Personalize`, registry.QUERY_VALUE)
	if err != nil {
		return false
	}
	defer k.Close()
	v, _, err := k.GetIntegerValue("SystemUsesLightTheme")
	return err == nil && v == 1
}

// trayIcon 用嵌入的 PNG 生成 HICON（Vista+ 支持 PNG 图标资源）
func trayIcon(mode string) uintptr {
	theme := "dark"
	if systemUsesLightTheme() {
		theme = "light"
	}
	key := theme + "_" + mode
	if h, ok := iconCache[key]; ok {
		return h
	}
	b := assetBytes("tray_" + theme + "_" + mode + ".png")
	if len(b) == 0 {
		return 0
	}
	h, _, _ := procCreateIconFromRes.Call(
		uintptr(unsafe.Pointer(&b[0])),
		uintptr(len(b)),
		1,              // fIcon
		0x00030000,     // PNG 图标要求 3.0
		0, 0,           // 用原始尺寸
		0,              // LR_DEFAULTCOLOR
	)
	iconCache[key] = h
	return h
}

func addTrayIcon(hwnd uintptr, mode string) {
	trayHIcon = trayIcon(mode)
	trayMode = mode
	var nid notifyIconData
	nid.CbSize = uint32(unsafe.Sizeof(nid))
	nid.HWnd = hwnd
	nid.UID = trayUID
	nid.Flags = nifMessage | nifIcon | nifTip
	nid.CallbackMessage = wmTrayMsg
	nid.HIcon = trayHIcon
	copy(nid.SzTip[:], syscall.StringToUTF16("perfmode — 性能模式"))
	r, _, err := procShellNotifyIcon.Call(nimAdd, uintptr(unsafe.Pointer(&nid)))
	Logf("托盘图标: hicon=0x%X add=%d err=%v theme_light=%v", trayHIcon, r, err, systemUsesLightTheme())
}

// SetTrayMode 切换托盘图标到对应模式
func SetTrayMode(mode string) {
	if hwndMain == 0 || mode == trayMode {
		return
	}
	h := trayIcon(mode)
	if h == 0 {
		return
	}
	trayMode = mode
	trayHIcon = h
	var nid notifyIconData
	nid.CbSize = uint32(unsafe.Sizeof(nid))
	nid.HWnd = hwndMain
	nid.UID = trayUID
	nid.Flags = nifIcon
	nid.HIcon = h
	procShellNotifyIcon.Call(nimModify, uintptr(unsafe.Pointer(&nid)))
}

func removeTrayIcon(hwnd uintptr) {
	var nid notifyIconData
	nid.CbSize = uint32(unsafe.Sizeof(nid))
	nid.HWnd = hwnd
	nid.UID = trayUID
	procShellNotifyIcon.Call(nimDelete, uintptr(unsafe.Pointer(&nid)))
}

// showTrayMenu 弹出右键菜单，返回被点中的 ID（0 表示没选）
func showTrayMenu(hwnd uintptr) int {
	items := MenuProvider()
	if len(items) == 0 {
		return 0
	}
	hMenu, _, _ := procCreatePopupMenu.Call()
	if hMenu == 0 {
		return 0
	}
	defer procDestroyMenu.Call(hMenu)

	for _, it := range items {
		if it.Sep {
			procAppendMenu.Call(hMenu, mfSeparator, 0, 0)
			continue
		}
		flags := uintptr(mfString)
		if it.Checked {
			flags |= mfChecked
		}
		txt := syscall.StringToUTF16(it.Text)
		procAppendMenu.Call(hMenu, flags, uintptr(it.ID), uintptr(unsafe.Pointer(&txt[0])))
	}

	var pt point
	procGetCursorPos.Call(uintptr(unsafe.Pointer(&pt)))
	procSetForeground.Call(hwnd)
	r, _, _ := procTrackPopupMenu.Call(hMenu, tpmRightBtn|tpmReturnCmd,
		uintptr(pt.X), uintptr(pt.Y), 0, hwnd, 0)
	return int(r)
}
