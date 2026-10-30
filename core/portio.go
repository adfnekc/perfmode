// 端口 I/O：通过已加载的 inpoutx64 驱动（Highresolution Enterprises, InpOut v1.2）
// 从用户态读写 x86 I/O 端口。
//
// 驱动设备名 \\.\inpoutx64，IOCTL 定义见其源码 driver/hwinterfacedrv.h:
//
//	IOCTL_READ_PORT_UCHAR  = CTL_CODE(40000, 0x801, METHOD_BUFFERED, FILE_ANY_ACCESS) = 0x9C402004
//	IOCTL_WRITE_PORT_UCHAR = CTL_CODE(40000, 0x802, METHOD_BUFFERED, FILE_ANY_ACCESS) = 0x9C402008
//
// 缓冲区约定（METHOD_BUFFERED，输入输出共用 SystemBuffer）:
//
//	读: 输入 2 字节 = 端口地址(USHORT)，输出 1 字节 = 读到的值
//	写: 输入 3 字节 = 端口地址(USHORT) + 值(UCHAR)
package core

import (
	"encoding/binary"
	"fmt"

	"golang.org/x/sys/windows"
)

const (
	ioctlReadPortUchar  = 0x9C402004
	ioctlWritePortUchar = 0x9C402008

	inpoutDeviceName = `\\.\inpoutx64`
)

// PortIO 是 inpoutx64 驱动的句柄封装
type PortIO struct {
	handle windows.Handle
}

// OpenPortIO 打开 inpoutx64 驱动
func OpenPortIO() (*PortIO, error) {
	ptr, err := windows.UTF16PtrFromString(inpoutDeviceName)
	if err != nil {
		return nil, err
	}
	h, err := windows.CreateFile(ptr,
		windows.GENERIC_READ|windows.GENERIC_WRITE,
		0, nil, windows.OPEN_EXISTING, 0, 0)
	if err != nil {
		return nil, fmt.Errorf("打开 %s 失败（inpoutx64 驱动未加载?）: %w", inpoutDeviceName, err)
	}
	return &PortIO{handle: h}, nil
}

// Close 关闭驱动句柄
func (p *PortIO) Close() error {
	if p.handle == 0 {
		return nil
	}
	err := windows.CloseHandle(p.handle)
	p.handle = 0
	return err
}

// InB 读一个字节
func (p *PortIO) InB(port uint16) (byte, error) {
	var in [2]byte
	binary.LittleEndian.PutUint16(in[:], port)
	var out [1]byte
	var returned uint32
	if err := windows.DeviceIoControl(p.handle, ioctlReadPortUchar,
		&in[0], uint32(len(in)), &out[0], uint32(len(out)), &returned, nil); err != nil {
		return 0, err
	}
	return out[0], nil
}

// OutB 写一个字节
func (p *PortIO) OutB(port uint16, v byte) error {
	var in [3]byte
	binary.LittleEndian.PutUint16(in[:], port)
	in[2] = v
	var returned uint32
	return windows.DeviceIoControl(p.handle, ioctlWritePortUchar,
		&in[0], uint32(len(in)), nil, 0, &returned, nil)
}
