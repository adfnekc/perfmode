// 图标资源：编译期嵌入，运行期不需要外部文件。
package osd

import (
	"bytes"
	"embed"
	"image"
	"image/png"
	"sync"
)

//go:embed assets/*.png
var assetsFS embed.FS

var (
	imgCache   = map[string]image.Image{}
	imgMu      sync.Mutex
	pngCache   = map[string][]byte{}
	iconCache  = map[string]uintptr{}
)

func assetBytes(name string) []byte {
	imgMu.Lock()
	defer imgMu.Unlock()
	if b, ok := pngCache[name]; ok {
		return b
	}
	b, err := assetsFS.ReadFile("assets/" + name)
	if err != nil {
		return nil
	}
	pngCache[name] = b
	return b
}

// glyphImage 返回 OSD 里用的模式字形（透明底 PNG）
func glyphImage(mode string) image.Image {
	imgMu.Lock()
	defer imgMu.Unlock()
	if im, ok := imgCache[mode]; ok {
		return im
	}
	b, err := assetsFS.ReadFile("assets/glyph_" + mode + ".png")
	if err != nil {
		return nil
	}
	im, err := png.Decode(bytes.NewReader(b))
	if err != nil {
		return nil
	}
	imgCache[mode] = im
	return im
}
