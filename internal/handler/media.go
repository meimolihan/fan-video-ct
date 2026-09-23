package handler

import (
	"context"
	"net/http"
	"os"
	"path/filepath"
	"strings"
	"time"

	"github.com/gin-gonic/gin"
	"go.uber.org/zap"

	"github.com/meimolihan/fan-video-ct/internal/service"
)

type mediaHandler struct {
	svc *service.Service
	log *zap.SugaredLogger
}

// listDir 浏览目录 GET /api/media/dir?path=/foo
func (h *mediaHandler) listDir(c *gin.Context) {
	dir := c.Query("path")
	list, err := h.svc.Media.ListDir(dir)
	if err != nil {
		h.log.Warnf("浏览目录失败 %s: %v", dir, err)
		c.JSON(http.StatusBadRequest, gin.H{"error": "无法读取目录: " + err.Error()})
		return
	}
	c.JSON(http.StatusOK, gin.H{
		"path":    h.svc.Media.HomeDir(),
		"current": normalizePath(dir),
		"up":      upPath(dir),
		"items":   list,
	})
}

// normalizePath 返回空串对应的实际根路径展示值。
func normalizePath(dir string) string {
	if dir == "" {
		return ""
	}
	return filepath.Clean(dir)
}

// upPath 返回上一级目录路径；已在根目录时返回空串。
func upPath(dir string) string {
	if strings.TrimSpace(dir) == "" {
		return ""
	}
	cleaned := filepath.Clean(dir)
	parent := filepath.Dir(cleaned)
	if parent == cleaned {
		return ""
	}
	return parent
}

// info 探测媒体信息 GET /api/media/info?path=
func (h *mediaHandler) info(c *gin.Context) {
	p := c.Query("path")
	if strings.TrimSpace(p) == "" {
		c.JSON(http.StatusBadRequest, gin.H{"error": "缺少 path 参数"})
		return
	}
	ctx, cancel := context.WithTimeout(c.Request.Context(), 30*time.Second)
	defer cancel()
	info, err := h.svc.Media.Info(ctx, p)
	if err != nil {
		c.JSON(http.StatusBadRequest, gin.H{"error": err.Error()})
		return
	}
	c.JSON(http.StatusOK, gin.H{
		"info":     info,
		"hasCover": h.svc.Covers.CoverPath(p) != "",
		"stream":   service.ResolveStreamURL(p),
	})
}

// keyframes 关键帧列表（供 copy 模式对齐） GET /api/media/keyframes?path=
func (h *mediaHandler) keyframes(c *gin.Context) {
	p := c.Query("path")
	if strings.TrimSpace(p) == "" {
		c.JSON(http.StatusBadRequest, gin.H{"error": "缺少 path 参数"})
		return
	}
	ctx, cancel := context.WithTimeout(c.Request.Context(), 120*time.Second)
	defer cancel()
	keys, err := h.svc.Media.Keyframes(ctx, p)
	if err != nil {
		h.log.Warnf("读取关键帧失败 %s: %v", p, err)
		c.JSON(http.StatusBadRequest, gin.H{"error": err.Error()})
		return
	}
	c.JSON(http.StatusOK, gin.H{"path": p, "keyframes": keys})
}

// stream 播放视频流 GET /api/media/stream?path=（支持 HTTP Range）
func (h *mediaHandler) stream(c *gin.Context) {
	p := c.Query("path")
	if strings.TrimSpace(p) == "" {
		c.Status(http.StatusBadRequest)
		return
	}
	f, err := os.Open(p)
	if err != nil {
		if os.IsNotExist(err) {
			c.Status(http.StatusNotFound)
		} else {
			c.Status(http.StatusBadRequest)
		}
		return
	}
	defer f.Close()
	st, err := f.Stat()
	if err != nil || st.IsDir() {
		c.Status(http.StatusNotFound)
		return
	}
	ct := mimeTypeForExt(filepath.Ext(p))
	if ct != "" {
		c.Header("Content-Type", ct)
	}
	c.Header("Accept-Ranges", "bytes")
	http.ServeContent(c.Writer, c.Request, filepath.Base(p), st.ModTime(), f)
}

// mimeTypeForExt 简易 MIME 映射（浏览器播放用）。
func mimeTypeForExt(ext string) string {
	switch strings.ToLower(ext) {
	case ".mp4", ".m4v":
		return "video/mp4"
	case ".webm":
		return "video/webm"
	case ".mov":
		return "video/quicktime"
	case ".ts":
		return "video/mp2t"
	case ".avi":
		return "video/x-msvideo"
	case ".mkv", ".flv", ".wmv", ".rmvb", ".rm", ".mpg", ".mpeg", ".m2ts", ".3gp":
		return "application/octet-stream"
	default:
		return ""
	}
}
