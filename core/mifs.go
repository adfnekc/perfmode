// ACPI-WMI 支持：解析 _WDG 并调用 WMI 方法块。
//
// ACPI-WMI 的数据块描述符 _WDG 每 20 字节一项：
//
//	0x00  16  GUID
//	0x10   2  方法 ID（两个 ASCII 字符）或事件通知 ID
//	0x12   1  实例数
//	0x13   1  标志位（0x2 = 方法块，0x8 = 事件块）
//
// 方法块对应的 ACPI 方法是 "WM" + 方法ID，签名 WMxx(实例, 方法ID, 缓冲区)。
package core

import (
	"encoding/binary"
	"fmt"
	"strconv"
	"strings"

	"golang.org/x/sys/windows"
)

// MIFS 是同方（Tongfang）笔记本的 ACPI-WMI 接口 GUID
const mifsGUID = "B60BFB48-3E5B-49E4-A0E9-8CFFE1B3434B"

// MIFS 方法号
const (
	mifsFnSystemPerMode = 8  // 性能模式
	mifsTypeGet         = 250 // 查询
	mifsTypeSet         = 251 // 设置
)

// WMIBlock 是一个 _WDG 数据块描述
type WMIBlock struct {
	GUID          windows.GUID
	MethodID      string
	InstanceCount uint8
	Flags         uint8
}

// IsMethod 判断是否为方法块
func (b WMIBlock) IsMethod() bool { return b.Flags&0x2 != 0 }

// ReadWDG 读取并解析设备自己的 _WDG
func ReadWDG(dev *ACPIDevice) ([]WMIBlock, error) {
	args, err := dev.Eval("_WDG")
	if err != nil {
		return nil, err
	}
	if len(args) == 0 {
		return nil, fmt.Errorf("_WDG 没有返回值")
	}
	buf := args[0].Data
	if len(buf) == 0 || len(buf)%20 != 0 {
		return nil, fmt.Errorf("_WDG 长度异常: %d", len(buf))
	}
	blocks := make([]WMIBlock, 0, len(buf)/20)
	for off := 0; off+20 <= len(buf); off += 20 {
		e := buf[off : off+20]
		blocks = append(blocks, WMIBlock{
			GUID: windows.GUID{
				Data1: binary.LittleEndian.Uint32(e[0:]),
				Data2: binary.LittleEndian.Uint16(e[4:]),
				Data3: binary.LittleEndian.Uint16(e[6:]),
				Data4: [8]byte{e[8], e[9], e[10], e[11], e[12], e[13], e[14], e[15]},
			},
			MethodID:      string(e[16:18]),
			InstanceCount: e[18],
			Flags:         e[19],
		})
	}
	return blocks, nil
}

// EvalWMI 调用一个 WMI 方法块
func (d *ACPIDevice) EvalWMI(block WMIBlock, instance, methodID uint32, in []byte) ([]byte, error) {
	if !block.IsMethod() {
		return nil, fmt.Errorf("该数据块不是方法块 (flags=0x%02X)", block.Flags)
	}
	name := "WM" + block.MethodID
	args, err := d.Eval(name, IntArg(instance), IntArg(methodID), BufferArg(in))
	if err != nil {
		return nil, err
	}
	if len(args) == 0 {
		return nil, nil
	}
	return args[0].Data, nil
}

// ============================================================
// MIFS 后端
// ============================================================

type mifsBackend struct {
	dev   *ACPIDevice
	block WMIBlock
}

// Name 实现 Backend
func (b *mifsBackend) Name() string { return "MIFS ACPI-WMI" }

// DevicePath 实现 Backend
func (b *mifsBackend) DevicePath() string { return b.dev.Path() }

// Close 实现 Backend
func (b *mifsBackend) Close() error { return b.dev.Close() }

// call 构造 32 字节 MIFS 请求并取回结果
func (b *mifsBackend) call(typ, fn byte, payload []byte) ([]byte, error) {
	buf := make([]byte, 32)
	buf[1] = typ
	buf[3] = fn
	copy(buf[4:], payload)
	return b.dev.EvalWMI(b.block, 0, 1, buf)
}

// GetRaw 查询当前性能模式
func (b *mifsBackend) GetRaw() (uint32, error) {
	out, err := b.call(mifsTypeGet, mifsFnSystemPerMode, nil)
	if err != nil {
		return 0, err
	}
	if len(out) < 5 {
		return 0, fmt.Errorf("MIFS 返回数据过短: %d", len(out))
	}
	return uint32(out[4]), nil
}

// SetRaw 设置性能模式
func (b *mifsBackend) SetRaw(v uint32) error {
	if v > 0xFF {
		return fmt.Errorf("模式值超出范围: %d", v)
	}
	out, err := b.call(mifsTypeSet, mifsFnSystemPerMode, []byte{byte(v)})
	if err != nil {
		return err
	}
	// 返回缓冲区第一个字节为状态: 0 = 成功
	if len(out) > 0 && out[0] != 0 {
		return fmt.Errorf("MIFS 拒绝该值，状态 0x%02X", out[0])
	}
	return nil
}

// openMIFS 找到带 MIFS GUID 的 ACPI 设备
func openMIFS(paths []string) (Backend, error) {
	want, err := parseGUID(mifsGUID)
	if err != nil {
		return nil, err
	}
	var lastErr error
	for _, p := range paths {
		dev, err := OpenACPIDevice(p)
		if err != nil {
			lastErr = err
			continue
		}
		blocks, err := ReadWDG(dev)
		if err != nil {
			lastErr = err
			dev.Close()
			continue
		}
		for _, blk := range blocks {
			if blk.GUID == want && blk.IsMethod() {
				b := &mifsBackend{dev: dev, block: blk}
				if _, err := b.GetRaw(); err != nil {
					lastErr = err
					break
				}
				return b, nil
			}
		}
		dev.Close()
	}
	if lastErr == nil {
		lastErr = fmt.Errorf("没有找到 GUID %s 的方法块", mifsGUID)
	}
	return nil, fmt.Errorf("MIFS 后端不可用: %w", lastErr)
}

// parseGUID 解析 {XXXXXXXX-XXXX-XXXX-XXXX-XXXXXXXXXXXX}
func parseGUID(s string) (windows.GUID, error) {
	var g windows.GUID
	t := strings.Trim(s, "{}")
	parts := strings.Split(t, "-")
	if len(parts) != 5 {
		return g, fmt.Errorf("GUID 格式错误: %s", s)
	}
	d1, err := strconv.ParseUint(parts[0], 16, 32)
	if err != nil {
		return g, err
	}
	d2, err := strconv.ParseUint(parts[1], 16, 16)
	if err != nil {
		return g, err
	}
	d3, err := strconv.ParseUint(parts[2], 16, 16)
	if err != nil {
		return g, err
	}
	g.Data1 = uint32(d1)
	g.Data2 = uint16(d2)
	g.Data3 = uint16(d3)
	tail := parts[3] + parts[4]
	if len(tail) != 16 {
		return g, fmt.Errorf("GUID 尾部长度错误: %s", s)
	}
	for i := 0; i < 8; i++ {
		v, err := strconv.ParseUint(tail[i*2:i*2+2], 16, 8)
		if err != nil {
			return g, err
		}
		g.Data4[i] = byte(v)
	}
	return g, nil
}
