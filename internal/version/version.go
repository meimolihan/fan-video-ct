package version

import (
	"os"
	"strings"
)

// Version 由发布构建通过 -ldflags 注入；本地未注入时使用默认值。
var Version = "1.1.2"

// Current 返回当前应用版本（统一不含 v 前缀，展示层自行拼接 v）。
// 优先级：FCT_VERSION > APP_VERSION > 内置 Version。
// 兼容 tag 形如 v1.0.7 的误传：开头多余的 v 一律剥掉，避免 Web/CLI 出现 vvX.Y.Z。
func Current() string {
	v := Version
	if envVersion := os.Getenv("FCT_VERSION"); envVersion != "" {
		v = envVersion
	} else if envVersion := os.Getenv("APP_VERSION"); envVersion != "" {
		v = envVersion
	}
	return strings.TrimPrefix(v, "v")
}
