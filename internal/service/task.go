package service

import (
	"context"
	"fmt"
	"os"
	"path/filepath"
	"regexp"
	"sort"
	"strings"
	"sync"
	"time"

	"github.com/google/uuid"
	"github.com/meimolihan/fan-video-ct/internal/config"
	"github.com/meimolihan/fan-video-ct/internal/ffmpeg"
	"go.uber.org/zap"
)

// 任务状态
const (
	TaskQueued    = "queued"
	TaskRunning   = "running"
	TaskDone      = "done"
	TaskFailed    = "failed"
	TaskCancelled = "cancelled"
)

// 剪切结果的保存目标
const (
	// SaveTargetDefault 保存到软件数据目录（app.output_dir 解析结果，默认值）
	SaveTargetDefault = "default"
	// SaveTargetSource 保存到原视频所在目录（仅修改文件名，不改变目录）
	SaveTargetSource = "source"
)

// Task 剪切任务。
type Task struct {
	ID         string    `json:"id"`
	Path       string    `json:"path"`
	Output     string    `json:"output"`
	OutputName string    `json:"output_name"`
	SaveTarget string    `json:"save_target"`
	Mode       string    `json:"mode"`
	Start      float64   `json:"start"`
	End        float64   `json:"end"`
	Status     string    `json:"status"`
	Progress   float64   `json:"progress"`
	Error      string    `json:"error,omitempty"`
	OutputSize int64     `json:"output_size"`
	CreatedAt  time.Time `json:"created_at"`
	UpdatedAt  time.Time `json:"updated_at"`

	cancel context.CancelFunc
	mu     sync.RWMutex
}

// setLocked 仅在工作区文件内部小范围更新状态。
func (t *Task) update(status string, progress float64, errMsg string) {
	t.Status = status
	t.Progress = progress
	t.Error = errMsg
	t.UpdatedAt = time.Now()
}

// TaskManager 内存任务调度器：并发上限受 app.worker 限制，
// 超出部分进入等待队列按创建顺序执行。
type TaskManager struct {
	cfg *config.Config
	ffc *ffmpeg.Client
	log *zap.SugaredLogger

	mu     sync.RWMutex
	tasks  map[string]*Task
	queue  []*Task
	active int
}

// NewTaskManager 创建任务管理器。
func NewTaskManager(cfg *config.Config, ffc *ffmpeg.Client, log *zap.SugaredLogger) *TaskManager {
	return &TaskManager{
		cfg:   cfg,
		ffc:   ffc,
		log:   log,
		tasks: make(map[string]*Task),
	}
}

// TaskMeta 创建任务时的入参。
type TaskMeta struct {
	Path       string
	Mode       string
	Start      float64
	End        float64
	OutputName string
	// SaveTarget 保存目标（SaveTargetDefault / SaveTargetSource），留空按默认处理
	SaveTarget string
}

// OutputTarget 描述一个可选的输出保存位置（供前端弹窗展示绝对路径）。
type OutputTarget struct {
	// ID 保存目标标识（default / source）
	ID string `json:"id"`
	// Label 前端展示名
	Label string `json:"label"`
	// Dir 目标目录绝对路径
	Dir string `json:"dir"`
	// File 按当前文件名推算出的输出文件绝对路径（不可用时为空）
	File string `json:"file,omitempty"`
	// Available 目录是否存在且可写
	Available bool `json:"available"`
	// Reason 不可用原因（Available 为 false 时给出）
	Reason string `json:"reason,omitempty"`
}

// Create 创建并调度剪切任务。
func (m *TaskManager) Create(ctx context.Context, meta TaskMeta) (*Task, error) {
	if strings.TrimSpace(meta.Path) == "" {
		return nil, fmt.Errorf("缺少视频路径")
	}
	if !ffmpeg.ValidMode(meta.Mode) {
		return nil, fmt.Errorf("不支持的剪切模式: %s（仅支持 copy / reencode）", meta.Mode)
	}
	info, err := m.ffc.Probe(ctx, meta.Path)
	if err != nil {
		return nil, fmt.Errorf("探测视频失败: %w", err)
	}
	if err := ffmpeg.ValidateTimeRange(meta.Start, meta.End, info.Duration); err != nil {
		return nil, err
	}

	ext := ffmpeg.OutputExtension(meta.Path, ffmpeg.CutMode(meta.Mode))
	name := sanitizeOutputName(meta.OutputName, meta.Path)
	if !strings.HasSuffix(strings.ToLower(name), ext) {
		name += ext
	}
	outDir, err := m.resolveOutputDir(meta.SaveTarget, meta.Path)
	if err != nil {
		return nil, err
	}
	name = uniqueName(outDir, name)
	outPath := filepath.Join(outDir, name)

	task := &Task{
		ID:         uuid.NewString(),
		Path:       meta.Path,
		Output:     outPath,
		OutputName: name,
		SaveTarget: normalizeSaveTarget(meta.SaveTarget),
		Mode:       meta.Mode,
		Start:      meta.Start,
		End:        meta.End,
		Status:     TaskQueued,
		CreatedAt:  time.Now(),
		UpdatedAt:  time.Now(),
	}

	m.mu.Lock()
	m.tasks[task.ID] = task
	worker := m.cfg.App.Worker
	if worker < 1 {
		worker = 1
	}
	if m.active < worker {
		m.active++
		m.mu.Unlock()
		m.run(task)
	} else {
		m.queue = append(m.queue, task)
		m.mu.Unlock()
		m.log.Infof("任务 %s 已排队（当前并发已达上限）", task.ID)
	}
	return task, nil
}

// run 执行任务；结束后自动调度下一个排队任务。
func (m *TaskManager) run(task *Task) {
	ctx, cancel := context.WithCancel(context.Background())
	task.cancel = cancel
	go func() {
		defer func() {
			m.mu.Lock()
			m.active--
			var next *Task
			if len(m.queue) > 0 {
				next = m.queue[0]
				m.queue = m.queue[1:]
				m.active++
			}
			m.mu.Unlock()
			if next != nil {
				m.run(next)
			}
		}()

		start := task.Start
		end := task.End
		m.mu.Lock()
		task.update(TaskRunning, 0, "")
		m.mu.Unlock()
		m.log.Infof("剪切任务 %s 开始（%s mode=%s %.3fs-%.3fs）", task.ID, task.Path, task.Mode, start, end)

		err := m.ffc.RunCut(ctx, task.Path, task.Output, start, end-start, ffmpeg.CutMode(task.Mode), func(p float64) {
			task.mu.Lock()
			task.Progress = p
			task.UpdatedAt = time.Now()
			task.mu.Unlock()
		})

		m.mu.Lock()
		defer m.mu.Unlock()
		switch {
		case ctx.Err() != nil:
			task.update(TaskCancelled, task.Progress, "任务已取消")
			m.log.Infof("剪切任务 %s 已取消", task.ID)
		case err != nil:
			task.update(TaskFailed, task.Progress, err.Error())
			m.log.Errorf("剪切任务 %s 失败: %v", task.ID, err)
		default:
			task.update(TaskDone, 1, "")
			if st, err := os.Stat(task.Output); err == nil {
				task.OutputSize = st.Size()
			}
			m.log.Infof("剪切任务 %s 完成，输出 %s（%.0f MB）", task.ID, task.Output, float64(task.OutputSize)/1024/1024)
		}
	}()
}

// List 返回全部任务，按创建时间倒序。
func (m *TaskManager) List() []*Task {
	m.mu.RLock()
	defer m.mu.RUnlock()
	list := make([]*Task, 0, len(m.tasks))
	for _, t := range m.tasks {
		list = append(list, t)
	}
	sort.Slice(list, func(i, j int) bool {
		return list[i].CreatedAt.After(list[j].CreatedAt)
	})
	return list
}

// Get 按 ID 获取任务。
func (m *TaskManager) Get(id string) (*Task, bool) {
	m.mu.RLock()
	defer m.mu.RUnlock()
	t, ok := m.tasks[id]
	return t, ok
}

// Cancel 取消任务。
func (m *TaskManager) Cancel(id string) error {
	m.mu.RLock()
	t, ok := m.tasks[id]
	m.mu.RUnlock()
	if !ok {
		return fmt.Errorf("任务不存在: %s", id)
	}
	if t.cancel != nil {
		t.cancel()
	}
	return nil
}

// Remove 移除已完成/已取消/失败的任务记录（不删除输出文件）。
func (m *TaskManager) Remove(id string) error {
	m.mu.Lock()
	defer m.mu.Unlock()
	t, ok := m.tasks[id]
	if !ok {
		return fmt.Errorf("任务不存在: %s", id)
	}
	switch t.Status {
	case TaskRunning, TaskQueued:
		return fmt.Errorf("任务正在执行，无法删除")
	}
	delete(m.tasks, id)
	return nil
}

// CleanFinished 清理所有终态任务记录。
func (m *TaskManager) CleanFinished() int {
	m.mu.Lock()
	defer m.mu.Unlock()
	n := 0
	for id, t := range m.tasks {
		switch t.Status {
		case TaskDone, TaskFailed, TaskCancelled:
			delete(m.tasks, id)
			n++
		}
	}
	return n
}

// ==================== 工具函数 ====================

// normalizeSaveTarget 归一化保存目标；空值或非法值一律回退到默认目录。
func normalizeSaveTarget(target string) string {
	if target == SaveTargetSource {
		return SaveTargetSource
	}
	return SaveTargetDefault
}

// sourceDirOf 返回原视频所在目录。要求源路径为绝对路径，
// 以保证弹窗展示与实际写入的目录恒为绝对路径（前端明确要求显示绝对路径）。
func sourceDirOf(srcPath string) (string, error) {
	clean := filepath.Clean(strings.TrimSpace(srcPath))
	if clean == "" || clean == "." {
		return "", fmt.Errorf("无法确定原视频所在目录")
	}
	if !filepath.IsAbs(clean) {
		return "", fmt.Errorf("原视频路径不是绝对路径")
	}
	return filepath.Dir(clean), nil
}

// resolveOutputDir 按保存目标解析输出目录。跟随原视频时取源文件所在目录，
// 目录不存在 / 非目录 / 不可写时返回可读错误，避免任务跑到一半才失败。
func (m *TaskManager) resolveOutputDir(target, srcPath string) (string, error) {
	if normalizeSaveTarget(target) != SaveTargetSource {
		return m.cfg.OutputDir(), nil
	}
	dir, err := sourceDirOf(srcPath)
	if err != nil {
		return "", fmt.Errorf("跟随原视频保存失败：%w", err)
	}
	if err := checkDirWritable(dir); err != nil {
		return "", fmt.Errorf("跟随原视频保存失败：%s（%s）", dir, err)
	}
	return dir, nil
}

// checkDirWritable 校验目录存在且可写：以临时文件探测并立即清理，
// 避免只凭权限位判断（ACL、只读挂载、网络盘等场景权限位不可靠）。
func checkDirWritable(dir string) error {
	st, err := os.Stat(dir)
	if err != nil {
		return fmt.Errorf("目录不可访问")
	}
	if !st.IsDir() {
		return fmt.Errorf("不是目录")
	}
	f, err := os.CreateTemp(dir, ".fan-video-ct-probe-*")
	if err != nil {
		return fmt.Errorf("目录不可写")
	}
	// 用 defer 保证任何返回路径都不会在用户目录里留下探针文件
	defer func() {
		_ = f.Close()
		_ = os.Remove(f.Name())
	}()
	return nil
}

// OutputTargets 返回给定源视频的全部可选保存位置，含绝对路径与可写性，
// 供前端“保存位置”弹窗展示；与 Create 共用同一套命名 / 目录解析逻辑。
func (m *TaskManager) OutputTargets(srcPath, outputName, mode string) []OutputTarget {
	ext := ffmpeg.OutputExtension(srcPath, ffmpeg.CutMode(mode))
	name := sanitizeOutputName(outputName, srcPath)
	if !strings.HasSuffix(strings.ToLower(name), ext) {
		name += ext
	}

	sourceDir, sourceErr := sourceDirOf(srcPath)
	defs := []struct {
		id, label string
		dir       string
		err       error
	}{
		{SaveTargetDefault, "软件数据目录", m.cfg.OutputDir(), nil},
		{SaveTargetSource, "跟随原视频保存", sourceDir, sourceErr},
	}

	targets := make([]OutputTarget, 0, len(defs))
	for _, d := range defs {
		t := OutputTarget{ID: d.id, Label: d.label, Dir: d.dir}
		// 目录无法定位时只给原因、不暴露任何路径；否则探测可写性并推算完整文件路径
		if d.err != nil {
			t.Reason = d.err.Error()
		} else if err := checkDirWritable(d.dir); err != nil {
			t.Reason = err.Error()
		} else {
			t.Available = true
			t.File = filepath.Join(d.dir, uniqueName(d.dir, name))
		}
		targets = append(targets, t)
	}
	return targets
}

var invalidNameChars = regexp.MustCompile(`[\\/:*?"<>|\r\n]`)

// sanitizeOutputName 清理用户提供的输出文件名；为空时基于输入文件名生成。
func sanitizeOutputName(name, inputPath string) string {
	name = strings.TrimSpace(name)
	if name == "" {
		base := filepath.Base(inputPath)
		if idx := strings.LastIndexByte(base, '.'); idx > 0 {
			base = base[:idx]
		}
		name = base + "_cut"
	}
	name = invalidNameChars.ReplaceAllString(name, "_")
	name = strings.TrimSpace(name)
	if name == "" || name == "." || name == ".." {
		name = "output"
	}
	return name
}

// uniqueName 若输出文件已存在则追加序号，避免覆盖。
func uniqueName(dir, name string) string {
	candidate := name
	i := 1
	for {
		full := filepath.Join(dir, candidate)
		if _, err := os.Stat(full); os.IsNotExist(err) {
			return candidate
		}
		ext := filepath.Ext(candidate)
		stem := strings.TrimSuffix(candidate, ext)
		candidate = fmt.Sprintf("%s_%d%s", stem, i, ext)
		i++
	}
}
