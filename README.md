# fan-video-ct

视频剪切工具：基于 FFmpeg 的轻量级 Web 视频剪切服务。内置网页播放器与时间轴选区，支持「直接剪切（stream copy，不重编码）」「精确转码（重编码）」两种模式、封面管理与片头片尾预设，前后端打包为单个自包含二进制，无外部依赖安装。

## 功能特性

- **视频浏览**：内置文件浏览器，直接播放本地视频（支持 HTTP Range 拖拽播放）；播放器支持停止/全屏、起终点预览播放（▶起点 / ▶末尾10s）
- **两种剪切模式**
  - `快速剪切`：stream copy，不重编码，秒级完成；片段边界自动吸附到最近关键帧（时间轴显示关键帧刻度）
  - `精确转码`：libx264 重编码 + AAC，可精确到任意帧；支持 `-movflags +faststart` 边下边播
- **时间轴选区**：点击/拖拽左右手柄设定起止时间，拖动/微调锚点实时跳帧预览；选区外（起点前/终点后）进度条清空置灰，直观呈现剪切范围
- **精确微调**：0.1/0.5/1/5/10s 步长微调按钮 + 快捷键（`空格` 播放/暂停、`←`/`→` 跳帧、`[`/`]` 设起/终点、「当前帧」一键落点
- **片头片尾预设**：服务端持久化的预设库（名称 + 片头跳过秒 + 片尾跳过秒），一键回填起止时间（起点=片头，终点=总时长−片尾）；新增/编辑/删除预设，换浏览器/设备数据不丢失
- **任务队列**：后台异步剪切，实时进度、支持取消与结果下载；并发数可配置
- **封面管理**：默认首帧预览、截任意帧设封面、上传自定义封面、移除封面
- **明暗主题**：亮/暗/跟随系统三态切换，持久化到浏览器
- **单文件部署**：前端静态资源 `go:embed` 进二进制，无 Node/构建步骤；amd64 / arm64 二进制 + Docker multi-arch
- **模式参考**：结构与发布链路沿用同仓库 `fan-video` / `fan-files` / `2panel` 项目惯例

## 快速开始

### 二进制运行

```bash
# 本地构建
make build
./bin/fan-video-ct -data ./data -port 8788

# 打开 http://127.0.0.1:8788
```

**指定端口 / 数据目录 / 原视频目录 / 剪切输出目录**（四个目标与指定方式）：

| 目标 | CLI 参数 | 配置 / 环境变量（优先级更高） |
| --- | --- | --- |
| 端口 | `-port 8788` | `app.port` · `FCT_APP_PORT=8788` |
| 数据目录 | `-data /var/lib/fan-video-ct/data` | `app.data_dir` · `FCT_APP_DATA_DIR` |
| 剪切输出目录 | 无 CLI 参数（仅配置） | `app.output_dir` · `FCT_APP_OUTPUT_DIR` |
| 原视频目录 | `-media /vol2/1000/downloads/fan-video-ct` | `app.media_dir` · `FCT_APP_MEDIA_DIR` |

直接运行示例：

```bash
./fan-video-ct -port 8788 -data /var/lib/fan-video-ct/data -media /vol2/1000/downloads/fan-video-ct
```

剪切输出目录无命令行开关，二选一：

```bash
# ① 环境变量
FCT_APP_OUTPUT_DIR=/mnt/fct-output ./fan-video-ct -port 8788 -data /var/lib/fan-video-ct/data -media /vol2/1000/downloads/fan-video-ct

# ② config.yaml（搜路径：当前目录 / ./data 即数据目录 / /etc/fan-video-ct）
# /var/lib/fan-video-ct/data/config.yaml
app:
  port: 8788
  data_dir: /var/lib/fan-video-ct/data
  media_dir: /vol2/1000/downloads/fan-video-ct
  output_dir: /mnt/fct-output
```

> 优先级：环境变量 > config.yaml > CLI 参数 > 内置默认。数据目录（安装时由 `install.sh -d` 指定）即封面、剪切输出、预设 `presets.json`、`config.yaml` 的存放位置；输出目录相对路径基于数据目录，绝对路径原样生效。

### 安装脚本（systemd 服务）

```bash
# -p 端口 | -d 安装目录（二进制+数据，默认 /var/lib/fan-video-ct）| -s 视频浏览根目录（可选）
bash -c "$(curl -sSL https://raw.githubusercontent.com/meimolihan/fan-video-ct/main/scripts/install.sh)" -p 8788 -d /var/lib/fan-video-ct -s /vol1/1000/Video
```

卸载：

```bash
bash -c "$(curl -sSL https://raw.githubusercontent.com/meimolihan/fan-video-ct/main/scripts/uninstall.sh)" -y
```

### Docker

数据目录挂载（剪切输出在宿主机的 `<数据目录>/output`，同时保存封面、预设等）：

```bash
docker run -d --name fan-video-ct -p 8788:8788 \
  -v /var/lib/fan-video-ct/data:/data \
  mobufan/fan-video-ct:latest
```

**映射原视频目录**（让文件浏览器可直接浏览宿主机视频）：把视频目录只读挂载进容器，并通过 `FCT_APP_MEDIA_DIR` 让浏览器默认从该目录开始：

```bash
docker run -d --name fan-video-ct -p 8788:8788 \
  -e FCT_APP_MEDIA_DIR=/media \
  -v /vol2/1000/downloads/fan-video-ct:/media:ro \
  -v /var/lib/fan-video-ct/data:/data \
  mobufan/fan-video-ct:latest
```

多个视频目录：每条 `-v 宿主机目录:/media/子目录:ro` 挂一份即可（页面内通过目录层级访问）。也可挂整盘浏览：

```bash
docker run -d --name fan-video-ct -p 8788:8788 \
  -e FCT_APP_MEDIA_DIR=/host \
  -v /:/host:ro \
  -v /var/lib/fan-video-ct/data:/data \
  mobufan/fan-video-ct:latest
```

> 提示：视频目录只读挂载（`:ro`）即可，剪切输出与封面写入都在 `/data`；全盘挂载权限较高，仅在信任的隔离环境使用。容器内以非 root 用户运行，挂载目录需对容器可读。

或 `docker compose up -d`（见 [docker-compose.yml](docker-compose.yml)）。

## 命令行

| 命令 / 选项 | 说明 |
| --- | --- |
| （无命令） | 启动 Web 服务（可加 `-port` / `-data` / `-media`） |
| `status` | 查看运行状态（banner + 安装信息、运行方式、PID、端口、访问地址、运行时长、内存、路径、健康检查） |
| `start` / `stop` / `restart` | 启动 / 停止 / 重启服务（systemd 优先，其次直接管理进程） |
| `uninstall` | 停止服务并移除 systemd 安装 |
| `uninstall -y` / `--yes` | 免确认卸载（默认保留数据目录） |
| `uninstall --purge` | 卸载同时删除数据目录 |
| `uninstall --keep-data` | 卸载时保留数据目录 |
| `backup` / `restore` | 备份/恢复提示（直接归档/还原数据目录，见下） |
| `version` / `-version` / `--version` / `-v` | 打印版本号 |
| `help` / `-h` / `--help` | 显示帮助（banner + 命令表 + 访问地址） |

备份数据目录（含封面、剪切输出、任务历史、片头片尾预设 `presets.json`）即完成整体备份；恢复则将归档解压回数据目录并重启服务。

## 配置

配置文件 `config.yaml`（搜索路径：当前目录 / `./data` / `/etc/fan-video-ct`）或环境变量 `FCT_` 前缀覆盖，示例见 [config.example.yaml](config.example.yaml)。常用项：

| 配置 | 环境变量 | 默认 | 说明 |
| --- | --- | --- | --- |
| `app.port` | `FCT_APP_PORT` | `8788` | 监听端口 |
| `app.data_dir` | `FCT_APP_DATA_DIR` | `./data` | 数据目录 |
| `app.media_dir` | `FCT_APP_MEDIA_DIR` | 空 | 文件浏览器根目录 |
| `app.output_dir` | `FCT_APP_OUTPUT_DIR` | `output`（相对数据目录） | 剪切输出目录 |
| `app.worker` | `FCT_APP_WORKER` | `1` | 剪切任务并发数 |
| `ffmpeg.preset` | `FCT_FFMPEG_PRESET` | `veryfast` | 重编码预设 |
| `ffmpeg.crf` | `FCT_FFMPEG_CRF` | `18` | 重编码质量 |

## 片头片尾预设

「剪切片段」模块右侧的【片头片尾预设】按钮打开预设库弹窗，可**新增 / 编辑 / 删除任意多条预设**，每条含：预设名称（唯一）、片头跳过时长（秒）、片尾跳过时长（秒）。

- **回填**：点击列表中任一条目，自动计算并回填时间轴——`起点 = 片头时长`，`终点 = 视频总时长 − 片尾时长`，随后沿用原有剪切逻辑（快速/转码均可），新起点帧实时预览
- **内置默认预设**（程序首次启动自动生成）：电视剧通用（片头 90s / 片尾 60s）、短剧（15s / 20s）、动漫（85s / 75s）
- **持久化**：预设保存在服务端数据目录 `presets.json`（非浏览器 localStorage），换浏览器/设备不丢失；文件读写带并发锁（原子落盘），首次启动或文件缺失/损坏自动重建
- **接口**：`GET /api/presets` 列表、`POST /api/presets` 新增/编辑（同名即更新）、`DELETE /api/presets/:name` 删除（中文名需 URL 编码）

## HTTP API

| 方法 | 路径 | 说明 |
| --- | --- | --- |
| GET | `/api/health` | 健康检查 |
| GET | `/api/version` | 版本信息 |
| GET | `/api/media/dir?path=` | 列出目录 |
| GET | `/api/media/info?path=` | 探测媒体信息（时长/流/封面是否存在） |
| GET | `/api/media/stream?path=` | 播放视频流（支持 Range） |
| GET | `/api/media/keyframes?path=` | 关键帧时间点列表（快速剪切对齐用） |
| POST | `/api/tasks` | 创建剪切任务 `{path, mode, start, end, output_name}` |
| GET | `/api/tasks` / `/api/tasks/:id` | 任务列表 / 详情 |
| DELETE | `/api/tasks/:id` / `/api/tasks` | 取消任务 / 清空已完成任务 |
| GET | `/api/tasks/:id/download` | 下载剪切结果 |
| GET/POST/DELETE | `/api/cover?path=&t=` | 查看 / 截帧设封面 / 移除封面 |
| POST | `/api/cover/upload?path=` | 上传自定义封面（multipart 字段 `file`） |
| GET | `/api/cover/preview?path=&t=` | 指定时间帧预览（不落盘，默认首帧） |
| GET/POST | `/api/presets` | 片头片尾预设：列表 / 新增或编辑 |
| DELETE | `/api/presets/:name` | 按名称删除预设 |

## 发布

- 本地产物：`bash scripts/build-release.sh` → `dist/`（amd64+arm64 二进制 + `sha256sums.txt` + 安装脚本 + 示例配置）
- 线上发布：`bash scripts/build-and-push.sh v1.0.0 --yes -m "发版说明"`（bump 版本号 → 推送 tag → 触发 GitHub Actions release.yml：构建二进制+sha256 创建 Release，并用 buildx 推送 amd64/arm64 镜像到 `mobufan/fan-video-ct` 与 `ghcr.io/meimolihan/fan-video-ct`）
- 依赖：FFmpeg / FFprobe（Debian/Ubuntu `apt install ffmpeg` ｜ Alpine `apk add ffmpeg` ｜ CentOS `yum install ffmpeg`）；安装脚本在缺失时自动尝试安装

## 开发

```bash
make build     # 编译
make vet       # 静态检查
make test      # 运行测试（当前无单测）
make fmt       # 格式化
```

前端为 `internal/embedded/web/` 下的纯静态页面（HTML/CSS/JS），`go:embed` 自动内嵌，无需构建步骤。

## 许可证

[Apache License 2.0](LICENSE)