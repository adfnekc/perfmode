// EC RAM 里已知字段的说明表。
//
// 偏移与名称来自 DSDT 的 OperationRegion(ECF2) 字段声明；
// 「含义」列是对照实机读数推断出来的（温度、转速、模式等）。
package core

import "fmt"

// ECField 描述一个已知的 EC 字段
type ECField struct {
	Offset byte
	Name   string
	Desc   string
	Unit   string
}

// KnownECFields 是当前机型（无界14 Pro）已验证的字段
var KnownECFields = []ECField{
	{0x17, "TSR1", "温度传感器 1", "°C"},
	{0x18, "TSR2", "温度传感器 2", "°C"},
	{0x19, "TSR3", "温度传感器 3", "°C"},
	{0x1A, "TSR4", "温度传感器 4", "°C"},
	{0x1B, "TSR5", "温度传感器 5", "°C"},
	{0x1C, "TSR6", "温度传感器 6", "°C"},
	{0x1D, "TSR7", "温度传感器 7", "°C"},
	{0x1E, "TSR8", "温度传感器 8", "°C"},
	{0x1F, "TSR9", "温度传感器 9", "°C"},
	{0x60, "ECWR", "EC 状态位（bit0=交流供电 等）", ""},
	{0x9B, "F1HI", "风扇 1 转速高字节", "rpm"},
	{0x9C, "F1LO", "风扇 1 转速低字节", "rpm"},
	{0x9D, "F2HI", "风扇 2 转速高字节", "rpm"},
	{0x9E, "F2LO", "风扇 2 转速低字节", "rpm"},
	{0xE0, "DGPU", "独显模式标志", ""},
	{0xE2, "KBBL", "键盘背光档位", ""},
	{0xE3, "BPWM", "背光 PWM", ""},
	{0xE4, "ITSM", "性能模式", ""},
	{0xE7, "ECTP", "EC 温度", "°C"},
}

// ECSnapshot 一次 EC 读数结果
type ECSnapshot struct {
	Values map[byte]byte
	Fans   []int // 风扇转速
}

// ReadSensors 读取已知字段，并解析风扇转速
func ReadSensors(ec *ECDirect) (ECSnapshot, error) {
	snap := ECSnapshot{Values: map[byte]byte{}}
	for _, f := range KnownECFields {
		if f.Name == "F1LO" || f.Name == "F1HI" || f.Name == "F2LO" || f.Name == "F2HI" {
			continue
		}
		v, err := ec.ReadStable(f.Offset)
		if err != nil {
			return snap, fmt.Errorf("读取 0x%02X (%s) 失败: %w", f.Offset, f.Name, err)
		}
		snap.Values[f.Offset] = v
	}

	for _, pair := range [][2]byte{{0x9B, 0x9C}, {0x9D, 0x9E}} {
		hi, err := ec.ReadStable(pair[0])
		if err != nil {
			return snap, err
		}
		lo, err := ec.ReadStable(pair[1])
		if err != nil {
			return snap, err
		}
		snap.Values[pair[0]] = hi
		snap.Values[pair[1]] = lo
		snap.Fans = append(snap.Fans, int(hi)<<8|int(lo))
	}
	return snap, nil
}
