# 实现说明

这份文档记录最终的实现方案，以及走到这个方案所依据的实证过程。

## 1. 目标

在 Windows 上用一个命令行程序切换机械革命 无界14 Pro 的三档性能模式，
并在原厂 OSD 缺失时提供当前模式提示。

## 2. 固件里到底发生了什么

### 2.1 EC 与 ITSM 字段

从机器上导出 SSDT 并反汇编（`iasl -d`）可以看到 EC 设备路径：

```
External (_SB_.PCI0.LPC0.H_EC, DeviceObj)
External (_SB_.PCI0.LPC0.H_EC.ECRD, MethodObj)    // 1 Arguments
External (_SB_.PCI0.LPC0.H_EC.ECWT, MethodObj)    // 2 Arguments
```

`ECRD(RefOf(field))` 读字段、`ECWT(value, RefOf(field))` 写字段。

DSDT 里 EC 的 `OperationRegion` 字段布局（节选）：

```
Offset (0xE0),
DGPU,   8,
SSDK,   8,
KBBL,   8,
BPWM,   8,
ITSM,   8,
```

`0xE0 + 4` ⇒ **`ITSM` = EC RAM 偏移 `0xE4`**。

### 2.2 Fn+F1 做了什么

`_Q01` 是 Fn+F1 触发的 EC 查询处理：

```
If (ECRD(ITSM) == 0)      { ECWT(0x02, ITSM); FNQS(0x02); Notify(WMID, 0x20) }
ElseIf (ECRD(ITSM) == 1)  { ECWT(0,    ITSM); FNQS(0);    Notify(WMID, 0x20) }
ElseIf (ECRD(ITSM) == 2)  { ECWT(1,    ITSM); FNQS(1);    Notify(WMID, 0x20) }
```

所以按键循环是 `0 → 2 → 1 → 0`，并且每次都会发 `Notify(WMID, 0x20)` ——
原厂 OSD 就是订阅这个通知来弹提示的。OSD 没装/坏了，切换仍然生效，只是没有提示。

### 2.3 三档对应的功耗墙

`FNQS()` 把 ITSM 映射到 `THMP` 热表：

| ITSM | 交流供电时 | THMP 里的功耗值 |
|------|-----------|-----------------|
| 0 | THMP[0] | 65000 / 54000 |
| 1 | THMP[1] | 54000 / 50000 |
| 2 | THMP[3] | 45000 / 25000 |

单调递减，所以 **0 = 性能(65W)、1 = 均衡(54W)、2 = 静音(45W)**。
这与内核模块 `mechrevo-wujie14pro-perfmode-kmod` 的枚举一致。

## 3. 为什么不用 ACPI

一开始的方案是用用户态 ACPI IOCTL（`IOCTL_ACPI_EVAL_METHOD_EX`）直接调
`\_SB.PCI0.LPC0.H_EC.ITSM` / `ECWT`。在实机上逐项验证后否掉了：

| 尝试 | 结果 |
|------|------|
| `CreateFile("\\.\ACPI")` | `ERROR_FILE_NOT_FOUND` —— 这个符号链接不存在 |
| `\\.\GLOBALROOT\Device\ACPI` | 同样不存在（枚举 `\Device` 的 521 个对象里没有 ACPI） |
| SetupAPI 枚举 `GUID_DEVINTERFACE_ACPI` | 0 个设备 |
| 注册表 `Control\DeviceClasses` 全量搜索 `ACPI#PNP0C09` / `ACPI#PNP0C14` | 0 条 —— ACPI 设备不注册设备接口 |
| 遍历 `\Device\000000xx` 逐个发 ACPI IOCTL | 能打开 12 个，全部返回「不支持该请求」 |
| `root\WMI` 类枚举（1591 个类） | 没有任何 GUID 命名的类 —— 固件没提供 BMOF |

另外 `ECWT` 的第二个参数是 ACPI Reference，而用户态
`ACPI_METHOD_ARGUMENT` 只有 INTEGER/STRING/BUFFER/PACKAGE 四种类型，
**即使拿到句柄也构造不出 Reference**，写入路径本来就走不通。

结论：Windows 上不存在受支持的、用户态的任意 ACPI 方法调用通道。

## 4. 最终方案：直接读写 EC

### 4.1 端口 I/O

系统里已加载 `inpoutx64.sys`（InpOut v1.2，Highresolution Enterprises），
它把 x86 端口读写暴露成 `\\.\inpoutx64` 上的 IOCTL：

```
IOCTL_READ_PORT_UCHAR  = CTL_CODE(40000, 0x801, METHOD_BUFFERED, FILE_ANY_ACCESS) = 0x9C402004
IOCTL_WRITE_PORT_UCHAR = CTL_CODE(40000, 0x802, METHOD_BUFFERED, FILE_ANY_ACCESS) = 0x9C402008
```

读：输入 2 字节端口地址，输出 1 字节；写：输入 3 字节（端口 + 值）。

### 4.2 EC 时序

```
0x62  数据端口
0x66  命令/状态端口      bit0 = OBF(输出缓冲满)  bit1 = IBF(输入缓冲满)
```

读 `EC[off]`：

```
等 IBF 清零 → 0x66 写 0x80 → 等 IBF 清零 → 0x62 写 off → 等 OBF 置位 → 读 0x62
```

写 `EC[off] = v`：

```
等 IBF 清零 → 0x66 写 0x81 → 等 IBF 清零 → 0x62 写 off → 等 IBF 清零 → 0x62 写 v
```

**关键点：轮询必须忙等，不能 sleep。** OBF 只维持极短时间，
用 100µs 级别的 `sleep` 轮询会 100% 错过（实测 20ms 窗口内只出现 1 次，约在第 11 次紧循环读取）。

### 4.3 偏移的获取方式

`ITSM` 的偏移本来只能从 DSDT 读，而 Windows 的
`GetSystemFirmwareTable('ACPI','DSDT')` 直接返回 0（屏蔽了 DSDT）。

绕过的办法：`FACP` 表里记录了 DSDT 的物理地址（`X_DSDT`），
而 inpoutx64 还提供物理内存映射 IOCTL（`0x9C40201C`），
把该物理地址映射进本进程后即可直接读出 DSDT 再反汇编。

对没有这套工具链的用户，程序内置了 `perfmode calibrate`：
两次 EC 快照做差分，找出随 `Fn+F1` 变化的字节。

## 5. 代码结构

```
main.go                 入口
cmd/root.go             子命令：status / turbo / balance / silence / set /
                        list / watch / probe / calibrate / offset
core/mode.go            模式枚举、原始值映射、配置文件读写
core/backend.go         Backend 接口 + 后端自动探测
                        （EC 直连 > MIFS ACPI-WMI > ACPI-EC）
core/ecdirect.go        EC 读写时序
core/portio.go          inpoutx64 端口 I/O
core/mifs.go            ACPI-WMI：_WDG 解析 + WMxx 调用（其他同方机型可用）
core/acpi.go            ACPI IOCTL 封装（当前机型不可用，保留备用）
core/probe.go           诊断输出
```

后端抽象的意义：`Backend` 只有 `GetRaw` / `SetRaw` 两个核心方法，
换接口不影响上层 CLI。`mifs.go` 与 `acpi.go` 是给别的机型留的通路。

## 6. 后台提示程序（OSD）

`perfmode osd` 是一个无窗口的后台进程：

- 每 300ms 读一次 `EC[0xE4]`（`ReadStable`，连续两次一致才采信）。
- 值变化且落在 0/1/2 时，往 OSD 窗口投递一条消息。
- OSD 是一个 `WS_EX_LAYERED | WS_EX_TOPMOST | WS_EX_TOOLWINDOW | WS_EX_NOACTIVATE`
  的 `WS_POPUP` 窗口，用 GDI 画两行字，`SetTimer` 1.8 秒后自动隐藏。
- 进程按物理像素工作（`SetProcessDpiAwarenessContext(PER_MONITOR_AWARE_V2)`），
  再按 `GetDpiForSystem()` 缩放窗口和字体，避免 200% 缩放下错位。
- 托盘图标用 `Shell_NotifyIcon`，右键菜单「显示当前模式 / 退出」。

托盘菜单是动态构建的：`osd.MenuProvider` 每次右键时被调用，
由 `cmd/osd.go` 按当前模式打勾；`osd.CommandHandler` 负责执行切换/显示传感器/自启开关。

踩过的坑：

- 窗口已经可见时，再次 `ShowWindow` **不会**重绘，必须 `InvalidateRect(hwnd, NULL, TRUE)`
  强制作废客户区，否则连续切换时提示框内容不变。
- 弹窗高度按行数自适应（`SetWindowPos`），否则单行提示会留一大片空白。

- `WS_EX_LAYERED` 窗口默认 alpha 为 0，不调 `SetLayeredWindowAttributes` 就完全不可见。
- `SetBkMode` / `SetTextColor` 在 **gdi32**，不是 user32，绑错 DLL 会 panic。
- 后台进程用 WSL 的 `&` 启动会随 shell 退出被杀，要验证得用
  `Start-Process` 真正脱离。

## 7. 已知限制

- 只验证过 无界14 Pro。其他模具的 EC 偏移不同，需要 `calibrate`。
- 依赖 `inpoutx64.sys`。若系统未加载该驱动，程序会明确报错而不是静默失败。
- 直接写 EC 属于未文档化操作。本工具只写 `0xE4` 一个字节，
  即固件自身 `Fn+F1` 写入的那个字节，不改动其他寄存器。
