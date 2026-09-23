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

// Task 剪切任务。
type Task struct {
	ID         string    `json:"id"`
	Path       string    `json:"path"`
	Output     string    `json:"output"`
	OutputName string    `json:"output_name"`
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
	name = uniqueName(m.cfg.OutputDir(), name)
	outPath := filepath.Join(m.cfg.OutputDir(), name)

	task := &Task{
		ID:         uuid.NewString(),
		Path:       meta.Path,
		Output:     outPath,
		OutputName: name,
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
