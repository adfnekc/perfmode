// 用 GDI 把嵌入的 PNG 画到窗口上（AlphaBlend 需要预乘 alpha）
package osd

import (
	"image"
	"unsafe"

	"golang.org/x/sys/windows"
)

var (
	msimg32 = windows.NewLazySystemDLL("msimg32.dll")

	procAlphaBlend    = msimg32.NewProc("AlphaBlend")
	procCreateCompatDC = gdi32.NewProc("CreateCompatibleDC")
	procCreateDIBSec  = gdi32.NewProc("CreateDIBSection")
	procDeleteDC      = gdi32.NewProc("DeleteDC")
	procDeleteObject  = gdi32.NewProc("DeleteObject")
)

type bitmapInfoHeader struct {
	BiSize          uint32
	BiWidth         int32
	BiHeight        int32
	BiPlanes        uint16
	BiBitCount      uint16
	BiCompression   uint32
	BiSizeImage     uint32
	BiXPelsPerMeter int32
	BiYPelsPerMeter int32
	BiClrUsed       uint32
	BiClrImportant  uint32
}

type bitmapInfo struct {
	BmiHeader bitmapInfoHeader
	BmiColors [1]uint32
}

type blendFunction struct {
	BlendOp             byte
	BlendFlags          byte
	SourceConstantAlpha byte
	AlphaFormat         byte
}

type dibImage struct {
	dc     uintptr
	bitmap uintptr
	w, h   int
}

var dibCache = map[string]*dibImage{}

// dibFor 把 image.Image 转成预乘 alpha 的 32 位 DIB 并缓存
func dibFor(key string, im image.Image) *dibImage {
	if d, ok := dibCache[key]; ok {
		return d
	}
	if im == nil {
		return nil
	}
	b := im.Bounds()
	w, h := b.Dx(), b.Dy()

	bi := bitmapInfo{}
	bi.BmiHeader.BiSize = uint32(unsafe.Sizeof(bitmapInfoHeader{}))
	bi.BmiHeader.BiWidth = int32(w)
	bi.BmiHeader.BiHeight = -int32(h) // 负数 = 自上而下
	bi.BmiHeader.BiPlanes = 1
	bi.BmiHeader.BiBitCount = 32
	bi.BmiHeader.BiCompression = 0 // BI_RGB

	var bits unsafe.Pointer
	dc, _, _ := procCreateCompatDC.Call(0)
	if dc == 0 {
		return nil
	}
	hbm, _, _ := procCreateDIBSec.Call(dc, uintptr(unsafe.Pointer(&bi)), 0,
		uintptr(unsafe.Pointer(&bits)), 0, 0)
	if hbm == 0 || bits == nil {
		procDeleteDC.Call(dc)
		return nil
	}
	procSelectObject.Call(dc, hbm)

	dst := unsafe.Slice((*byte)(bits), w*h*4)
	for y := 0; y < h; y++ {
		for x := 0; x < w; x++ {
			r, g, bl, a := im.At(b.Min.X+x, b.Min.Y+y).RGBA()
			// RGBA() 返回 16 位预乘值，这里换算成 8 位
			rr, gg, bb := uint8(r>>8), uint8(g>>8), uint8(bl>>8)
			aa := uint8(a >> 8)
			o := (y*w + x) * 4
			dst[o+0] = bb // B
			dst[o+1] = gg // G
			dst[o+2] = rr // R
			dst[o+3] = aa // A
		}
	}

	d := &dibImage{dc: dc, bitmap: hbm, w: w, h: h}
	dibCache[key] = d
	return d
}

// drawGlyph 在 (x,y) 处按 size 绘制字形
func drawGlyph(hdc uintptr, mode string, x, y, size int) {
	d := dibFor(mode, glyphImage(mode))
	if d == nil {
		return
	}
	bf := blendFunction{BlendOp: 0, BlendFlags: 0, SourceConstantAlpha: 255, AlphaFormat: 1} // AC_SRC_ALPHA
	procAlphaBlend.Call(hdc,
		uintptr(x), uintptr(y), uintptr(size), uintptr(size),
		d.dc, 0, 0, uintptr(d.w), uintptr(d.h),
		uintptr(*(*uint32)(unsafe.Pointer(&bf))))
}
