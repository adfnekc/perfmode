// 诊断输出：把当前能探测到的所有信息打印出来
package core

import (
	"fmt"
	"strings"

	"golang.org/x/sys/windows/registry"
)

// Probe 打印诊断信息
func Probe() {
	fmt.Println("=== 诊断 ===")
	fmt.Println()

	fmt.Println("[环境]")
	fmt.Printf("  管理员: %v\n", IsAdmin())
	fmt.Printf("  厂商:   %s\n", regStr(`HARDWARE\DESCRIPTION\System\BIOS`, "SystemManufacturer"))
	fmt.Printf("  型号:   %s\n", regStr(`HARDWARE\DESCRIPTION\System\BIOS`, "SystemProductName"))
	fmt.Printf("  配置:   %s\n", ConfigPath())
	cfg := LoadConfig()
	if cfg.ECOffset != nil {
		fmt.Printf("  EC 偏移: 0x%02X\n", *cfg.ECOffset)
	} else {
		fmt.Printf("  EC 偏移: 未校准\n")
	}
	fmt.Println()

	fmt.Println("[端口 I/O 驱动 inpoutx64]")
	if io, err := OpenPortIO(); err != nil {
		fmt.Printf("  不可用: %v\n", err)
	} else {
		fmt.Println("  可用")
		for _, p := range []uint16{ECCmdPort, ECDataPort} {
			v, err := io.InB(p)
			if err != nil {
				fmt.Printf("  端口 0x%02X: %v\n", p, err)
			} else {
				fmt.Printf("  端口 0x%02X = 0x%02X\n", p, v)
			}
		}
		io.Close()
	}
	fmt.Println()

	fmt.Println("[EC 直连读测试]")
	if ec, err := OpenECDirect(); err != nil {
		fmt.Printf("  不可用: %v\n", err)
	} else {
		ok := 0
		for i := 0; i < 256; i++ {
			if _, err := ec.Read(byte(i)); err == nil {
				ok++
			}
		}
		fmt.Printf("  256 个偏移中 %d 个读取成功\n", ok)
		ec.Close()
	}
	fmt.Println()

	fmt.Println("[ACPI 设备]")
	if paths, err := EnumerateACPIDevices(); err != nil {
		fmt.Printf("  枚举失败: %v\n", err)
	} else if len(paths) == 0 {
		fmt.Println("  没有可用的 ACPI 设备接口")
		fmt.Println("  （Windows 不向用户态暴露 PNP0C09/PNP0C14 的设备接口，属正常）")
	} else {
		for _, p := range paths {
			fmt.Printf("  %s\n", p)
		}
	}
	fmt.Println()

	fmt.Println("[结论]")
	b, err := OpenBackend(cfg)
	if err != nil {
		fmt.Println("  没有可用后端:")
		for _, line := range strings.Split(err.Error(), "\n") {
			fmt.Printf("    %s\n", line)
		}
	} else {
		defer b.Close()
		if v, err := b.GetRaw(); err == nil {
			fmt.Printf("  后端 %s 可用，当前原始值 %d\n", b.Name(), v)
		} else {
			fmt.Printf("  后端 %s 读取失败: %v\n", b.Name(), err)
		}
	}
}

func regStr(path, name string) string {
	k, err := registry.OpenKey(registry.LOCAL_MACHINE, path, registry.QUERY_VALUE)
	if err != nil {
		return "(未知)"
	}
	defer k.Close()
	v, _, err := k.GetStringValue(name)
	if err != nil {
		return "(未知)"
	}
	return v
}
