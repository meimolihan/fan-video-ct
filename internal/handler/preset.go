// 片头片尾预设 HTTP 接口：查询 / 新增或编辑 / 删除。
package handler

import (
	"errors"
	"net/http"
	"net/url"
	"strings"

	"github.com/gin-gonic/gin"
	"go.uber.org/zap"

	"github.com/meimolihan/fan-video-ct/internal/service"
)

type presetHandler struct {
	svc *service.Service
	log *zap.SugaredLogger
}

// list GET /api/presets 获取全部预设列表。
func (h *presetHandler) list(c *gin.Context) {
	c.JSON(http.StatusOK, gin.H{"presets": h.svc.Presets.List()})
}

// save POST /api/presets 新增（名称不存在）或更新（名称已存在）一条预设。
func (h *presetHandler) save(c *gin.Context) {
	var req service.Preset
	if err := c.ShouldBindJSON(&req); err != nil {
		c.JSON(http.StatusBadRequest, gin.H{"error": "请求参数无效: " + err.Error()})
		return
	}
	presets, err := h.svc.Presets.Save(req)
	if err != nil {
		c.JSON(http.StatusBadRequest, gin.H{"error": err.Error()})
		return
	}
	h.log.Infof("已保存片头片尾预设「%s」（片头 %.0fs / 片尾 %.0fs）", req.Name, req.Head, req.Tail)
	c.JSON(http.StatusOK, gin.H{"ok": true, "presets": presets})
}

// delete DELETE /api/presets/:name 按预设名称删除指定预设。
func (h *presetHandler) delete(c *gin.Context) {
	name := strings.TrimSpace(c.Param("name"))
	if decoded, err := url.PathUnescape(name); err == nil {
		name = strings.TrimSpace(decoded)
	}
	if name == "" {
		c.JSON(http.StatusBadRequest, gin.H{"error": "缺少预设名称"})
		return
	}
	presets, err := h.svc.Presets.Delete(name)
	if err != nil {
		if errors.Is(err, service.ErrPresetNotFound) {
			c.JSON(http.StatusNotFound, gin.H{"error": "预设不存在: " + name})
			return
		}
		c.JSON(http.StatusInternalServerError, gin.H{"error": "删除预设失败: " + err.Error()})
		return
	}
	h.log.Infof("已删除片头片尾预设「%s」", name)
	c.JSON(http.StatusOK, gin.H{"ok": true, "presets": presets})
}