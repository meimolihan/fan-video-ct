package handler

import (
	"context"
	"net/http"
	"net/url"
	"path/filepath"
	"time"

	"github.com/gin-gonic/gin"
	"go.uber.org/zap"

	"github.com/meimolihan/fan-video-ct/internal/service"
)

type cutTaskHandler struct {
	svc *service.Service
	log *zap.SugaredLogger
}

// create POST /api/tasks  body: {path, mode, start, end, output_name}
func (h *cutTaskHandler) create(c *gin.Context) {
	var req struct {
		Path       string  `json:"path"`
		Mode       string  `json:"mode"`
		Start      float64 `json:"start"`
		End        float64 `json:"end"`
		OutputName string  `json:"output_name"`
	}
	if err := c.ShouldBindJSON(&req); err != nil {
		c.JSON(http.StatusBadRequest, gin.H{"error": "请求参数无效: " + err.Error()})
		return
	}

	meta := service.TaskMeta{
		Path:       req.Path,
		Mode:       req.Mode,
		Start:      req.Start,
		End:        req.End,
		OutputName: req.OutputName,
	}
	ctx, cancel := context.WithTimeout(c.Request.Context(), 60*time.Second)
	defer cancel()
	task, err := h.svc.Tasks.Create(ctx, meta)
	if err != nil {
		c.JSON(http.StatusBadRequest, gin.H{"error": err.Error()})
		return
	}
	c.JSON(http.StatusOK, gin.H{"task": task})
}

// list GET /api/tasks
func (h *cutTaskHandler) list(c *gin.Context) {
	c.JSON(http.StatusOK, gin.H{"tasks": h.svc.Tasks.List()})
}

// get GET /api/tasks/:id
func (h *cutTaskHandler) get(c *gin.Context) {
	t, ok := h.svc.Tasks.Get(c.Param("id"))
	if !ok {
		c.JSON(http.StatusNotFound, gin.H{"error": "任务不存在"})
		return
	}
	c.JSON(http.StatusOK, gin.H{"task": t})
}

// cancel DELETE /api/tasks/:id
func (h *cutTaskHandler) cancel(c *gin.Context) {
	if err := h.svc.Tasks.Cancel(c.Param("id")); err != nil {
		c.JSON(http.StatusBadRequest, gin.H{"error": err.Error()})
		return
	}
	c.JSON(http.StatusOK, gin.H{"ok": true})
}

// cleanFinished DELETE /api/tasks
func (h *cutTaskHandler) cleanFinished(c *gin.Context) {
	n := h.svc.Tasks.CleanFinished()
	c.JSON(http.StatusOK, gin.H{"ok": true, "removed": n})
}

// download GET /api/tasks/:id/download
func (h *cutTaskHandler) download(c *gin.Context) {
	t, ok := h.svc.Tasks.Get(c.Param("id"))
	if !ok {
		c.JSON(http.StatusNotFound, gin.H{"error": "任务不存在"})
		return
	}
	if t.Status != service.TaskDone {
		c.JSON(http.StatusBadRequest, gin.H{"error": "任务尚未完成，无法下载"})
		return
	}
	if t.Output == "" {
		c.JSON(http.StatusNotFound, gin.H{"error": "输出文件不存在"})
		return
	}
	name := t.OutputName
	if name == "" {
		name = filepath.Base(t.Output)
	}
	// 正确的视频 MIME：以 application/octet-stream 提供会让 Chrome 视为
	// “可疑二进制”并在 HTTP（非 HTTPS）页面上拦截下载。视频类型则放行。
	if ct := mimeTypeForExt(filepath.Ext(t.Output)); ct != "" {
		c.Header("Content-Type", ct)
	} else {
		c.Header("Content-Type", "application/octet-stream")
	}
	// RFC 5987 编码中文文件名；纯 ASCII 名同时附带 filename= 兜底
	encoded := "filename*=UTF-8''" + url.PathEscape(name)
	cd := "attachment; " + encoded
	if isASCII(name) {
		cd = `attachment; filename="` + name + `"; ` + encoded
	}
	c.Header("Content-Disposition", cd)
	http.ServeFile(c.Writer, c.Request, t.Output)
}

// isASCII 判断字符串是否全部为 ASCII 字符。
func isASCII(s string) bool {
	for i := 0; i < len(s); i++ {
		if s[i] >= 0x80 {
			return false
		}
	}
	return true
}
