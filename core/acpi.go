// Package core 提供 Windows 用户态 ACPI 访问能力。
//
// 设计依据（全部来自真实固件与内核实现，不是猜的）:
//
//  1. IOCTL 码来自 Windows SDK 的 acpiioct.h:
//     IOCTL_ACPI_EVAL_METHOD_V1    = CTL_CODE(FILE_DEVICE_ACPI, 1, METHOD_BUFFERED, FILE_READ|FILE_WRITE)
//     IOCTL_ACPI_EVAL_METHOD_V1_EX = CTL_CODE(FILE_DEVICE_ACPI, 6, METHOD_BUFFERED, FILE_READ|FILE_WRITE)
//
//  2. 缓冲区签名来自同一头文件（MSVC 多字符常量按大端打包）:
//     'AieA' = 简单输入 EX      'FieA' = 复杂输入 EX
//     'BieA' = 简单输入 相对    'CieA' = 复杂输入 相对
//     'BoeA' = 输出
//
//  3. 参数类型: INTEGER=0 STRING=1 BUFFER=2 PACKAGE=3 PACKAGE_EX=4
//
//  4. ACPI 设备句柄通过 SetupAPI 枚举 GUID_DEVINTERFACE_ACPI
//     {d4500b1b-2cfa-4c4c-9e6c-6fd1b304d97a} 得到，形如:
//     \\?\ACPI#PNP0C14#MIFS#{d4500b1b-2cfa-4c4c-9e6c-6fd1b304d97a}
//
// 参考:
//   https://learn.microsoft.com/en-us/windows-hardware/drivers/ddi/acpiioct/
//   https://github.com/wlmqljj/mechrevo-wujie14pro-perfmode-kmod
package core

import (
	"encoding/binary"
	"fmt"
	"strings"
	"syscall"
	"unsafe"

	"golang.org/x/sys/windows"
)

// ============================================================
// 常量
// ============================================================

const (
	fileDeviceACPI = 0x00000032

	// CTL_CODE(0x32, fn, METHOD_BUFFERED=0, FILE_READ_ACCESS|FILE_WRITE_ACCESS=3)
	ioctlEvalMethod   = 0x0032C004 // fn = 1
	ioctlEvalMethodEx = 0x0032C018 // fn = 6
)

// ACPI_METHOD_ARGUMENT 类型
const (
	argInteger = 0x0
	argString  = 0x1
	argBuffer  = 0x2
	argPackage = 0x3
)

// 缓冲区签名
const (
	sigSimpleEx  = 0x41696541 // 'AieA'
	sigSimple    = 0x42696541 // 'BieA'
	sigComplexEx = 0x46696541 // 'FieA'
	sigComplex   = 0x43696541 // 'CieA'
	sigOutput    = 0x426F6541 // 'BoeA'
)

// guidACPIDeviceInterface 是 GUID_DEVINTERFACE_ACPI
var guidACPIDeviceInterface = windows.GUID{
	Data1: 0xd4500b1b,
	Data2: 0x2cfa,
	Data3: 0x4c4c,
	Data4: [8]byte{0x9e, 0x6c, 0x6f, 0xd1, 0xb3, 0x04, 0xd9, 0x7a},
}

// ============================================================
// 参数
// ============================================================

// Arg 表示一个传给 ACPI 方法的参数
type Arg struct {
	Type uint16
	Data []byte
}

// IntArg 构造整数参数
func IntArg(v uint32) Arg {
	b := make([]byte, 4)
	binary.LittleEndian.PutUint32(b, v)
	return Arg{Type: argInteger, Data: b}
}

// BufferArg 构造缓冲区参数
func BufferArg(b []byte) Arg {
	cp := make([]byte, len(b))
	copy(cp, b)
	return Arg{Type: argBuffer, Data: cp}
}

// Uint32 把参数按无符号 32 位整数解读
func (a Arg) Uint32() uint32 {
	if len(a.Data) >= 4 {
		return binary.LittleEndian.Uint32(a.Data)
	}
	var v uint32
	for i := 0; i < len(a.Data); i++ {
		v |= uint32(a.Data[i]) << (8 * i)
	}
	return v
}

// argLen 返回参数在缓冲区中占用的字节数
// ACPI_METHOD_ARGUMENT_LENGTH(n) = 4 + max(4, n)
func argLen(a Arg) int {
	n := len(a.Data)
	if n < 4 {
		n = 4
	}
	return 4 + n
}

// ============================================================
// 设备
// ============================================================

// ACPIDevice 表示一个已打开的 ACPI 命名空间设备
type ACPIDevice struct {
	handle windows.Handle
	path   string
}

// NewDeviceFromHandle 用一个已存在的句柄构造 ACPIDevice
func NewDeviceFromHandle(h windows.Handle, path string) *ACPIDevice {
	return &ACPIDevice{handle: h, path: path}
}

// Path 返回设备接口路径
func (d *ACPIDevice) Path() string { return d.path }

// Close 关闭句柄
func (d *ACPIDevice) Close() error {
	if d.handle == 0 {
		return nil
	}
	err := windows.CloseHandle(d.handle)
	d.handle = 0
	return err
}

// Eval 评估一个 ACPI 方法。
// method 以 '\' 开头时视为绝对路径，走 EX IOCTL；否则视为当前设备下的 4 字符方法名。
func (d *ACPIDevice) Eval(method string, args ...Arg) ([]Arg, error) {
	if strings.HasPrefix(method, `\`) {
		return d.evalEx(method, args...)
	}
	return d.evalRel(method, args...)
}

func (d *ACPIDevice) evalEx(method string, args ...Arg) ([]Arg, error) {
	var buf []byte
	if len(args) == 0 {
		// ACPI_EVAL_INPUT_BUFFER_V1_EX { ULONG Signature; CHAR MethodName[256]; }
		buf = make([]byte, 4+256)
		binary.LittleEndian.PutUint32(buf[0:], sigSimpleEx)
		copy(buf[4:], method)
	} else {
		// ACPI_EVAL_INPUT_BUFFER_COMPLEX_V1_EX
		//   { ULONG Signature; CHAR MethodName[256]; ULONG Size; ULONG ArgumentCount; Argument[]; }
		const hdr = 4 + 256 + 4 + 4
		total := hdr
		for _, a := range args {
			total += argLen(a)
		}
		buf = make([]byte, total)
		binary.LittleEndian.PutUint32(buf[0:], sigComplexEx)
		copy(buf[4:], method)
		binary.LittleEndian.PutUint32(buf[260:], uint32(total))
		binary.LittleEndian.PutUint32(buf[264:], uint32(len(args)))
		off := hdr
		for _, a := range args {
			writeArg(buf[off:], a)
			off += argLen(a)
		}
	}
	return d.ioctl(ioctlEvalMethodEx, buf)
}

func (d *ACPIDevice) evalRel(method string, args ...Arg) ([]Arg, error) {
	name := []byte(method)
	if len(name) != 4 {
		return nil, fmt.Errorf("相对方法名必须是 4 个字符: %q", method)
	}
	var buf []byte
	if len(args) == 0 {
		// ACPI_EVAL_INPUT_BUFFER_V1 { ULONG Signature; UCHAR MethodName[4]; }
		buf = make([]byte, 8)
		binary.LittleEndian.PutUint32(buf[0:], sigSimple)
		copy(buf[4:], name)
	} else {
		// ACPI_EVAL_INPUT_BUFFER_COMPLEX_V1
		//   { ULONG Signature; UCHAR MethodName[4]; ULONG Size; ULONG ArgumentCount; Argument[]; }
		const hdr = 4 + 4 + 4 + 4
		total := hdr
		for _, a := range args {
			total += argLen(a)
		}
		buf = make([]byte, total)
		binary.LittleEndian.PutUint32(buf[0:], sigComplex)
		copy(buf[4:], name)
		binary.LittleEndian.PutUint32(buf[8:], uint32(total))
		binary.LittleEndian.PutUint32(buf[12:], uint32(len(args)))
		off := hdr
		for _, a := range args {
			writeArg(buf[off:], a)
			off += argLen(a)
		}
	}
	return d.ioctl(ioctlEvalMethod, buf)
}

func writeArg(dst []byte, a Arg) {
	binary.LittleEndian.PutUint16(dst[0:], a.Type)
	binary.LittleEndian.PutUint16(dst[2:], uint16(len(a.Data)))
	copy(dst[4:], a.Data)
}

func (d *ACPIDevice) ioctl(code uint32, in []byte) ([]Arg, error) {
	out := make([]byte, 16384)
	var returned uint32
	err := windows.DeviceIoControl(
		d.handle, code,
		&in[0], uint32(len(in)),
		&out[0], uint32(len(out)),
		&returned, nil,
	)
	if err != nil {
		return nil, err
	}
	if returned == 0 {
		return nil, nil
	}
	return parseOutput(out[:returned])
}

// parseOutput 解析 ACPI_EVAL_OUTPUT_BUFFER_V1
// { ULONG Signature; ULONG Length; ULONG Count; ACPI_METHOD_ARGUMENT_V1 Argument[]; }
func parseOutput(data []byte) ([]Arg, error) {
	if len(data) < 12 {
		return nil, fmt.Errorf("输出缓冲区过短: %d 字节", len(data))
	}
	sig := binary.LittleEndian.Uint32(data[0:])
	if sig != sigOutput {
		return nil, fmt.Errorf("输出签名不符: 0x%08X (期望 0x%08X)", sig, sigOutput)
	}
	length := int(binary.LittleEndian.Uint32(data[4:]))
	count := int(binary.LittleEndian.Uint32(data[8:]))
	if length > len(data) || length < 12 {
		length = len(data)
	}
	args := make([]Arg, 0, count)
	off := 12
	for i := 0; i < count; i++ {
		if off+4 > length {
			break
		}
		t := binary.LittleEndian.Uint16(data[off:])
		dl := int(binary.LittleEndian.Uint16(data[off+2:]))
		n := dl
		if n < 4 {
			n = 4
		}
		if off+4+n > length {
			break
		}
		a := Arg{Type: t, Data: append([]byte(nil), data[off+4:off+4+dl]...)}
		args = append(args, a)
		off += 4 + n
	}
	return args, nil
}

// ============================================================
// 设备枚举
// ============================================================

// EnumerateACPIDevices 返回系统中所有 ACPI 设备接口路径
func EnumerateACPIDevices() ([]string, error) {
	dll := syscall.NewLazyDLL("setupapi.dll")
	procGetClassDevs := dll.NewProc("SetupDiGetClassDevsW")
	procEnumInterfaces := dll.NewProc("SetupDiEnumDeviceInterfaces")
	procGetDetail := dll.NewProc("SetupDiGetDeviceInterfaceDetailW")
	procDestroy := dll.NewProc("SetupDiDestroyDeviceInfoList")

	const digcfPresentDeviceInterface = 0x12 // DIGCF_PRESENT | DIGCF_DEVICEINTERFACE

	h, _, callErr := procGetClassDevs.Call(
		uintptr(unsafe.Pointer(&guidACPIDeviceInterface)),
		0, 0,
		digcfPresentDeviceInterface,
	)
	if h == uintptr(syscall.InvalidHandle) || h == 0 {
		return nil, fmt.Errorf("SetupDiGetClassDevs 失败: %v", callErr)
	}
	defer procDestroy.Call(h)

	// SP_DEVICE_INTERFACE_DATA
	var ifaceData struct {
		CbSize             uint32
		InterfaceClassGuid windows.GUID
		Flags              uint32
		Reserved           uintptr
	}
	ifaceData.CbSize = uint32(unsafe.Sizeof(ifaceData))

	var paths []string
	for i := 0; ; i++ {
		r, _, _ := procEnumInterfaces.Call(
			h, 0,
			uintptr(unsafe.Pointer(&guidACPIDeviceInterface)),
			uintptr(i),
			uintptr(unsafe.Pointer(&ifaceData)),
		)
		if r == 0 {
			break
		}

		var need uint32
		procGetDetail.Call(h, uintptr(unsafe.Pointer(&ifaceData)), 0, 0,
			uintptr(unsafe.Pointer(&need)), 0)
		if need == 0 || need > 65536 {
			continue
		}

		detail := make([]byte, need)
		// SP_DEVICE_INTERFACE_DETAIL_DATA_W.cbSize: 32 位为 6，64 位为 8
		cbSize := uint32(6)
		if unsafe.Sizeof(uintptr(0)) == 8 {
			cbSize = 8
		}
		*(*uint32)(unsafe.Pointer(&detail[0])) = cbSize

		r, _, _ = procGetDetail.Call(
			h,
			uintptr(unsafe.Pointer(&ifaceData)),
			uintptr(unsafe.Pointer(&detail[0])),
			uintptr(need),
			uintptr(unsafe.Pointer(&need)),
			0,
		)
		if r == 0 {
			continue
		}
		p := windows.UTF16PtrToString((*uint16)(unsafe.Pointer(&detail[4])))
		if p != "" {
			paths = append(paths, p)
		}
	}
	return paths, nil
}

// OpenACPIDevice 打开指定路径的 ACPI 设备
func OpenACPIDevice(path string) (*ACPIDevice, error) {
	ptr, err := windows.UTF16PtrFromString(path)
	if err != nil {
		return nil, err
	}
	h, err := windows.CreateFile(
		ptr,
		windows.GENERIC_READ|windows.GENERIC_WRITE,
		windows.FILE_SHARE_READ|windows.FILE_SHARE_WRITE,
		nil,
		windows.OPEN_EXISTING,
		0, 0,
	)
	if err != nil {
		return nil, err
	}
	return &ACPIDevice{handle: h, path: path}, nil
}

// ============================================================
// 辅助
// ============================================================

// GUIDString 把 windows.GUID 格式化为标准字符串
func GUIDString(g windows.GUID) string {
	return fmt.Sprintf("{%08X-%04X-%04X-%02X%02X-%02X%02X%02X%02X%02X%02X}",
		g.Data1, g.Data2, g.Data3,
		g.Data4[0], g.Data4[1], g.Data4[2], g.Data4[3],
		g.Data4[4], g.Data4[5], g.Data4[6], g.Data4[7])
}

// IsAdmin 检查当前进程是否以管理员权限运行
func IsAdmin() bool {
	var sid *windows.SID
	ntAuthority := windows.SECURITY_NT_AUTHORITY
	if err := windows.AllocateAndInitializeSid(
		&ntAuthority, 2,
		windows.SECURITY_BUILTIN_DOMAIN_RID,
		windows.DOMAIN_ALIAS_RID_ADMINS,
		0, 0, 0, 0, 0, 0, &sid,
	); err != nil {
		return false
	}
	defer windows.FreeSid(sid)

	var token windows.Token
	if err := windows.OpenProcessToken(windows.CurrentProcess(), windows.TOKEN_QUERY, &token); err != nil {
		return false
	}
	defer token.Close()

	member, err := token.IsMember(sid)
	return err == nil && member
}
