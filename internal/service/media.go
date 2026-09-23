package service

import (
	"context"
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
}

// MediaService 媒体文件浏览与信息探测。
type MediaService struct {
	cfg *config.Config
	ffc *ffmpeg.Client
	log *zap.SugaredLogger

	kfMu   sync.Mutex
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
		fe := FileEntry{
			Name:  name,
			Path:  filepath.Join(dir, name),
			IsDir: e.IsDir(),
			Size:  size,
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
