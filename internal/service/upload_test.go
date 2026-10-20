package service

import (
	"context"
	"errors"
	"io"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/meimolihan/fan-video-ct/internal/config"
)

func TestSanitizeUploadName(t *testing.T) {
	cases := []struct {
		in   string
		want string
	}{
		{"movie.mp4", "movie.mp4"},
		{"", "upload.mp4"},
		{"   ", "upload.mp4"},
		{"../../etc/passwd.mp4", "passwd.mp4"},     // 路径穿越
		{`C:\Videos\a<b>c:d?.mp4`, "a_b_c_d_.mp4"}, // Windows 盘符/反斜杠/非法字符
		{"..", "upload.mp4"},                       // 纯点号
		{"...", "upload.mp4"},
		{"tab\there.mp4", "tabhere.mp4"}, // 控制字符剔除
		{"UPPER.MKV", "UPPER.MKV"},       // 大小写保留（IsVideoFile 会归一化判断）
	}
	for _, c := range cases {
		got := sanitizeUploadName(c.in)
		if got != c.want {
			t.Errorf("sanitizeUploadName(%q) = %q, want %q", c.in, got, c.want)
		}
		if strings.ContainsAny(got, `/\`) || strings.Contains(got, "..") {
			t.Errorf("sanitizeUploadName(%q) = %q 仍含路径成分", c.in, got)
		}
	}
}

func TestSanitizeUploadNameLength(t *testing.T) {
	long := strings.Repeat("啊", 200) + ".mp4"
	got := sanitizeUploadName(long)
	if n := len([]rune(strings.TrimSuffix(got, ".mp4"))); n > 96 {
		t.Errorf("文件名主体过长: %d runes", n)
	}
	if filepath.Ext(got) != ".mp4" {
		t.Errorf("扩展名丢失: %q", got)
	}
}

func TestUniqueFilePath(t *testing.T) {
	dir := t.TempDir()
	first, renamed := uniqueFilePath(dir, "a.mp4")
	if renamed {
		t.Fatalf("首次应当不改名: %v", renamed)
	}
	if err := os.WriteFile(first, []byte("x"), 0644); err != nil {
		t.Fatal(err)
	}
	second, renamed := uniqueFilePath(dir, "a.mp4")
	if !renamed {
		t.Fatalf("已存在时应当改名")
	}
	if filepath.Base(second) != "a(1).mp4" {
		t.Fatalf("改名格式错误: %s", second)
	}
	if err := os.WriteFile(second, []byte("x"), 0644); err != nil {
		t.Fatal(err)
	}
	third, _ := uniqueFilePath(dir, "a.mp4")
	if filepath.Base(third) != "a(2).mp4" {
		t.Fatalf("递增序号错误: %s", third)
	}
}

func TestLimitReader(t *testing.T) {
	data := strings.Repeat("x", 100)
	r := &limitReader{r: strings.NewReader(data), max: 50, ctx: context.Background()}
	if _, err := io.ReadAll(r); !errors.Is(err, errTooLarge) {
		t.Fatalf("期望 errTooLarge，实际 %v", err)
	}

	ok := &limitReader{r: strings.NewReader(data), max: 200, ctx: context.Background()}
	got, err := io.ReadAll(ok)
	if err != nil || len(got) != 100 {
		t.Fatalf("上限内不应报错: %v (%d bytes)", err, len(got))
	}

	// 恰好等于上限不应误报
	exact := &limitReader{r: strings.NewReader(data), max: 100, ctx: context.Background()}
	got, err = io.ReadAll(exact)
	if err != nil || len(got) != 100 {
		t.Fatalf("恰好达上限不应报错: %v (%d bytes)", err, len(got))
	}
}

func TestLimitReaderContextCancel(t *testing.T) {
	ctx, cancel := context.WithCancel(context.Background())
	cancel()
	r := &limitReader{r: strings.NewReader("x"), max: 100, ctx: ctx}
	if _, err := io.ReadAll(r); !errors.Is(err, context.Canceled) {
		t.Fatalf("期望 context.Canceled，实际 %v", err)
	}
}

func TestSaveUpload(t *testing.T) {
	cfg := testUploadConfig(t)
	svc := NewUploadService(cfg, nil)

	res, err := svc.Save(context.Background(), "../../evil name.mp4", strings.NewReader("video-bytes"))
	if err != nil {
		t.Fatalf("Save 失败: %v", err)
	}
	if filepath.Base(res.Path) != "evil name.mp4" {
		t.Fatalf("文件名净化结果不符: %s", res.Path)
	}
	if res.Size != 11 {
		t.Fatalf("size 不符: %d", res.Size)
	}
	b, err := os.ReadFile(res.Path)
	if err != nil || string(b) != "video-bytes" {
		t.Fatalf("落盘内容不符: %v", err)
	}

	// 重名自动改名，不覆盖
	res2, err := svc.Save(context.Background(), "evil name.mp4", strings.NewReader("second"))
	if err != nil {
		t.Fatalf("二次 Save 失败: %v", err)
	}
	if !res2.Renamed || filepath.Base(res2.Path) != "evil name(1).mp4" {
		t.Fatalf("重名处理错误: %+v", res2)
	}

	// 非视频扩展名拒绝
	if _, err := svc.Save(context.Background(), "a.txt", strings.NewReader("x")); err == nil {
		t.Fatal("应当拒绝非视频格式")
	}

	// 超限拒绝且不残留 .part
	if _, err := svc.Save(context.Background(), "big.mp4", strings.NewReader(strings.Repeat("x", 200))); err == nil {
		t.Fatal("应当拒绝超限文件")
	}
	entries, _ := os.ReadDir(cfg.UploadsDir())
	for _, e := range entries {
		if strings.HasSuffix(e.Name(), ".part") {
			t.Fatalf("超限后残留临时文件: %s", e.Name())
		}
	}
}

// testUploadConfig 构造指向临时目录、上传上限 100 字节的配置。
func testUploadConfig(t *testing.T) *config.Config {
	return &config.Config{
		App: config.AppConfig{
			DataDir:       t.TempDir(),
			UploadDir:     "uploads",
			UploadMaxSize: 100,
		},
	}
}
