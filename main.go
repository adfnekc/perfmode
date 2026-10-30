// perfmode — 机械革命无界14 Pro 性能模式切换工具
//
// 通过直接访问 EC（Embedded Controller）切换三档性能模式。
//
// 构建:
//   GOOS=windows GOARCH=amd64 CGO_ENABLED=0 go build -o perfmode.exe .
package main

import (
	"mechrevo-perfmode/cmd"
)

func main() {
	cmd.Run()
}
