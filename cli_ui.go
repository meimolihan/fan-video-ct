package main

import (
	"flag"
	"fmt"
	"os"
	"strings"

	"github.com/meimolihan/fan-video-ct/internal/version"
)

// run 解析命令行并分发到对应命令。
func run() error {
	sub, rest := parseCommand(os.Args[1:])

	fs := flag.NewFlagSet("fan-video-ct", flag.ExitOnError)
	dataDir := fs.String("data", "", "数据目录（默认 ./data 或配置 app.data_dir）")
	port := fs.Int("port", 0, "监听端口（默认 8788 或配置 app.port）")
	mediaDir := fs.String("media", "", "视频浏览根目录（默认全盘或配置 app.media_dir）")
	showVersion := fs.Bool("version", false, "打印版本号")
	showVersionV := fs.Bool("v", false, "打印版本号")
	fs.SetOutput(os.Stdout)
	fs.Usage = func() {
		fmt.Fprintln(os.Stdout, "用法: fan-video-ct [命令] [选项]")
		fmt.Fprintln(os.Stdout)
		fmt.Fprintln(os.Stdout, "命令:")
		fmt.Fprintln(os.Stdout, "  status          查看运行状态")
		fmt.Fprintln(os.Stdout, "  uninstall       停止服务并移除安装")
		fmt.Fprintln(os.Stdout, "  （无命令时启动 Web 服务）")
		fmt.Fprintln(os.Stdout)
		fmt.Fprintln(os.Stdout, "选项:")
		fs.PrintDefaults()
	}
	if err := fs.Parse(rest); err != nil {
		return err
	}

	// 允许 -port 0 表示使用配置默认值
	var portPtr *int
	if *port > 0 {
		portPtr = port
	}

	switch {
	case *showVersion || *showVersionV:
		printVersion()
		return nil
	case sub == "status":
		return runStatus()
	case sub == "uninstall":
		return runUninstall()
	case sub == "backup":
		fmt.Fprintln(os.Stderr, "提示: 备份请直接归档数据目录（app.data_dir）。")
		return nil
	case sub == "restore":
		fmt.Fprintln(os.Stderr, "提示: 恢复请将备份解压回数据目录后重启服务。")
		return nil
	default:
		return runServer(*dataDir, portPtr, *mediaDir)
	}
}

// parseCommand 从参数中识别首个子命令并返回剩余参数。
func parseCommand(args []string) (string, []string) {
	if len(args) == 0 {
		return "", args
	}
	switch args[0] {
	case "status", "uninstall", "backup", "restore":
		return args[0], args[1:]
	}
	return "", args
}

// printVersion 输出版本信息。
func printVersion() {
	fmt.Println(version.Current())
}

// confirm 交互式二次确认，默认返回 false。
func confirm(prompt string) bool {
	fmt.Printf("%s [y/N]: ", prompt)
	var input string
	_, err := fmt.Scanln(&input)
	if err != nil {
		return false
	}
	input = strings.TrimSpace(strings.ToLower(input))
	return input == "y" || input == "yes"
}

// truncatePath 过长路径缩写显示。
func truncatePath(s string) string {
	if len(s) <= 60 {
		return s
	}
	return s[:20] + "..." + s[len(s)-37:]
}
