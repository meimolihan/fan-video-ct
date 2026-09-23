package handler

import (
	"context"
	"net/http"
	"os"
	"strconv"
	"strings"
	"time"

	"github.com/gin-gonic/gin"
	"go.uber.org/zap"

	"github.com/meimolihan/fan-video-ct/internal/service"
)

type coverHandler struct {
	svc *service.Service
	log *zap.SugaredLogger
}

// pathFromQuery 统一从 query 中提取视频路径并做基础校验。
func (h *coverHandler) pathFromQuery(c *gin.Context) (string, bool) {
	p := strings.TrimSpace(c.Query("path"))
	if p == "" {
		c.JSON(http.StatusBadRequest, gin.H{"error": "缺少 path 参数"})
		return "", false
	}
	if _, err := os.Stat(p); err != nil {
		c.JSON(http.StatusBadRequest, gin.H{"error": "视频文件不存在: " + p})
		return "", false
	}
	return p, true
}

// tFromQuery 提取 "t"（秒）参数并校验非负。
func (h *coverHandler) tFromQuery(c *gin.Context) (float64, bool) {
	raw := strings.TrimSpace(c.Query("t"))
	if raw == "" {
		c.JSON(http.StatusBadRequest, gin.H{"error": "缺少 t（时间秒）参数"})
		return 0, false
	}
	t, err := strconv.ParseFloat(raw, 64)
	if err != nil || t < 0 {
		c.JSON(http.StatusBadRequest, gin.H{"error": "t 参数必须是 >=0 的数字"})
		return 0, false
	}
	return t, true
}

// preview GET /api/cover/preview?path=&t= 返回指定时间的视频帧预览（不保存）。
// t 缺省为 0，即默认显示首帧。
func (h *coverHandler) preview(c *gin.Context) {
	p, ok := h.pathFromQuery(c)
	if !ok {
		return
	}
	t := 0.0
	if raw := strings.TrimSpace(c.Query("t")); raw != "" {
		tv, err := strconv.ParseFloat(raw, 64)
		if err != nil || tv < 0 {
			c.JSON(http.StatusBadRequest, gin.H{"error": "t 参数必须是 >=0 的数字"})
			return
		}
		t = tv
	}
	ctx, cancel := context.WithTimeout(c.Request.Context(), 60*time.Second)
	defer cancel()
	data, contentType, err := h.svc.Covers.PreviewFrame(ctx, p, t)
	if err != nil {
		c.JSON(http.StatusInternalServerError, gin.H{"error": "截取帧预览失败: " + err.Error()})
		return
	}
	c.Header("Cache-Control", "public, max-age=3600")
	c.Data(http.StatusOK, contentType, data)
}

// get GET /api/cover?path= 返回当前封面图片（不存在时 404）。
// 优先返回自定义封面；视频自带内嵌封面（attached_pic）时返回该封面。
func (h *coverHandler) get(c *gin.Context) {
	p, ok := h.pathFromQuery(c)
	if !ok {
		return
	}
	ctx, cancel := context.WithTimeout(c.Request.Context(), 60*time.Second)
	defer cancel()
	data, contentType, err := h.svc.Covers.CoverData(ctx, p)
	if err != nil {
		if h.svc.Covers.CoverPath(p) == "" {
			c.JSON(http.StatusNotFound, gin.H{"error": "该视频暂无封面", "hasCover": false})
			return
		}
		c.JSON(http.StatusInternalServerError, gin.H{"error": "读取封面失败: " + err.Error()})
		return
	}
	c.Header("Cache-Control", "public, max-age=3600")
	c.Data(http.StatusOK, contentType, data)
}

// capture POST /api/cover?path=&t= 截取指定时间帧，保存并替换封面。
func (h *coverHandler) capture(c *gin.Context) {
	p, ok := h.pathFromQuery(c)
	if !ok {
		return
	}
	t, ok := h.tFromQuery(c)
	if !ok {
		return
	}
	ctx, cancel := context.WithTimeout(c.Request.Context(), 60*time.Second)
	defer cancel()
	dst, err := h.svc.Covers.SetFromFrame(ctx, p, t)
	if err != nil {
		c.JSON(http.StatusBadRequest, gin.H{"error": err.Error()})
		return
	}
	h.log.Infof("已截取帧作为封面: %s (t=%.3fs)", p, t)
	c.JSON(http.StatusOK, gin.H{"ok": true, "cover": dst})
}

// upload POST /api/cover/upload?path=  multipart 字段 file 上传自定义封面。
func (h *coverHandler) upload(c *gin.Context) {
	p, ok := h.pathFromQuery(c)
	if !ok {
		return
	}
	file, header, err := c.Request.FormFile("file")
	if err != nil {
		c.JSON(http.StatusBadRequest, gin.H{"error": "缺少 file 上传字段"})
		return
	}
	defer file.Close()

	// 32MB 上限由 service.SetFromUpload 内部的 LimitReader 保证
	// （multipart 层 librarary 不限制，这里显式拒绝超大文件）
	dst, err := h.svc.Covers.SetFromUpload(p, file, header.Header.Get("Content-Type"))
	if err != nil {
		c.JSON(http.StatusBadRequest, gin.H{"error": err.Error()})
		return
	}
	h.log.Infof("已上传自定义封面: %s (%s)", p, header.Filename)
	c.JSON(http.StatusOK, gin.H{"ok": true, "cover": dst})
}

// delete DELETE /api/cover?path= 移除已设置的封面。
func (h *coverHandler) delete(c *gin.Context) {
	p, ok := h.pathFromQuery(c)
	if !ok {
		return
	}
	if err := h.svc.Covers.Remove(p); err != nil {
		c.JSON(http.StatusInternalServerError, gin.H{"error": "移除封面失败: " + err.Error()})
		return
	}
	h.log.Infof("已移除封面: %s", p)
	c.JSON(http.StatusOK, gin.H{"ok": true, "hasCover": false})
}
