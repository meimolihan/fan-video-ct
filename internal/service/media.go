package service

import (
	"context"
	"errors"
	"fmt"
	"net/url"
	"os"
	"path/filepath"
	"sort"
	"strings"
	"sync"

	"github.com/meimolihan/fan-video-ct/internal/config"
	"github.com/meimolihan/fan-video-ct/internal/ffmpeg"
	"go.uber.org/zap"
)

// videoExts 可浏览/可剪切的视频扩展名。
var videoExts = map[string]struct{}{
	".mp4": {}, ".mkv": {}, ".mov": {}, ".avi": {}, ".wmv": {},
	".flv": {}, ".webm": {}, ".m4v": {}, ".ts": {}, ".3gp": {},
	".rmvb": {}, ".mpg": {}, ".mpeg": {}, ".m2ts": {}, ".rm": {},
}

// IsVideoFile 判断文件名是否为支持的视频格式。
func IsVideoFile(name string) bool {
	_, ok := videoExts[strings.ToLower(filepath.Ext(name))]
	return ok
}

// FileEntry 文件浏览条目。
type FileEntry struct {
	Name    string `json:"name"`
	Path    string `json:"path"`
	IsDir   bool   `json:"is_dir"`
	Size    int64  `json:"size"`
	IsVideo bool   `json:"is_video"`
	// Deletable 标记该条目是否允许通过删除接口移除（位于允许删除的根目录内，
	// 且不是根目录本身）。前端据此决定是否渲染删除按钮。
	Deletable bool `json:"deletable"`
}

// MediaService 媒体文件浏览与信息探测。
type MediaService struct {
	cfg *config.Config
	ffc *ffmpeg.Client
	log *zap.SugaredLogger

	kfMu    sync.Mutex
	kfCache map[string][]float64
}

// NewMediaService 创建媒体服务。
func NewMediaService(cfg *config.Config, ffc *ffmpeg.Client, log *zap.SugaredLogger) *MediaService {
	return &MediaService{
		cfg:     cfg,
		ffc:     ffc,
		log:     log,
		kfCache: make(map[string][]float64),
	}
}

// HomeDir 返回文件浏览器的起始目录。
func (m *MediaService) HomeDir() string {
	if dir := strings.TrimSpace(m.cfg.App.MediaDir); dir != "" {
		return filepath.Clean(dir)
	}
	if dir, err := os.Getwd(); err == nil {
		return dir
	}
	return "/"
}

// ListDir 列出目录条目：目录在前，按名称排序，视频文件标记 is_video。
func (m *MediaService) ListDir(dir string) ([]FileEntry, error) {
	if dir == "" {
		dir = m.HomeDir()
	}
	dir = filepath.Clean(dir)
	entries, err := os.ReadDir(dir)
	if err != nil {
		return nil, err
	}
	list := make([]FileEntry, 0, len(entries))
	// 根目录集合预计算一次，避免逐条目重复解析
	roots := m.deletableRoots()
	for _, e := range entries {
		name := e.Name()
		info, err := e.Info()
		if err != nil {
			info = nil
		}
		size := int64(0)
		if info != nil {
			size = info.Size()
		}
		full := filepath.Join(dir, name)
		fe := FileEntry{
			Name:      name,
			Path:      full,
			IsDir:     e.IsDir(),
			Size:      size,
			Deletable: pathDeletable(full, roots),
		}
		if !e.IsDir() {
			// 仅展示可读取的视频文件，便于在浏览器中直接打开
			if IsVideoFile(name) {
				fe.IsVideo = true
				list = append(list, fe)
			}
			continue
		}
		list = append(list, fe)
	}
	sort.Slice(list, func(i, j int) bool {
		if list[i].IsDir != list[j].IsDir {
			return list[i].IsDir
		}
		return strings.ToLower(list[i].Name) < strings.ToLower(list[j].Name)
	})
	return list, nil
}

// Info 探测媒体信息。
func (m *MediaService) Info(ctx context.Context, path string) (*ffmpeg.MediaInfo, error) {
	info, err := m.ffc.Probe(ctx, path)
	if err != nil {
		return nil, err
	}
	if st, err := os.Stat(path); err == nil {
		info.Size = st.Size()
	}
	return info, nil
}

// ResolveStreamURL 将本地磁盘路径编码为可播放的流地址（供前端拼接使用）。
func ResolveStreamURL(path string) string {
	return "/api/media/stream?path=" + url.QueryEscape(path)
}

// Keyframes 返回视频关键帧时间点（带路径级内存缓存）。
// 供前端在"快速剪切"模式下把起止锚点对齐到关键帧，保证输出与预览一致。
func (m *MediaService) Keyframes(ctx context.Context, path string) ([]float64, error) {
	path = filepath.Clean(path)
	m.kfMu.Lock()
	if v, ok := m.kfCache[path]; ok {
		m.kfMu.Unlock()
		return v, nil
	}
	m.kfMu.Unlock()

	keys, err := m.ffc.Keyframes(ctx, path)
	if err != nil {
		return nil, err
	}
	m.kfMu.Lock()
	m.kfCache[path] = keys
	m.kfMu.Unlock()
	return keys, nil
}

// ==================== 删除 ====================
//
// 文件浏览器允许向上导航至任意目录，但删除是不可逆的破坏性操作，
// 因此只放开应用自有目录：媒体目录（含「跟随原视频」的剪切输出）、
// 上传目录、封面目录、输出目录。数据目录根本身不放开，
// 以免误删 config.yaml / 预设 / 日志等应用自身文件。

// deletableRoots 返回允许删除的根目录（绝对路径、已清理、去重）。
// 浏览根为文件系统根 / 时忽略该项，避免把「从 / 浏览」误当成「允许删除全盘」。
func (m *MediaService) deletableRoots() []string {
	candidates := []string{
		m.HomeDir(),
		m.cfg.UploadsDir(),
		m.cfg.CoversDir(),
		m.cfg.OutputDir(),
	}
	roots := make([]string, 0, len(candidates))
	seen := make(map[string]struct{}, len(candidates))
	for _, c := range candidates {
		if strings.TrimSpace(c) == "" {
			continue
		}
		abs, err := filepath.Abs(c)
		if err != nil {
			continue
		}
		abs = filepath.Clean(abs)
		if abs == string(filepath.Separator) {
			continue
		}
		if _, ok := seen[abs]; ok {
			continue
		}
		seen[abs] = struct{}{}
		roots = append(roots, abs)
	}
	return roots
}

// underRoot 判断 path 是否严格位于 root 之内。
// 用 filepath.Rel 而非字符串前缀比较，避免 /data-evil 命中 /data 这类误判。
func underRoot(path, root string) bool {
	rel, err := filepath.Rel(root, path)
	if err != nil || rel == "." || rel == ".." {
		return false
	}
	return !strings.HasPrefix(rel, ".."+string(filepath.Separator))
}

// isUnderRoots 判断 path 是否位于任一 root 之内。
func isUnderRoots(path string, roots []string) bool {
	for _, root := range roots {
		if underRoot(path, root) {
			return true
		}
	}
	return false
}

// resolveTarget 清理路径并解析符号链接，防止经由软链跳出允许目录。
// 路径不存在时按清理后的绝对路径返回，交由后续 os 调用报错。
func resolveTarget(path string) (string, bool) {
	if strings.TrimSpace(path) == "" {
		return "", false
	}
	target := filepath.Clean(path)
	if !filepath.IsAbs(target) {
		abs, err := filepath.Abs(target)
		if err != nil {
			return "", false
		}
		target = abs
	}
	if resolved, err := filepath.EvalSymlinks(target); err == nil {
		target = resolved
	}
	return target, true
}

// pathDeletable 用预计算的根目录集合判断单个路径是否可删除。
func pathDeletable(path string, roots []string) bool {
	target, ok := resolveTarget(path)
	if !ok {
		return false
	}
	return isUnderRoots(target, roots)
}

// IsDeletable 判断路径是否允许删除（供 ListDir 标记条目与外部校验使用）。
func (m *MediaService) IsDeletable(path string) bool {
	return pathDeletable(path, m.deletableRoots())
}

// Remove 删除文件或目录（目录递归删除）。返回实际删除的路径。
// 仅允许删除位于 deletableRoots 内的内容，且不允许删除根目录本身。
func (m *MediaService) Remove(path string) (string, error) {
	target, ok := resolveTarget(path)
	if !ok {
		return "", errors.New("缺少 path 参数")
	}
	if !isUnderRoots(target, m.deletableRoots()) {
		return "", fmt.Errorf("出于安全考虑，仅允许删除媒体、上传、封面与输出目录内的内容: %s", target)
	}

	// 删除时使用清理后的原始路径而非解析结果：目标为符号链接时只移除链接本身，
	// 不跟随其指向的目标，避免删掉链接外的文件。
	orig := filepath.Clean(path)
	if abs, err := filepath.Abs(orig); err == nil {
		orig = abs
	}
	if _, err := os.Lstat(orig); err != nil {
		if os.IsNotExist(err) {
			return "", fmt.Errorf("目标不存在: %s", target)
		}
		return "", fmt.Errorf("无法访问目标: %w", err)
	}
	if err := os.RemoveAll(orig); err != nil {
		return "", fmt.Errorf("删除失败: %w", err)
	}
	return target, nil
}
