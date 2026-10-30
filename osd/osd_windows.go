// Package osd 提供两个能力：
//
//  1. 屏幕上方无边框、置顶、不抢焦点的提示框（替代原厂 OSD）
//  2. 系统托盘图标 + 右键菜单
//
// 纯 Win32 + GDI，不依赖第三方库。
package osd

import (
	"runtime"
	"syscall"
	"unsafe"

	"golang.org/x/sys/windows"
)

var (
	user32   = windows.NewLazySystemDLL("user32.dll")
	gdi32    = windows.NewLazySystemDLL("gdi32.dll")
	shell32  = windows.NewLazySystemDLL("shell32.dll")
	kernel32 = windows.NewLazySystemDLL("kernel32.dll")

	procRegisterClassEx    = user32.NewProc("RegisterClassExW")
	procCreateWindowEx     = user32.NewProc("CreateWindowExW")
	procDefWindowProc      = user32.NewProc("DefWindowProcW")
	procShowWindow         = user32.NewProc("ShowWindow")
	procUpdateWindow       = user32.NewProc("UpdateWindow")
	procInvalidateRect     = user32.NewProc("InvalidateRect")
	procSetWindowPos       = user32.NewProc("SetWindowPos")
	procGetMessage         = user32.NewProc("GetMessageW")
	procTranslateMessage   = user32.NewProc("TranslateMessage")
	procDispatchMessage    = user32.NewProc("DispatchMessageW")
	procPostMessage        = user32.NewProc("PostMessageW")
	procSetTimer           = user32.NewProc("SetTimer")
	procKillTimer          = user32.NewProc("KillTimer")
	procBeginPaint         = user32.NewProc("BeginPaint")
	procEndPaint           = user32.NewProc("EndPaint")
	procFillRect           = user32.NewProc("FillRect")
	procDrawText           = user32.NewProc("DrawTextW")
	procGetSystemMetrics   = user32.NewProc("GetSystemMetrics")
	procDestroyWindow      = user32.NewProc("DestroyWindow")
	procPostQuitMessage    = user32.NewProc("PostQuitMessage")
	procSetProcessDPIAware = user32.NewProc("SetProcessDPIAware")
	procGetClientRect      = user32.NewProc("GetClientRect")
	procSetLayeredAttrs    = user32.NewProc("SetLayeredWindowAttributes")
	procSetDpiAwareCtx     = user32.NewProc("SetProcessDpiAwarenessContext")
	procGetDpiForSystem    = user32.NewProc("GetDpiForSystem")

	procSetTextColor     = gdi32.NewProc("SetTextColor")
	procSetBkMode        = gdi32.NewProc("SetBkMode")
	procCreateSolidBrush = gdi32.NewProc("CreateSolidBrush")
	procCreateFont       = gdi32.NewProc("CreateFontW")
	procSelectObject     = gdi32.NewProc("SelectObject")

	procGetModuleHandle = kernel32.NewProc("GetModuleHandleW")
)

const (
	wsPopup        = 0x80000000
	wsExTopmost    = 0x00000008
	wsExLayered    = 0x00080000
	wsExToolWindow = 0x00000080
	wsExNoActivate = 0x08000000

	swShowNoActivate = 4
	swHide           = 0

	wmDestroy    = 0x0002
	wmPaint      = 0x000F
	wmTimer      = 0x0113
	wmEraseBkgnd = 0x0014
	wmApp        = 0x8000

	transparent = 1

	dtCenter     = 0x00000001
	dtVCenter    = 0x00000004
	dtSingleLine = 0x00000020

	winW0 = 420
	winH0 = 168
)

// Item 是提示框要显示的内容
type Item struct {
	Title string   // 第一行，大号
	Lines []string // 后续每行，小号
	Mode  string   // app / turbo / balance / silence，决定左侧图标
}

// MenuItem 是托盘右键菜单的一项
type MenuItem struct {
	ID      int
	Text    string
	Checked bool
	Sep     bool
}

// 由调用方注入的回调
var (
	Logf           = func(format string, a ...any) {}
	MenuProvider   = func() []MenuItem { return nil }
	CommandHandler = func(id int) {}
)

type wndClassEx struct {
	cbSize        uint32
	style         uint32
	lpfnWndProc   uintptr
	cbClsExtra    int32
	cbWndExtra    int32
	hInstance     uintptr
	hIcon         uintptr
	hCursor       uintptr
	hbrBackground uintptr
	lpszMenuName  *uint16
	lpszClassName *uint16
	hIconSm       uintptr
}

type rect struct {
	Left, Top, Right, Bottom int32
}

type paintStruct struct {
	hdc         uintptr
	fErase      int32
	rcPaint     rect
	fRestore    int32
	fIncUpdate  int32
	rgbReserved [32]byte
}

type msg struct {
	hwnd    uintptr
	message uint32
	wParam  uintptr
	lParam  uintptr
	time    uint32
	pt      struct{ X, Y int32 }
}

var (
	hwndMain uintptr
	curMode  = "app"
	title    []uint16
	lines    []string
	fontBig  uintptr
	fontMid  uintptr
	fontName *uint16
	brushBg  uintptr
	classNamePtr *uint16
	osdMs        = uintptr(1800)
	osdScale     = 1.0
	screenW      int
	screenH      int
	curWinH      int
)

func u16(s string) *uint16 {
	p, err := syscall.UTF16PtrFromString(s)
	if err != nil {
		panic(err)
	}
	return p
}

// Init 让进程按物理像素工作，避免高分屏下坐标和尺寸错位
func Init() {
	// DPI_AWARENESS_CONTEXT_PER_MONITOR_AWARE_V2 = -4
	if r, _, _ := procSetDpiAwareCtx.Call(^uintptr(3)); r == 0 {
		procSetProcessDPIAware.Call()
	}
}

func scaleFactor() float64 {
	if dpi, _, _ := procGetDpiForSystem.Call(); dpi > 0 {
		return float64(dpi) / 96.0
	}
	return 1.0
}

// Run 创建窗口和托盘图标并进入消息循环
func Run(updates <-chan Item, durationMs int, initialMode string) {
	runtime.LockOSThread()
	defer runtime.UnlockOSThread()

	osdMs = uintptr(durationMs)
	if initialMode == "" {
		initialMode = "app"
	}
	curMode = initialMode
	hInst, _, _ := procGetModuleHandle.Call(0)

	classNamePtr = u16("perfmode_osd")
	fontName = u16("Microsoft YaHei UI")

	wc := wndClassEx{
		cbSize:        uint32(unsafe.Sizeof(wndClassEx{})),
		lpfnWndProc:   syscall.NewCallback(wndProc),
		hInstance:     hInst,
		lpszClassName: classNamePtr,
	}
	if atom, _, err := procRegisterClassEx.Call(uintptr(unsafe.Pointer(&wc))); atom == 0 {
		panic("RegisterClassEx 失败: " + err.Error())
	}

	scale := scaleFactor()
	osdScale = scale
	winW := int(float64(winW0) * scale)
	winH := int(float64(winH0) * scale)
	curWinH = winH

	sw, _, _ := procGetSystemMetrics.Call(0)
	sh, _, _ := procGetSystemMetrics.Call(1)
	screenW, screenH = int(sw), int(sh)
	x := (screenW - winW) / 2
	y := int(float64(screenH) * 0.10)
	Logf("DPI %.2f 窗口 %dx%d 屏幕 %dx%d", scale, winW, winH, sw, sh)

	hwnd, _, err := procCreateWindowEx.Call(
		wsExTopmost|wsExLayered|wsExToolWindow|wsExNoActivate,
		uintptr(unsafe.Pointer(classNamePtr)), 0, wsPopup,
		uintptr(x), uintptr(y), uintptr(winW), uintptr(winH),
		0, 0, hInst, 0,
	)
	if hwnd == 0 {
		panic("CreateWindowEx 失败: " + err.Error())
	}
	hwndMain = hwnd

	// WS_EX_LAYERED 窗口默认 alpha 为 0，必须显式设置
	procSetLayeredAttrs.Call(hwnd, 0, 244, 0x00000002)

	fontBig, _, _ = procCreateFont.Call(uintptr(float64(28)*scale), 0, 0, 0, 700, 0, 0, 0, 0x86, 0, 0, 5, 0, uintptr(unsafe.Pointer(fontName)))
	fontMid, _, _ = procCreateFont.Call(uintptr(float64(15)*scale), 0, 0, 0, 400, 0, 0, 0, 0x86, 0, 0, 5, 0, uintptr(unsafe.Pointer(fontName)))
	brushBg, _, _ = procCreateSolidBrush.Call(0x00201F1E)

	addTrayIcon(hwnd, initialMode)

	go func() {
		for it := range updates {
			setItem(it)
			procPostMessage.Call(hwnd, wmApp+1, 0, 0)
		}
	}()

	var m msg
	for {
		r, _, _ := procGetMessage.Call(uintptr(unsafe.Pointer(&m)), 0, 0, 0)
		if int32(r) <= 0 {
			break
		}
		procTranslateMessage.Call(uintptr(unsafe.Pointer(&m)))
		procDispatchMessage.Call(uintptr(unsafe.Pointer(&m)))
	}
}

func setItem(it Item) {
	title = syscall.StringToUTF16(it.Title)
	lines = it.Lines
	if it.Mode == "" {
		it.Mode = "app"
	}
	curMode = it.Mode
}

// SetTip 更新托盘图标的悬停提示
func SetTip(text string) {
	var nid notifyIconData
	nid.CbSize = uint32(unsafe.Sizeof(nid))
	nid.HWnd = hwndMain
	nid.UID = trayUID
	nid.Flags = nifTip
	copy(nid.SzTip[:], syscall.StringToUTF16(text))
	procShellNotifyIcon.Call(nimModify, uintptr(unsafe.Pointer(&nid)))
}

func clientRect(hwnd uintptr) rect {
	var r rect
	procGetClientRect.Call(hwnd, uintptr(unsafe.Pointer(&r)))
	return r
}

func wndProc(hwnd uintptr, msgID uint32, wParam, lParam uintptr) uintptr {
	switch msgID {
	case wmEraseBkgnd:
		return 1

	case wmPaint:
		var ps paintStruct
		hdc, _, _ := procBeginPaint.Call(hwnd, uintptr(unsafe.Pointer(&ps)))
		r := clientRect(hwnd)
		procFillRect.Call(hdc, uintptr(unsafe.Pointer(&r)), brushBg)
		procSetBkMode.Call(hdc, transparent)

		h := int(r.Bottom)
		sc := osdScale
		iconSize := int(52 * sc)
		padX := int(18 * sc)
		iconY := (h - iconSize) / 2
		drawGlyph(hdc, curMode, padX, iconY, iconSize)

		n := len(lines)
		if n > 3 {
			n = 3
		}
		titleH := int(26 * sc)
		lineH := int(22 * sc)
		textH := titleH + n*lineH
		top := int32((h - textH) / 2)
		textX := int32(padX + iconSize + int(14*sc))
		textR := int32(r.Right) - int32(16*sc)

		old, _, _ := procSelectObject.Call(hdc, fontBig)
		procSetTextColor.Call(hdc, 0x00FFFFFF)
		r1 := rect{textX, top, textR, top + int32(titleH)}
		if len(title) > 0 {
			procDrawText.Call(hdc, uintptr(unsafe.Pointer(&title[0])), uintptr(len(title)-1),
				uintptr(unsafe.Pointer(&r1)), dtVCenter|dtSingleLine)
		}

		procSelectObject.Call(hdc, fontMid)
		procSetTextColor.Call(hdc, 0x00A8ACB4)
		for i := 0; i < n; i++ {
			t := syscall.StringToUTF16(lines[i])
			yt := top + int32(titleH) + int32(i*lineH)
			rr := rect{textX, yt, textR, yt + int32(lineH)}
			procDrawText.Call(hdc, uintptr(unsafe.Pointer(&t[0])), uintptr(len(t)-1),
				uintptr(unsafe.Pointer(&rr)), dtVCenter|dtSingleLine)
		}
		procSelectObject.Call(hdc, old)
		procEndPaint.Call(hwnd, uintptr(unsafe.Pointer(&ps)))
		return 0

	case wmApp + 1:
		// 高度按内容计算：上下留白 + max(图标, 文字块)
		n := len(lines)
		if n > 3 {
			n = 3
		}
		pad := float64(18)
		iconH := float64(52)
		textH := float64(26) + float64(n)*float64(22)
		if textH < iconH {
			textH = iconH
		}
		wantH := int((pad*2 + textH) * osdScale)
		if wantH != curWinH {
			curWinH = wantH
			w := int(float64(winW0) * osdScale)
			x := (screenW - w) / 2
			y := int(float64(screenH) * 0.10)
			const hwndTopmost = ^uintptr(0) // HWND_TOPMOST = -1
			const swpNoActivate = 0x0010
			procSetWindowPos.Call(hwnd, hwndTopmost,
				uintptr(x), uintptr(y), uintptr(w), uintptr(wantH), swpNoActivate)
		}
		procShowWindow.Call(hwnd, swShowNoActivate)
		// 窗口已经可见时单纯 ShowWindow 不会重绘，必须显式作废客户区
		procInvalidateRect.Call(hwnd, 0, 1)
		procUpdateWindow.Call(hwnd)
		procSetTimer.Call(hwnd, 1, osdMs, 0)
		return 0

	case wmTimer:
		procKillTimer.Call(hwnd, 1)
		procShowWindow.Call(hwnd, swHide)
		return 0

	case wmTrayMsg:
		switch uint32(lParam) {
		case wmRButtonUp, wmLButtonUp:
			if id := showTrayMenu(hwnd); id > 0 {
				CommandHandler(id)
			}
		}
		return 0

	case wmDestroy:
		removeTrayIcon(hwnd)
		procPostQuitMessage.Call(0)
		return 0
	}
	r, _, _ := procDefWindowProc.Call(hwnd, uintptr(msgID), wParam, lParam)
	return r
}

// Quit 关闭窗口并退出消息循环
func Quit() {
	if hwndMain != 0 {
		procPostMessage.Call(hwndMain, wmDestroy, 0, 0)
	}
}
