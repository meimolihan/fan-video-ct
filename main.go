// fan-video-ct 视频剪切工具：内置 FFmpeg 能力的 Web 服务。
//
// 用法：
//
//	fan-video-ct [命令] [选项]
//
// 命令：
//
//	status          查看运行状态与基础信息
//	uninstall       停止服务并移除安装（systemd / 数据目录需二次确认清理）
//	（无命令时启动 Web 服务）
//
// 选项：
//
//	-data <dir>     数据目录（默认 ./data，或读取配置 app.data_dir）
//	-port <port>    监听端口（默认 8788，或读取配置 app.port）
//	-version / -v   打印版本号并退出
package main

import (
	"fmt"
	"os"
)

func main() {
	if err := run(); err != nil {
		fmt.Fprintln(os.Stderr, err)
		os.Exit(1)
	}
}
