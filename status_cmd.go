package main

import (
	"fmt"
	"net/http"
	"os"
	"path/filepath"
	"strconv"
	"strings"
	"syscall"

	"github.com/meimolihan/fan-video-ct/internal/config"
	"github.com/meimolihan/fan-video-ct/internal/version"
)

const defaultBinPath = "/var/lib/fan-video-ct/fan-video-ct"
const defaultDataDir = "/var/lib/fan-video-ct/data"
const defaultRecordFile = "/etc/fan-video-ct.conf"
const defaultServiceFile = "/etc/systemd/system/fan-video-ct.service"
const defaultPidFile = "/var/run/fan-video-ct.pid"

// defaultPort 返回默认监听端口。
func defaultPort() int { return 8788 }

// readRecord 解析安装记录文件（install.sh 生成），返回 KEY=VALUE 映射。
func readRecord() map[string]string {
	m := map[string]string{}
	data, err := os.ReadFile(defaultRecordFile)
	if err != nil {
		return m
	}
	for _, line := range strings.Split(string(data), "\n") {
		line = strings.TrimSpace(line)
		if line == "" || strings.HasPrefix(line, "#") {
			continue
		}
		k, v, ok := strings.Cut(line, "=")
		if !ok {
			continue
		}
		m[strings.TrimSpace(k)] = strings.TrimSpace(v)
	}
	return m
}

// floatNull 空值回退为 fallback。
func floatNull(s, fallback string) string {
	if s == "" {
		return fallback
	}
	return s
}

// dirOf 返回路径所在目录。
func dirOf(p string) string { return filepath.Dir(p) }

// runStatus 输出运行状态。
func runStatus() error {
	fmt.Println("==== fan-video-ct 状态 ====")

	rec := readRecord()

	binPath := rec["BIN_PATH"]
	if binPath == "" {
		binPath = defaultBinPath
	}
	instDir := rec["INSTALL_DIR"]
	fmt.Printf("版本: %s\n", version.Current())
	fmt.Printf("安装目录: %s\n", floatNull(instDir, dirOf(binPath)))
	fmt.Printf("安装路径: %s\n", binPath)

	// 端口优先级：安装记录 > 配置文件 > 默认值
	port := defaultPort()
	if v, err := strconv.Atoi(rec["PORT"]); err == nil && v > 0 {
		port = v
	}
	cfg, err := config.Load()
	if err == nil && cfg.App.Port > 0 && rec["PORT"] == "" {
		port = cfg.App.Port
	}
	dataDir := defaultDataDir
	if err == nil && cfg.App.DataDir != "" {
		dataDir = cfg.App.DataDir
	}
	fmt.Printf("数据目录: %s\n", floatNull(rec["DATA_DIR"], dataDir))
	fmt.Printf("监听端口: %d\n", port)
	mediaDir := floatNull(rec["VIDEO_DIR"], "")
	if err == nil && mediaDir == "" {
		mediaDir = cfg.App.MediaDir
	}
	if mediaDir != "" {
		fmt.Printf("视频目录: %s\n", mediaDir)
	}

	if pid, perr := os.ReadFile(defaultPidFile); perr == nil {
		if pidAlive(strings.TrimSpace(string(pid))) {
			fmt.Printf("运行状态: 运行中（PID %s）\n", strings.TrimSpace(string(pid)))
		} else {
			_ = os.Remove(defaultPidFile)
			fmt.Println("运行状态: 未运行（残留 PID 文件已清理）")
		}
	} else {
		fmt.Println("运行状态: 未运行")
	}

	if isSystemd() {
		fmt.Println("服务方式: systemd unit fan-video-ct.service")
	} else {
		fmt.Println("服务方式: 手动启动（未检测到 systemd）")
	}

	if _, herr := http.Get(fmt.Sprintf("http://127.0.0.1:%d/api/health", port)); herr == nil {
		fmt.Printf("健康检查: 正常（http://127.0.0.1:%d/api/health）\n", port)
	} else {
		fmt.Println("健康检查: 无响应")
	}
	return nil
}

// pidAlive 判断 PID 对应的进程是否存活。
func pidAlive(pid string) bool {
	pidNum, err := strconv.Atoi(pid)
	if err != nil || pidNum <= 0 {
		return false
	}
	proc, err := os.FindProcess(pidNum)
	if err != nil {
		return false
	}
	// signal 0 仅检查进程是否存在，不发送实际信号
	return proc.Signal(syscall.Signal(0)) == nil
}

func isSystemd() bool {
	_, err := os.Stat("/run/systemd/system")
	return err == nil
}
