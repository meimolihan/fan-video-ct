// Package service 承载业务逻辑：媒体浏览、封面管理与剪切任务调度。
package service

import (
	"github.com/meimolihan/fan-video-ct/internal/config"
	"github.com/meimolihan/fan-video-ct/internal/ffmpeg"
	"go.uber.org/zap"
)

// Service 聚合全部业务能力。
type Service struct {
	Cfg     *config.Config
	FFmpeg  *ffmpeg.Client
	Log     *zap.SugaredLogger
	Media   *MediaService
	Covers  *CoverService
	Tasks   *TaskManager
	Presets *PresetManager
}

// New 构建 Service。
func New(cfg *config.Config, ffc *ffmpeg.Client, log *zap.SugaredLogger) *Service {
	s := &Service{
		Cfg:    cfg,
		FFmpeg: ffc,
		Log:    log,
	}
	s.Media = NewMediaService(cfg, ffc, log)
	s.Covers = NewCoverService(ffc, cfg.CoversDir(), log)
	s.Tasks = NewTaskManager(cfg, ffc, log)
	pm, err := NewPresetManager(cfg.App.DataDir)
	if err != nil {
		log.Errorf("初始化片头片尾预设失败（已用内置默认值兜底）: %v", err)
		pm = &PresetManager{presets: append([]Preset(nil), defaultPresets...)}
	}
	s.Presets = pm
	return s
}
