// Package ffmpeg 封装对 FFmpeg / FFprobe 的调用，提供媒体探测、视频剪切、
// 帧截图与进度解析等视频处理能力。FFmpeg 通过内置路径或配置指定，调用方
// 需保证环境中存在 ffmpeg / ffprobe 可执行文件。
package ffmpeg

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"os"
	"os/exec"
	"strconv"
	"strings"
)

// DetectError 表示未找到可执行文件或当前环境无法运行 FFmpeg。
type DetectError struct{ bin string }

func (e *DetectError) Error() string {
	return fmt.Sprintf("未找到可执行文件 %q，请安装 ffmpeg 或在配置中指定 ffmpeg.path / ffmpeg.ffprobe_path", e.bin)
}

// Client FFmpeg 调用客户端。
type Client struct {
	ffmpegBin    string
	ffprobeBin   string
	threads      int
	preset       string
	crf          int
	audioBitrate string
}

// Options FFmpeg 客户端配置项。
type Options struct {
	FFmpegBin    string
	FFprobeBin   string
	Threads      int
	Preset       string
	CRF          int
	AudioBitrate string
}

// New 创建 FFmpeg 客户端，并校验可执行文件存在。
func New(opts Options) (*Client, error) {
	ffmpegBin := opts.FFmpegBin
	if ffmpegBin == "" {
		ffmpegBin = "ffmpeg"
	}
	ffprobeBin := opts.FFprobeBin
	if ffprobeBin == "" {
		ffprobeBin = "ffprobe"
	}
	if _, err := exec.LookPath(ffmpegBin); err != nil {
		return nil, &DetectError{bin: ffmpegBin}
	}
	if _, err := exec.LookPath(ffprobeBin); err != nil {
		return nil, &DetectError{bin: ffprobeBin}
	}
	preset := opts.Preset
	if preset == "" {
		preset = "veryfast"
	}
	crf := opts.CRF
	if crf <= 0 {
		crf = 18
	}
	bitrate := opts.AudioBitrate
	if bitrate == "" {
		bitrate = "192k"
	}
	return &Client{
		ffmpegBin:    ffmpegBin,
		ffprobeBin:   ffprobeBin,
		threads:      opts.Threads,
		preset:       preset,
		crf:          crf,
		audioBitrate: bitrate,
	}, nil
}

// ==================== 媒体探测 ====================

// MediaStream 媒体流信息（只保留前端/剪切需要的字段）。
type MediaStream struct {
	Index     int     `json:"index"`
	CodecType string  `json:"codec_type"`
	CodecName string  `json:"codec_name"`
	Width     int     `json:"width"`
	Height    int     `json:"height"`
	Duration  float64 `json:"duration"`
	BitRate   int64   `json:"bit_rate"`
	PixFmt    string  `json:"pix_fmt"`
	Profile   string  `json:"profile"`
	// IsAttachedPic 是否为内嵌封面（attached_pic）流。
	IsAttachedPic bool `json:"is_attached_pic"`
}

// MediaInfo 媒体文件探测结果。
type MediaInfo struct {
	Path       string        `json:"path"`
	Duration   float64       `json:"duration"`
	Size       int64         `json:"size"`
	BitRate    int64         `json:"bit_rate"`
	FormatName string        `json:"format_name"`
	Streams    []MediaStream `json:"streams"`
}

type ffprobeFormat struct {
	Duration   string `json:"duration"`
	Size       string `json:"size"`
	BitRate    string `json:"bit_rate"`
	FormatName string `json:"format_name"`
}

type ffprobeStream struct {
	Index       int            `json:"index"`
	CodecType   string         `json:"codec_type"`
	CodecName   string         `json:"codec_name"`
	Width       int            `json:"width"`
	Height      int            `json:"height"`
	Duration    string         `json:"duration"`
	BitRate     string         `json:"bit_rate"`
	PixFmt      string         `json:"pix_fmt"`
	Profile     string         `json:"profile"`
	Disposition map[string]int `json:"disposition"`
}

type ffprobeOutput struct {
	Format  ffprobeFormat   `json:"format"`
	Streams []ffprobeStream `json:"streams"`
}

// Probe 使用 ffprobe 探测媒体文件信息。
func (c *Client) Probe(ctx context.Context, path string) (*MediaInfo, error) {
	args := []string{
		"-v", "error",
		"-print_format", "json",
		"-show_format",
		"-show_streams",
		path,
	}
	cmd := exec.CommandContext(ctx, c.ffprobeBin, args...)
	var stdout, stderr bytes.Buffer
	cmd.Stdout = &stdout
	cmd.Stderr = &stderr
	if err := cmd.Run(); err != nil {
		msg := strings.TrimSpace(stderr.String())
		if msg == "" {
			msg = err.Error()
		}
		return nil, fmt.Errorf("ffprobe 探测 %s 失败: %s", path, msg)
	}

	var out ffprobeOutput
	if err := json.Unmarshal(stdout.Bytes(), &out); err != nil {
		return nil, fmt.Errorf("解析 ffprobe 输出失败: %w", err)
	}

	info := &MediaInfo{Path: path}
	info.Duration = parseFloat(out.Format.Duration)
	info.Size = parseInt64(out.Format.Size)
	info.BitRate = parseInt64(out.Format.BitRate)
	info.FormatName = out.Format.FormatName
	for _, s := range out.Streams {
		info.Streams = append(info.Streams, MediaStream{
			Index:         s.Index,
			CodecType:     s.CodecType,
			CodecName:     s.CodecName,
			Width:         s.Width,
			Height:        s.Height,
			Duration:      parseFloat(s.Duration),
			BitRate:       parseInt64(s.BitRate),
			PixFmt:        s.PixFmt,
			Profile:       s.Profile,
			IsAttachedPic: s.Disposition["attached_pic"] == 1,
		})
	}
	return info, nil
}

// Keyframes 返回视频流关键帧时间点（秒，升序），供"快速剪切"对齐起止点。
// 通过 ffprobe 读取包级 flags（仅解封装不解码），对长视频也足够快。
func (c *Client) Keyframes(ctx context.Context, path string) ([]float64, error) {
	args := []string{
		"-v", "error",
		"-select_streams", "v:0",
		"-show_entries", "packet=pts_time,flags",
		"-of", "csv=p=0",
		path,
	}
	cmd := exec.CommandContext(ctx, c.ffprobeBin, args...)
	var stdout, stderr bytes.Buffer
	cmd.Stdout = &stdout
	cmd.Stderr = &stderr
	if err := cmd.Run(); err != nil {
		msg := strings.TrimSpace(stderr.String())
		if msg == "" {
			msg = err.Error()
		}
		return nil, fmt.Errorf("ffprobe 读取关键帧 %s 失败: %s", path, msg)
	}
	var keys []float64
	sc := strings.Split(stdout.String(), "\n")
	for _, line := range sc {
		line = strings.TrimSpace(line)
		if line == "" {
			continue
		}
		// csv 行格式: pts_time,flags
		parts := strings.SplitN(line, ",", 2)
		if len(parts) != 2 || !strings.Contains(parts[1], "K") {
			continue
		}
		pts := parseFloat(parts[0])
		if pts >= 0 {
			keys = append(keys, pts)
		}
	}
	return keys, nil
}

func parseFloat(s string) float64 {
	f, err := strconv.ParseFloat(strings.TrimSpace(s), 64)
	if err != nil {
		return 0
	}
	return f
}

func parseInt64(s string) int64 {
	// ffprobe bit_rate 可能带 "N/A" 或科学计数
	n, err := strconv.ParseInt(strings.TrimSpace(s), 10, 64)
	if err != nil {
		if f, err := strconv.ParseFloat(strings.TrimSpace(s), 64); err == nil {
			return int64(f)
		}
		return 0
	}
	return n
}

// ==================== 内嵌封面 ====================

// EmbeddedCover 描述视频内嵌封面（attached_pic）流。
type EmbeddedCover struct {
	// Index 封面流在文件中的索引（供 -map 使用）。
	Index int
	// Codec 图片编码名：mjpeg / png / webp 等。
	Codec string
}

// EmbeddedCoverInfo 探测视频是否内嵌封面（attached_pic 流）。
// 用 ffprobe 读取流 disposition，不解码视频帧，速度足够快。
func (c *Client) EmbeddedCoverInfo(ctx context.Context, path string) (EmbeddedCover, bool, error) {
	info, err := c.Probe(ctx, path)
	if err != nil {
		return EmbeddedCover{}, false, err
	}
	for _, s := range info.Streams {
		if s.IsAttachedPic {
			return EmbeddedCover{Index: s.Index, Codec: s.CodecName}, true, nil
		}
	}
	return EmbeddedCover{}, false, nil
}

// ExtractEmbeddedCover 将内嵌封面流完整抽出为图片字节（stream copy，不重编码）。
// attached_pic 流本身就是一张完整图片（mjpeg/png/webp），copy 即可无损还原。
func (c *Client) ExtractEmbeddedCover(ctx context.Context, path string, idx int, codec string) ([]byte, error) {
	tmp, err := os.CreateTemp("", "fvct-cover-*"+extForImageCodec(codec))
	if err != nil {
		return nil, err
	}
	tmpName := tmp.Name()
	_ = tmp.Close()
	defer os.Remove(tmpName)

	args := []string{
		"-v", "error",
		"-y",
		"-i", path,
		"-map", "0:" + strconv.Itoa(idx),
		"-c", "copy",
		tmpName,
	}
	cmd := exec.CommandContext(ctx, c.ffmpegBin, args...)
	var stderr bytes.Buffer
	cmd.Stderr = &stderr
	if err := cmd.Run(); err != nil {
		msg := strings.TrimSpace(stderr.String())
		if msg == "" {
			msg = err.Error()
		}
		return nil, fmt.Errorf("提取内嵌封面 %s 失败: %s", path, msg)
	}
	return os.ReadFile(tmpName)
}

// extForImageCodec 由图片编码推导输出文件扩展名。
func extForImageCodec(codec string) string {
	switch strings.ToLower(codec) {
	case "png":
		return ".png"
	case "webp":
		return ".webp"
	default:
		return ".jpg"
	}
}

// ==================== 校验输入 ====================

// ValidateTimeRange 校验剪切时间区间是否合法。
func ValidateTimeRange(start, end, duration float64) error {
	if duration <= 0 {
		return errors.New("无法获取媒体时长，请先探测该视频")
	}
	if start < 0 || end < 0 {
		return errors.New("起始/结束时间不能为负数")
	}
	if start >= end {
		return errors.New("结束时间必须大于起始时间")
	}
	if end > duration {
		return errors.New("结束时间超出视频时长")
	}
	return nil
}
