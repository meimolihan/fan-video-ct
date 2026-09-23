package ffmpeg

import (
	"bufio"
	"context"
	"fmt"
	"io"
	"os"
	"os/exec"
	"path/filepath"
	"strconv"
	"strings"
)

// CutMode 剪切模式。
type CutMode string

const (
	// ModeCopy 直接剪切：stream copy，不重新编码，速度快。
	// 片段边界会落在最近的关键帧，不能精确到任意帧。
	ModeCopy CutMode = "copy"
	// ModeReencode 转码剪切：重新编码，可精确到任意帧，兼容容器格式。
	ModeReencode CutMode = "reencode"
)

// ValidMode 判断剪切模式是否合法。
func ValidMode(mode string) bool {
	switch CutMode(mode) {
	case ModeCopy, ModeReencode:
		return true
	}
	return false
}

// OutputExtension 根据输入文件与剪切模式推导输出扩展名。
// 直接剪切尽量复用输入容器；转码剪切统一输出 mp4。
func OutputExtension(inputPath string, mode CutMode) string {
	if mode == ModeReencode {
		return ".mp4"
	}
	ext := strings.ToLower(filepath.Ext(inputPath))
	switch ext {
	case ".mp4", ".mkv", ".mov", ".avi", ".webm", ".flv", ".m4v", ".ts":
		return ext
	default:
		return ".mp4"
	}
}

// buildCutArgs 组装剪切命令行。
// 统一将 -ss 放在 -i 前（输入快速 seek），并使用 -t 时长截取，在 copy 与
// reencode 两种模式下都能获得稳定行为。
func (c *Client) buildCutArgs(input, output string, start, duration float64, mode CutMode) []string {
	args := []string{
		"-y",
		"-ss", fmt.Sprintf("%.3f", start),
	}
	if c.threads > 0 {
		args = append(args, "-threads", strconv.Itoa(c.threads))
	}
	args = append(args, "-i", input, "-t", fmt.Sprintf("%.3f", duration))

	if mode == ModeCopy {
		args = append(args,
			"-map", "0", // 直接剪切保留全部流（含内嵌封面）
			"-c", "copy",
			"-avoid_negative_ts", "make_zero",
		)
	} else {
		// 转码剪切：只映射首个视频流与全部音频流，
		// 排除内嵌封面（attached pic 常为奇数分辨率，libx264 无法编码）。
		args = append(args,
			"-map", "0:v:0", "-map", "0:a?",
			"-c:v", "libx264",
			"-preset", c.preset,
			"-crf", strconv.Itoa(c.crf),
			"-pix_fmt", "yuv420p",
			"-c:a", "aac",
			"-b:a", c.audioBitrate,
			"-movflags", "+faststart",
		)
	}
	args = append(args, output)
	return args
}

// RunCut 执行剪切任务，并实时回调 0~1 的进度值。
// 当 ctx 被取消（任务取消按时）会终止 ffmpeg 进程并清理未完成输出文件。
func (c *Client) RunCut(ctx context.Context, input, output string, start, duration float64, mode CutMode, progress func(float64)) error {
	cleanup := func() {
		_ = os.Remove(output)
		if thumb := output + ".jpg"; mode == ModeCopy {
			_ = os.Remove(thumb)
		}
	}

	cmd := exec.CommandContext(ctx, c.ffmpegBin, c.buildCutArgs(input, output, start, duration, mode)...)
	stdout, err := cmd.StdoutPipe()
	if err != nil {
		return err
	}
	stderr, err := cmd.StderrPipe()
	if err != nil {
		return err
	}
	if err := cmd.Start(); err != nil {
		return fmt.Errorf("启动 ffmpeg 失败: %w", err)
	}

	canceled := make(chan struct{}, 1)
	go func() {
		defer close(canceled)
		if duration <= 0 {
			return
		}
		scanner := bufio.NewScanner(stdout)
		scanner.Buffer(make([]byte, 0, 64*1024), 256*1024)
		for scanner.Scan() {
			line := scanner.Text()
			if strings.HasPrefix(line, "out_time_ms=") {
				ms, err := strconv.ParseInt(strings.TrimPrefix(line, "out_time_ms="), 10, 64)
				if err == nil && progress != nil {
					p := float64(ms/1000) / duration
					if p < 0 {
						p = 0
					}
					if p > 1 {
						p = 1
					}
					progress(p)
				}
			} else if strings.HasPrefix(line, "progress=") && strings.TrimPrefix(line, "progress=") == "end" {
				if progress != nil {
					progress(1)
				}
			}
		}
	}()

	errBuf := &limitedBuffer{max: 64 * 1024}
	go func() {
		_, _ = io.Copy(errBuf, stderr)
	}()

	runErr := cmd.Wait()

	if ctx.Err() != nil {
		// 任务被取消
		cleanup()
		return context.Canceled
	}
	if runErr != nil {
		cleanup()
		return fmt.Errorf("ffmpeg 执行失败: %v（%s）", runErr, errBuf.String())
	}
	if progress != nil {
		progress(1)
	}
	return nil
}

// limitedBuffer 有限大小的错误信息缓冲，避免单条重复日志把内存撑爆。
type limitedBuffer struct {
	buf []byte
	max int
}

func (b *limitedBuffer) Write(p []byte) (int, error) {
	if len(b.buf) < b.max {
		room := b.max - len(b.buf)
		if len(p) > room {
			p = p[:room]
		}
		b.buf = append(b.buf, p...)
	}
	return len(p), nil
}

func (b *limitedBuffer) String() string {
	return strings.TrimSpace(string(b.buf))
}

// copyWithLineFilter 保留：用于兼容旧引用。
func copyWithLineFilter(src io.Reader, dst *limitedBuffer, _ func(string)) (int64, error) {
	return io.Copy(dst, src)
}

// ==================== 帧截图 / 封面 ====================

// CaptureFrame 在指定时间点截取一帧并输出为图片（默认 jpg）。
func (c *Client) CaptureFrame(ctx context.Context, input string, at float64, output string) error {
	args := []string{
		"-y",
		"-ss", fmt.Sprintf("%.3f", at),
		"-i", input,
		"-frames:v", "1",
		"-q:v", "2",
		output,
	}
	cmd := exec.CommandContext(ctx, c.ffmpegBin, args...)
	var stderr strings.Builder
	cmd.Stderr = &stderr
	if err := cmd.Run(); err != nil {
		return fmt.Errorf("截取帧失败（时间 %.3fs）: %v（%s）", at, err, strings.TrimSpace(stderr.String()))
	}
	return nil
}

// CaptureFrameBytes 在指定时间点截取一帧并返回图片字节与 Content-Type，
// 供预览与封面生成使用。
func (c *Client) CaptureFrameBytes(ctx context.Context, input string, at float64) ([]byte, string, error) {
	dir, err := os.MkdirTemp("", "fan-video-ct-frame-*")
	if err != nil {
		return nil, "", err
	}
	defer os.RemoveAll(dir)

	out := filepath.Join(dir, "frame.jpg")
	if err := c.CaptureFrame(ctx, input, at, out); err != nil {
		return nil, "", err
	}
	data, err := os.ReadFile(out)
	if err != nil {
		return nil, "", err
	}
	return data, "image/jpeg", nil
}

// EnsureFFmpegAvailable 供启动时快速自检，返回检测到的版本字符串。
func (c *Client) EnsureFFmpegAvailable(ctx context.Context) (string, error) {
	out, err := exec.CommandContext(ctx, c.ffmpegBin, "-version").Output()
	if err != nil {
		return "", err
	}
	first := strings.SplitN(string(out), "\n", 2)[0]
	return strings.TrimSpace(first), nil
}

// EnsureFFprobeAvailable 供启动时快速自检 FFprobe。
func (c *Client) EnsureFFprobeAvailable(ctx context.Context) error {
	if _, err := exec.CommandContext(ctx, c.ffprobeBin, "-version").Output(); err != nil {
		return err
	}
	return nil
}

// SuggestedCaptureTime 返回无封面时默认截取的推荐时间点（取中位时间）。
func SuggestedCaptureTime(duration float64) float64 {
	if duration <= 0 {
		return 0
	}
	return duration / 2
}
