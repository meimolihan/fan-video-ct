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
	// 浏览器根目录（未指定 path 或等于 HomeDir）时注入「我的上传」虚拟目录项，
	// 上传的视频可直接浏览播放，无需手动找路径（纯前端展示，不落盘）
	cur := normalizePath(dir)
	if cur == "" || samePath(cur, h.svc.Media.HomeDir()) {
		list = append([]service.FileEntry{{
			Name:  "📤 我的上传",
			Path:  h.svc.Cfg.UploadsDir(),
			IsDir: true,
		}}, list...)
	}
	c.JSON(http.StatusOK, gin.H{
		"path":    h.svc.Media.HomeDir(),
		"current": cur,
		"up":      upPath(dir),
		"items":   list,
	})
}

// samePath 判断两个路径是否指向同一目录（清理后比较）。
func samePath(a, b string) bool {
	return filepath.Clean(a) == filepath.Clean(b)
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
	// 是否有封面：自定义封面文件，或视频内嵌封面（attached_pic）
	hasCover := h.svc.Covers.CoverPath(p) != ""
	if !hasCover {
		for _, s := range info.Streams {
			if s.IsAttachedPic {
				hasCover = true
				break
			}
		}
	}
	c.JSON(http.StatusOK, gin.H{
		"info":     info,
		"hasCover": hasCover,
		"stream":   service.ResolveStreamURL(p),
	})
}

// remove 删除文件或目录 DELETE /api/media/file?path=
// 目录按递归删除处理；路径须位于应用自有目录内（见 service.MediaService.Remove）。
func (h *mediaHandler) remove(c *gin.Context) {
	p := strings.TrimSpace(c.Query("path"))
	if p == "" {
		c.JSON(http.StatusBadRequest, gin.H{"error": "缺少 path 参数"})
		return
	}
	// 先判定是否为目录：删除后视频已不存在，封面清理需据此跳过目录
	st, err := os.Lstat(filepath.Clean(p))
	isDir := err == nil && st.IsDir()

	target, err := h.svc.Media.Remove(p)
	if err != nil {
		h.log.Warnf("删除失败 %s: %v", p, err)
		c.JSON(http.StatusBadRequest, gin.H{"error": err.Error()})
		return
	}
	// 视频被删除时同步移除其自定义封面，避免在 covers 目录留下孤儿文件
	if !isDir {
		if cerr := h.svc.Covers.Remove(target); cerr != nil {
			h.log.Warnf("清理封面失败 %s: %v", target, cerr)
		}
	}
	h.log.Infof("已删除: %s", target)
	c.JSON(http.StatusOK, gin.H{"ok": true, "path": target})
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

// mimeTypeForExt 简易 MIME 映射（浏览器播放与下载用）。
func mimeTypeForExt(ext string) string {
	switch strings.ToLower(ext) {
	case ".mp4", ".m4v", ".mp4v", ".f4v":
		return "video/mp4"
	case ".webm":
		return "video/webm"
	case ".mov":
		return "video/quicktime"
	case ".ts", ".m2ts", ".mts":
		return "video/mp2t"
	case ".mkv":
		return "video/x-matroska"
	case ".avi":
		return "video/x-msvideo"
	case ".flv":
		return "video/x-flv"
	case ".wmv":
		return "video/x-ms-wmv"
	case ".mpg", ".mpeg":
		return "video/mpeg"
	case ".3gp":
		return "video/3gpp"
	case ".rm", ".rmvb":
		return "video/vnd.rn-realvideo"
	default:
		return ""
	}
}
