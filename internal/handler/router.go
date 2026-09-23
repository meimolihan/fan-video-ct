// Package handler 提供 HTTP API 与前端静态资源路由（基于 gin）。
package handler

import (
	"net/http"
	"path"
	"strings"

	"github.com/gin-gonic/gin"
	"go.uber.org/zap"

	"github.com/meimolihan/fan-video-ct/internal/config"
	"github.com/meimolihan/fan-video-ct/internal/embedded"
	"github.com/meimolihan/fan-video-ct/internal/service"
	"github.com/meimolihan/fan-video-ct/internal/version"
)

// Handler 聚合 HTTP 处理器。
type Handler struct {
	cfg     *config.Config
	svc     *service.Service
	log     *zap.SugaredLogger
	webRoot http.FileSystem

	media *mediaHandler
	cut   *cutTaskHandler
	cover *coverHandler
	preset *presetHandler
}

// New 构建 Handler。
func New(cfg *config.Config, svc *service.Service, log *zap.SugaredLogger) *Handler {
	return &Handler{
		cfg:     cfg,
		svc:     svc,
		log:     log,
		webRoot: embedded.Resolve(cfg.App.WebDir),
		media:   &mediaHandler{svc: svc, log: log},
		cut:     &cutTaskHandler{svc: svc, log: log},
		cover:   &coverHandler{svc: svc, log: log},
		preset:  &presetHandler{svc: svc, log: log},
	}
}

// Router 构建并返回 gin 路由。
func (h *Handler) Router() *gin.Engine {
	if !h.cfg.App.Debug {
		gin.SetMode(gin.ReleaseMode)
	}
	r := gin.New()
	r.Use(gin.Recovery())

	api := r.Group("/api")
	{
		api.GET("/health", func(c *gin.Context) {
			c.JSON(http.StatusOK, gin.H{"status": "ok"})
		})
		api.GET("/version", func(c *gin.Context) {
			c.JSON(http.StatusOK, gin.H{
				"app":     "fan-video-ct",
				"name":    "视频剪切工具",
				"version": version.Current(),
			})
		})
		api.GET("/media/dir", h.media.listDir)
		api.GET("/media/info", h.media.info)
		api.GET("/media/stream", h.media.stream)
		api.GET("/media/keyframes", h.media.keyframes)

		api.GET("/tasks", h.cut.list)
		api.POST("/tasks", h.cut.create)
		api.DELETE("/tasks", h.cut.cleanFinished)
		api.GET("/tasks/:id", h.cut.get)
		api.DELETE("/tasks/:id", h.cut.cancel)
		api.GET("/tasks/:id/download", h.cut.download)

		api.GET("/cover", h.cover.get)
		api.GET("/cover/preview", h.cover.preview)
		api.POST("/cover", h.cover.capture)
		api.POST("/cover/upload", h.cover.upload)
		api.DELETE("/cover", h.cover.delete)

		api.GET("/presets", h.preset.list)
		api.POST("/presets", h.preset.save)
		api.DELETE("/presets/:name", h.preset.delete)
	}

	h.serveStatic(r)
	return r
}

// serveStatic 提供前端静态资源：/ 与 asset 路径从 webRoot（磁盘或内嵌）读取，
// 未匹配到文件的路径回退到 index.html（服务端渲染单页应用友好）。
func (h *Handler) serveStatic(r *gin.Engine) {
	routes := []string{"/favicon.svg", "/css/", "/js/", "/assets/"}
	r.NoRoute(func(c *gin.Context) {
		p := c.Request.URL.Path
		clean := path.Clean("/" + p)
		if !strings.HasPrefix(clean, "/api/") {
			for _, prefix := range routes {
				if strings.HasPrefix(clean, prefix) {
					h.serveFile(c, strings.TrimPrefix(clean, "/"), "public, max-age=31536000, immutable")
					return
				}
			}
			h.serveFile(c, "index.html", "no-cache")
			return
		}
		c.JSON(http.StatusNotFound, gin.H{"error": "接口不存在"})
	})
}

// serveFile 从 webRoot 提供单个文件（支持缓存头）。
func (h *Handler) serveFile(c *gin.Context, name, cacheControl string) {
	f, err := h.webRoot.Open(path.Clean("/" + name))
	if err != nil {
		c.Status(http.StatusNotFound)
		return
	}
	defer f.Close()
	info, err := f.Stat()
	if err != nil || info.IsDir() {
		c.Status(http.StatusNotFound)
		return
	}
	if cacheControl != "" {
		c.Header("Cache-Control", cacheControl)
	}
	http.ServeContent(c.Writer, c.Request, info.Name(), info.ModTime(), f)
}
