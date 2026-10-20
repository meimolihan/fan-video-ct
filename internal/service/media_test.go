package service

import (
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/meimolihan/fan-video-ct/internal/config"
)

// newDeleteService 构造仅用于删除路径校验的 MediaService（不触碰 ffmpeg / 日志）。
// 返回服务与四个允许删除的根目录。
func newDeleteService(t *testing.T) (*MediaService, map[string]string) {
	t.Helper()
	data := t.TempDir()
	roots := map[string]string{
		"data":   data,
		"media":  filepath.Join(data, "media"),
		"upload": filepath.Join(data, "uploads"),
		"cover":  filepath.Join(data, "covers"),
		"output": filepath.Join(data, "output"),
	}
	for _, d := range []string{roots["media"], roots["upload"], roots["cover"], roots["output"]} {
		if err := os.MkdirAll(d, 0o755); err != nil {
			t.Fatal(err)
		}
	}
	cfg := &config.Config{
		App: config.AppConfig{
			DataDir:   roots["data"],
			MediaDir:  roots["media"],
			UploadDir: "uploads",
			OutputDir: "output",
		},
	}
	return NewMediaService(cfg, nil, nil), roots
}

func write(t *testing.T, path string) string {
	t.Helper()
	if err := os.MkdirAll(filepath.Dir(path), 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(path, []byte("x"), 0o644); err != nil {
		t.Fatal(err)
	}
	return path
}

func TestRemoveDeletesInsideAllowedRoots(t *testing.T) {
	svc, roots := newDeleteService(t)
	cases := []struct {
		name string
		file string
	}{
		{"媒体目录内视频", filepath.Join(roots["media"], "a.mp4")},
		{"媒体子目录内视频", filepath.Join(roots["media"], "sub", "b.mkv")},
		{"上传目录内文件", filepath.Join(roots["upload"], "c.mp4")},
		{"封面目录内文件", filepath.Join(roots["cover"], "d.jpg")},
		{"输出目录内文件", filepath.Join(roots["output"], "e.mp4")},
	}
	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			if err := os.MkdirAll(filepath.Dir(c.file), 0o755); err != nil {
				t.Fatal(err)
			}
			write(t, c.file)
			got, err := svc.Remove(c.file)
			if err != nil {
				t.Fatalf("Remove(%s) 报错: %v", c.file, err)
			}
			if got != filepath.Clean(c.file) {
				t.Errorf("返回路径 = %q, want %q", got, filepath.Clean(c.file))
			}
			if _, err := os.Stat(c.file); !os.IsNotExist(err) {
				t.Errorf("文件仍存在: %s", c.file)
			}
		})
	}
}

func TestRemoveRejectsOutsideAllowedRoots(t *testing.T) {
	svc, roots := newDeleteService(t)
	// 应用自有目录之外的目标
	outside := filepath.Join(t.TempDir(), "precious.txt")
	write(t, outside)

	cases := []struct {
		name string
		path string
	}{
		{"允许根之外的文件", outside},
		{"媒体根目录本身", roots["media"]},
		{"上传根目录本身", roots["upload"]},
		{"数据目录根本身（含 config/预设/日志）", roots["data"]},
		{"文件系统根", "/"},
		{"空路径", ""},
		{"仅空白", "   "},
		// 前缀相近但不等于根目录：/media-evil 不能命中 /media
		{"前缀相近的兄弟目录", roots["media"] + "-evil/x.mp4"},
	}
	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			if _, err := svc.Remove(c.path); err == nil {
				t.Fatalf("Remove(%q) 应当被拒绝", c.path)
			} else if c.path != "" && strings.TrimSpace(c.path) != "" {
				// 确认目标未被删除
				if _, err := os.Stat(outside); err != nil && !os.IsNotExist(err) {
					t.Fatal(err)
				}
			}
		})
	}
	if _, err := os.Stat(outside); err != nil {
		t.Fatalf("拒绝路径时不应删除目标文件: %v", err)
	}
}

func TestRemoveBlocksSymlinkEscape(t *testing.T) {
	svc, roots := newDeleteService(t)
	// 根目录之外的真实目标
	outside := filepath.Join(t.TempDir(), "secret.txt")
	write(t, outside)
	// 媒体目录内指向根目录之外的软链
	link := filepath.Join(roots["media"], "sneaky.mp4")
	if err := os.Symlink(outside, link); err != nil {
		t.Skipf("当前环境不支持符号链接: %v", err)
	}

	if _, err := svc.Remove(link); err == nil {
		t.Fatal("经符号链接跳出允许目录的删除应当被拒绝")
	}
	if _, err := os.Stat(outside); err != nil {
		t.Fatalf("软链目标文件不应被删除: %v", err)
	}
	// 软链本身也未被移除（拒绝即整体拒绝，交由用户处理）
	if _, err := os.Lstat(link); err != nil {
		t.Fatalf("拒绝时不应移除软链本身: %v", err)
	}
}

func TestRemoveBlocksPathTraversal(t *testing.T) {
	svc, roots := newDeleteService(t)
	outside := filepath.Join(t.TempDir(), "victim.txt")
	write(t, outside)
	// 从媒体目录经 ../ 逃逸到允许根之外
	rel, err := filepath.Rel(roots["media"], outside)
	if err != nil {
		t.Fatal(err)
	}
	traversal := filepath.Join(roots["media"], rel)
	if _, err := svc.Remove(traversal); err == nil {
		t.Fatalf("路径穿越逃逸 (%s) 应当被拒绝", traversal)
	}
	if _, err := os.Stat(outside); err != nil {
		t.Fatalf("穿越路径不应删除目标: %v", err)
	}
}

func TestRemoveDirectoryRecursive(t *testing.T) {
	svc, roots := newDeleteService(t)
	dir := filepath.Join(roots["output"], "batch")
	nested := filepath.Join(dir, "deep", "clip.mp4")
	if err := os.MkdirAll(filepath.Dir(nested), 0o755); err != nil {
		t.Fatal(err)
	}
	write(t, nested)

	if _, err := svc.Remove(dir); err != nil {
		t.Fatalf("递归删除目录失败: %v", err)
	}
	if _, err := os.Stat(dir); !os.IsNotExist(err) {
		t.Errorf("目录仍存在: %s", dir)
	}
}

func TestRemoveNotExistInsideRoot(t *testing.T) {
	svc, roots := newDeleteService(t)
	_, err := svc.Remove(filepath.Join(roots["media"], "ghost.mp4"))
	if err == nil {
		t.Fatal("不存在的目标应当报错")
	}
	if !strings.Contains(err.Error(), "不存在") {
		t.Errorf("错误信息应说明目标不存在, got: %v", err)
	}
}

func TestRemoveSymlinkInsideRootIsSafe(t *testing.T) {
	svc, roots := newDeleteService(t)
	real := write(t, filepath.Join(roots["media"], "real.mp4"))
	link := filepath.Join(roots["media"], "alias.mp4")
	if err := os.Symlink(real, link); err != nil {
		t.Skipf("当前环境不支持符号链接: %v", err)
	}

	// 软链与目标同在允许根内：删除软链本身，不动目标文件
	if _, err := svc.Remove(link); err != nil {
		t.Fatalf("删除根目录内软链失败: %v", err)
	}
	if _, err := os.Lstat(link); !os.IsNotExist(err) {
		t.Errorf("软链本身应被移除")
	}
	if _, err := os.Stat(real); err != nil {
		t.Errorf("软链目标文件不应被删除: %v", err)
	}
}

func TestIsDeletable(t *testing.T) {
	svc, roots := newDeleteService(t)
	inside := write(t, filepath.Join(roots["media"], "ok.mp4"))
	outside := write(t, filepath.Join(t.TempDir(), "no.mp4"))

	if !svc.IsDeletable(inside) {
		t.Errorf("允许根内文件应可删除: %s", inside)
	}
	if svc.IsDeletable(outside) {
		t.Errorf("允许根外文件不应可删除: %s", outside)
	}
	if svc.IsDeletable(roots["media"]) {
		t.Errorf("根目录本身不应可删除")
	}
	if svc.IsDeletable("") {
		t.Errorf("空路径不应可删除")
	}
}

func TestListDirMarksDeletable(t *testing.T) {
	svc, roots := newDeleteService(t)
	write(t, filepath.Join(roots["media"], "ok.mp4"))
	write(t, filepath.Join(roots["media"], "sub", "nested.mp4"))

	list, err := svc.ListDir("")
	if err != nil {
		t.Fatal(err)
	}
	if len(list) == 0 {
		t.Fatal("列表为空")
	}
	for _, fe := range list {
		if !fe.Deletable {
			t.Errorf("媒体目录内条目 %q 应标记为可删除", fe.Path)
		}
	}

	// 列出媒体目录之外的目录：条目不应标记为可删除
	outside := t.TempDir()
	write(t, filepath.Join(outside, "keep.mp4"))
	far, err := svc.ListDir(outside)
	if err != nil {
		t.Fatal(err)
	}
	for _, fe := range far {
		if fe.Deletable {
			t.Errorf("允许根外条目 %q 不应标记为可删除", fe.Path)
		}
	}
}
