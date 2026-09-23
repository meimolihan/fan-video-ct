// 片头片尾预设：服务端持久化（presets.json），读写带并发锁，防并发写坏 JSON。
package service

import (
	"encoding/json"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"sync"
)

// Preset 片头片尾预设条目。
type Preset struct {
	Name string  `json:"name"` // 预设名称（唯一标识）
	Head float64 `json:"head"` // 片头跳过时长（秒）
	Tail float64 `json:"tail"` // 片尾跳过时长（秒）
}

// 内置默认预设（程序首次启动、文件缺失或损坏时自动加载）。
var defaultPresets = []Preset{
	{Name: "电视剧通用", Head: 90, Tail: 60},
	{Name: "短剧", Head: 15, Tail: 20},
	{Name: "动漫", Head: 85, Tail: 75},
}

// ErrPresetNotFound 预设不存在。
var ErrPresetNotFound = errors.New("预设不存在")

// PresetManager 预设持久化管理：内存副本 + 读写锁 + 原子写盘。
type PresetManager struct {
	mu      sync.RWMutex
	path    string
	presets []Preset
}

// NewPresetManager 读取 presets.json；文件不存在时自动生成内置默认预设并落盘。
// 文件内容损坏时先备份为 presets.json.bak，再重建默认预设，避免工具无法启动。
func NewPresetManager(dir string) (*PresetManager, error) {
	m := &PresetManager{path: filepath.Join(dir, "presets.json")}
	raw, err := os.ReadFile(m.path)
	if err == nil {
		var list []Preset
		if jerr := json.Unmarshal(raw, &list); jerr == nil {
			m.presets = list
			return m, nil
		}
		_ = os.WriteFile(m.path+".bak", raw, 0644)
	} else if !os.IsNotExist(err) {
		return nil, fmt.Errorf("读取预设文件失败: %w", err)
	}
	m.presets = append([]Preset(nil), defaultPresets...)
	if err := m.writeLocked(m.presets); err != nil {
		return nil, err
	}
	return m, nil
}

// writeLocked 以「临时文件 + 原子重命名」写盘，避免写一半崩溃留下损坏 JSON。
func (m *PresetManager) writeLocked(list []Preset) error {
	data, err := json.MarshalIndent(list, "", "  ")
	if err != nil {
		return err
	}
	if err := os.MkdirAll(filepath.Dir(m.path), 0755); err != nil {
		return err
	}
	tmp := m.path + ".tmp"
	if err := os.WriteFile(tmp, data, 0644); err != nil {
		return err
	}
	return os.Rename(tmp, m.path)
}

// listLocked 返回当前预设列表的副本（调用方需已持有锁）。
func (m *PresetManager) listLocked() []Preset {
	out := make([]Preset, len(m.presets))
	copy(out, m.presets)
	return out
}

// List 返回全部预设列表副本。
func (m *PresetManager) List() []Preset {
	m.mu.RLock()
	defer m.mu.RUnlock()
	return m.listLocked()
}

// Save 新增或更新（按名称唯一匹配）一条预设，成功返回写盘后的完整列表。
func (m *PresetManager) Save(p Preset) ([]Preset, error) {
	name := strings.TrimSpace(p.Name)
	if name == "" {
		return nil, errors.New("预设名称不能为空")
	}
	if p.Head < 0 || p.Tail < 0 {
		return nil, errors.New("片头/片尾时长不能为负数")
	}
	p.Name = name

	m.mu.Lock()
	defer m.mu.Unlock()
	next := m.listLocked()
	idx := -1
	for i := range next {
		if next[i].Name == name {
			idx = i
			break
		}
	}
	if idx >= 0 {
		next[idx] = p
	} else {
		next = append(next, p)
	}
	if err := m.writeLocked(next); err != nil {
		return nil, err
	}
	m.presets = next
	return m.listLocked(), nil
}

// Delete 按名称删除一条预设，成功返回写盘后的完整列表。
func (m *PresetManager) Delete(name string) ([]Preset, error) {
	name = strings.TrimSpace(name)
	m.mu.Lock()
	defer m.mu.Unlock()
	next := make([]Preset, 0, len(m.presets))
	found := false
	for _, p := range m.presets {
		if p.Name == name {
			found = true
			continue
		}
		next = append(next, p)
	}
	if !found {
		return nil, ErrPresetNotFound
	}
	if err := m.writeLocked(next); err != nil {
		return nil, err
	}
	m.presets = next
	return m.listLocked(), nil
}