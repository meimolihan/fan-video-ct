package service

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"fmt"
	"io"
	"os"
	"path/filepath"
	"strings"

	"github.com/meimolihan/fan-video-ct/internal/ffmpeg"
	"go.uber.org/zap"
)

// CoverService 封面管理：预览当前帧、截取帧生成封面、上传自定义封面、
// 展示/替换已有封面。封面以视频路径的 SHA256 作为索引存入 <data>/covers。
type CoverService struct {
	ffc       *ffmpeg.Client
	coversDir string
	log       *zap.SugaredLogger
}

// NewCoverService 创建封面服务。
func NewCoverService(ffc *ffmpeg.Client, coversDir string, log *zap.SugaredLogger) *CoverService {
	return &CoverService{ffc: ffc, coversDir: coversDir, log: log}
}

// key 视频路径 -> 封面存储文件名（去扩展名）。
func (c *CoverService) key(videoPath string) string {
	sum := sha256.Sum256([]byte(videoPath))
	return hex.EncodeToString(sum[:8])
}

// CoverPath 返回已存在的封面文件完整路径；不存在时返回空串。
// 兼容 jpg / png / webp 任一种存储格式。
func (c *CoverService) CoverPath(videoPath string) string {
	base := filepath.Join(c.coversDir, c.key(videoPath)+".")
	for _, ext := range []string{"jpg", "png", "webp"} {
		if st, err := os.Stat(base + ext); err == nil && !st.IsDir() {
			return base + ext
		}
	}
	return ""
}

// PreviewFrame 预览指定时间的视频帧，返回图片字节与 Content-Type。
func (c *CoverService) PreviewFrame(ctx context.Context, videoPath string, at float64) ([]byte, string, error) {
	return c.ffc.CaptureFrameBytes(ctx, videoPath, at)
}

// SetFromFrame 将 videoPath 在 at 秒处的帧截取并保存为封面（替换已有封面）。
func (c *CoverService) SetFromFrame(ctx context.Context, videoPath string, at float64) (string, error) {
	out, _, err := c.PreviewFrame(ctx, videoPath, at)
	if err != nil {
		return "", err
	}
	return c.writeCover(videoPath, out, ".jpg")
}

// SetFromUpload 将上传的图片保存为封面（替换已有封面）。
// 支持 jpg / png / webp；写入前移除同视频的历史封面，保证只有一份当前封面。
func (c *CoverService) SetFromUpload(videoPath string, src io.Reader, contentType string) (string, error) {
	ext := extensionFromContentType(contentType)
	if ext == "" {
		ext = ".jpg"
	}
	data, err := io.ReadAll(io.LimitReader(src, 32<<20))
	if err != nil {
		return "", fmt.Errorf("读取上传图片失败: %w", err)
	}
	if len(data) == 0 {
		return "", fmt.Errorf("上传图片为空")
	}
	return c.writeCover(videoPath, data, ext)
}

// Remove 移除已存在的封面（不影响原视频）。
func (c *CoverService) Remove(videoPath string) error {
	old := c.CoverPath(videoPath)
	if old == "" {
		return nil
	}
	return os.Remove(old)
}

// writeCover 写入封面并清理同视频的旧封面（不同扩展名）。
func (c *CoverService) writeCover(videoPath string, data []byte, ext string) (string, error) {
	if err := os.MkdirAll(c.coversDir, 0755); err != nil {
		return "", err
	}
	// 移除旧封面（其它扩展名）
	old := c.CoverPath(videoPath)
	if old != "" {
		_ = os.Remove(old)
	}
	dst := filepath.Join(c.coversDir, c.key(videoPath)+ext)
	if err := os.WriteFile(dst, data, 0644); err != nil {
		return "", err
	}
	return dst, nil
}

// extensionFromContentType 由 MIME 类型推导扩展名。
func extensionFromContentType(ct string) string {
	switch strings.ToLower(strings.TrimSpace(strings.SplitN(ct, ";", 2)[0])) {
	case "image/jpeg", "image/jpg":
		return ".jpg"
	case "image/png":
		return ".png"
	case "image/webp":
		return ".webp"
	case "image/gif":
		return ".jpg"
	default:
		return ""
	}
}
