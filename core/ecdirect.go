// 直接通过 I/O 端口访问 ACPI EC（Embedded Controller）。
//
// 这台机器的 EC 在标准位置（注册表 LogConf 里可见 I/O 范围 0x62 / 0x66）:
//
//	0x62  数据端口
//	0x66  命令/状态端口
//
// 状态寄存器位:
//
//	bit0 OBF  Output Buffer Full  — 数据端口有数据可读
//	bit1 IBF  Input  Buffer Full  — EC 还没取走上一个字节
//
// 读 EC RAM 的时序:
//
//	等 IBF 清零 → 0x66 写 0x80 → 等 IBF 清零 → 0x62 写偏移
//	→ 等 OBF 置位 → 从 0x62 读出值
//
// 写 EC RAM 的时序:
//
//	等 IBF 清零 → 0x66 写 0x81 → 等 IBF 清零 → 0x62 写偏移
//	→ 等 IBF 清零 → 0x62 写值
package core

import (
	"fmt"

)

const (
	ECDataPort = 0x62
	ECCmdPort  = 0x66

	ecCmdRead  = 0x80
	ecCmdWrite = 0x81

	ecStatusOBF = 0x01
	ecStatusIBF = 0x02
)

// ECDirect 通过端口直接访问 EC
type ECDirect struct {
	io *PortIO
}

// OpenECDirect 打开直接 EC 访问
func OpenECDirect() (*ECDirect, error) {
	io, err := OpenPortIO()
	if err != nil {
		return nil, err
	}
	return &ECDirect{io: io}, nil
}

// Close 关闭
func (e *ECDirect) Close() error { return e.io.Close() }

// waitClear 紧密轮询直到状态位清零
//
// 注意: 必须用忙等而不是 sleep。OBF 只保持极短时间，
// 用 100µs 级别的 sleep 轮询会直接错过。
func (e *ECDirect) waitClear(mask byte, spins int) error {
	for i := 0; i < spins; i++ {
		st, err := e.io.InB(ECCmdPort)
		if err != nil {
			return err
		}
		if st&mask == 0 {
			return nil
		}
	}
	return fmt.Errorf("EC 忙（等待状态位 0x%02X 清零超时）", mask)
}

func (e *ECDirect) waitSet(mask byte, spins int) error {
	for i := 0; i < spins; i++ {
		st, err := e.io.InB(ECCmdPort)
		if err != nil {
			return err
		}
		if st&mask != 0 {
			return nil
		}
	}
	return fmt.Errorf("EC 无响应（等待状态位 0x%02X 置位超时）", mask)
}

const ecSpins = 200000

// Read 读取 EC RAM 的一个字节
func (e *ECDirect) Read(offset byte) (byte, error) {
	if err := e.waitClear(ecStatusIBF|ecStatusOBF, ecSpins); err != nil {
		return 0, err
	}
	if err := e.io.OutB(ECCmdPort, ecCmdRead); err != nil {
		return 0, err
	}
	if err := e.waitClear(ecStatusIBF, ecSpins); err != nil {
		return 0, err
	}
	if err := e.io.OutB(ECDataPort, offset); err != nil {
		return 0, err
	}
	if err := e.waitSet(ecStatusOBF, ecSpins); err != nil {
		return 0, err
	}
	return e.io.InB(ECDataPort)
}

// Write 写入 EC RAM 的一个字节
func (e *ECDirect) Write(offset, value byte) error {
	if err := e.waitClear(ecStatusIBF|ecStatusOBF, ecSpins); err != nil {
		return err
	}
	if err := e.io.OutB(ECCmdPort, ecCmdWrite); err != nil {
		return err
	}
	if err := e.waitClear(ecStatusIBF, ecSpins); err != nil {
		return err
	}
	if err := e.io.OutB(ECDataPort, offset); err != nil {
		return err
	}
	if err := e.waitClear(ecStatusIBF, ecSpins); err != nil {
		return err
	}
	return e.io.OutB(ECDataPort, value)
}

// Snapshot 读取整片 EC RAM（0x00-0xFF），单个字节失败会重试
func (e *ECDirect) Snapshot() ([256]byte, error) {
	var buf [256]byte
	for i := 0; i < 256; i++ {
		var v byte
		var err error
		for try := 0; try < 3; try++ {
			v, err = e.Read(byte(i))
			if err == nil {
				break
			}
		}
		if err != nil {
			return buf, fmt.Errorf("读取 EC[0x%02X] 失败: %w", i, err)
		}
		buf[i] = v
	}
	return buf, nil
}

// ReadMany 读取指定的若干偏移
func (e *ECDirect) ReadMany(offsets []byte) (map[byte]byte, error) {
	out := make(map[byte]byte, len(offsets))
	for _, off := range offsets {
		v, err := e.Read(off)
		if err != nil {
			return out, fmt.Errorf("读取 EC[0x%02X] 失败: %w", off, err)
		}
		out[off] = v
	}
	return out, nil
}

// ReadStable 读取一个字节，并通过「连续两次读到相同值」过滤掉
// 与 ACPI EC 驱动抢占总线造成的脏数据。
//
// 实测：单次读取偶尔会返回上一次事务残留的字节（例如模式值 0/1/2 之间
// 突然冒出 0x21），连续两次一致才可信。
func (e *ECDirect) ReadStable(offset byte) (byte, error) {
	var prev byte
	var err error
	for i := 0; i < 6; i++ {
		var v byte
		v, err = e.Read(offset)
		if err != nil {
			continue
		}
		if i > 0 && v == prev {
			return v, nil
		}
		prev = v
	}
	if err != nil {
		return 0, err
	}
	return prev, nil
}
