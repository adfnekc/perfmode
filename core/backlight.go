// 键盘背光控制。
//
// EC[0xE2] = KBBL，DSDT 里只声明未使用，实际由 EC 固件自己维护
// （Fn+F9 切换）。实测当前值为 2，推测档位为 0/1/2。
package core

import "fmt"

// KBBLOffset 是键盘背光档位字段在 EC 中的偏移
const KBBLOffset = 0xE2

// BacklightLevels 是背光档位数量（0..2）
const BacklightLevels = 3

// ReadBacklight 读取当前背光档位
func ReadBacklight(ec *ECDirect) (byte, error) {
	return ec.ReadStable(KBBLOffset)
}

// SetBacklight 设置背光档位
func SetBacklight(ec *ECDirect, level byte) error {
	if level >= BacklightLevels {
		return fmt.Errorf("背光档位必须在 0..%d 之间", BacklightLevels-1)
	}
	return ec.Write(KBBLOffset, level)
}
