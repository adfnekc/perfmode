// 后端抽象：把「读/写原始模式值」这件事和具体固件接口解耦。
//
// 目前有两种候选接口：
//
//  1. ecBackend  — EC 的 ITSM 字段（读）+ ECWT 方法（写）
//     对应 \_SB.PCI0.LPC0.H_EC.*，是 mechrevo-wujie14pro-perfmode-kmod 的做法。
//     注意: ECWT 的第二个参数是 ACPI Reference，用户态 IOCTL 无法构造，
//     所以这个后端的写入路径在 Windows 上不可用，只能读。
//
//  2. mifsBackend — PNP0C14\MIFS 的 ACPI-WMI 接口
//     通过 _WDG 找到 GUID 对应的方法块，再调用 WMxx(实例, 方法ID, 缓冲区)。
//     纯整数收发，可以读也可以写。
package core

import (
	"errors"
	"fmt"
	"strings"
)

// ErrNotWritable 表示该后端不支持写入
var ErrNotWritable = errors.New("该接口不支持写入")

// Backend 是性能模式的读写后端
type Backend interface {
	// Name 返回后端名称，用于诊断输出
	Name() string
	// DevicePath 返回所用 ACPI 设备的接口路径
	DevicePath() string
	// GetRaw 读取固件里的原始模式值
	GetRaw() (uint32, error)
	// SetRaw 写入原始模式值
	SetRaw(v uint32) error
	// Close 释放资源
	Close() error
}

// ============================================================
// EC 后端
// ============================================================

const (
	acpiPathITSM = `\_SB.PCI0.LPC0.H_EC.ITSM`
	acpiPathECWT = `\_SB.PCI0.LPC0.H_EC.ECWT`
	hidEC        = "PNP0C09"
)

type ecBackend struct {
	dev *ACPIDevice
}

// Name 实现 Backend
func (b *ecBackend) Name() string { return "EC (ITSM/ECWT)" }

// DevicePath 实现 Backend
func (b *ecBackend) DevicePath() string { return b.dev.Path() }

// Close 实现 Backend
func (b *ecBackend) Close() error { return b.dev.Close() }

// GetRaw 读取 ITSM 字段
func (b *ecBackend) GetRaw() (uint32, error) {
	args, err := b.dev.Eval(acpiPathITSM)
	if err != nil {
		return 0, fmt.Errorf("读取 ITSM 失败: %w", err)
	}
	if len(args) == 0 {
		return 0, fmt.Errorf("ITSM 没有返回值")
	}
	return args[0].Uint32(), nil
}

// SetRaw 写入 ITSM 字段（用户态不可用）
func (b *ecBackend) SetRaw(v uint32) error {
	// ECWT(Value, RefOf(ITSM)): 第二个参数必须是引用，
	// 而 ACPI_METHOD_ARGUMENT 只有 INTEGER/STRING/BUFFER/PACKAGE 四种类型。
	return fmt.Errorf("ECWT 需要 Reference 参数，用户态无法构造: %w", ErrNotWritable)
}

// ============================================================
// 后端发现
// ============================================================

// ecDirectBackend 通过 I/O 端口直接读写 EC RAM
type ecDirectBackend struct {
	ec     *ECDirect
	offset byte
}

// Name 实现 Backend
func (b *ecDirectBackend) Name() string { return "EC 直连 (端口 0x62/0x66)" }

// DevicePath 实现 Backend
func (b *ecDirectBackend) DevicePath() string { return "inpoutx64 -> EC[0x" + fmt.Sprintf("%02X", b.offset) + "]" }

// Close 实现 Backend
func (b *ecDirectBackend) Close() error { return b.ec.Close() }

// GetRaw 实现 Backend
func (b *ecDirectBackend) GetRaw() (uint32, error) {
	v, err := b.ec.ReadStable(b.offset)
	if err != nil {
		return 0, err
	}
	return uint32(v), nil
}

// SetRaw 实现 Backend
func (b *ecDirectBackend) SetRaw(v uint32) error {
	if v > 0xFF {
		return fmt.Errorf("模式值超出范围: %d", v)
	}
	return b.ec.Write(b.offset, byte(v))
}

// OpenBackend 自动探测可用后端
func OpenBackend(cfg Config) (Backend, error) {
	paths, err := EnumerateACPIDevices()
	if err != nil {
		return nil, err
	}

	var errs []string

	// 优先 EC 直连（已校准时可用，读写都支持）
	if cfg.ECOffset != nil {
		ec, err := OpenECDirect()
		if err == nil {
			b := &ecDirectBackend{ec: ec, offset: byte(*cfg.ECOffset)}
			if _, err := b.GetRaw(); err == nil {
				return b, nil
			} else {
				ec.Close()
				errs = append(errs, "EC 直连: "+err.Error())
			}
		} else {
			errs = append(errs, "EC 直连: "+err.Error())
		}
	} else {
		errs = append(errs, "EC 直连: 尚未校准（运行 perfmode calibrate）")
	}

	// 其次 MIFS（可读可写）
	if b, err := openMIFS(paths); err == nil {
		return b, nil
	} else if err != nil {
		errs = append(errs, err.Error())
	}

	// 退回 EC（只读）
	if b, err := openEC(paths); err == nil {
		return b, nil
	} else if err != nil {
		errs = append(errs, err.Error())
	}

	return nil, fmt.Errorf("找不到可用的性能模式接口:\n  %s", strings.Join(errs, "\n  "))
}

// openEC 找到能读取 ITSM 的 EC 设备
func openEC(paths []string) (Backend, error) {
	var lastErr error
	for _, p := range paths {
		if !strings.Contains(strings.ToUpper(p), hidEC) {
			continue
		}
		dev, err := OpenACPIDevice(p)
		if err != nil {
			lastErr = err
			continue
		}
		b := &ecBackend{dev: dev}
		if _, err := b.GetRaw(); err != nil {
			lastErr = err
			dev.Close()
			continue
		}
		return b, nil
	}
	if lastErr == nil {
		lastErr = fmt.Errorf("没有找到 %s 设备", hidEC)
	}
	return nil, fmt.Errorf("EC 后端不可用: %w", lastErr)
}
