# 发版说明

> 本文件由 `scripts/build-and-push.sh` 自动重写（含发版备注与安装命令）。
> 手动发布外发的说明请直接编辑本文件。

## 安装

### Docker

```bash
docker pull mobufan/fan-video-ct:latest
docker run -d --name fan-video-ct -p 8788:8788 -v /var/lib/fan-video-ct/data:/data mobufan/fan-video-ct
```

### 二进制（systemd 服务）

```bash
bash -c "$(curl -sSL https://raw.githubusercontent.com/meimolihan/fan-video-ct/main/scripts/install.sh)" -p 8788 -d /var/lib/fan-video-ct -s /vol1/1000/Video
```

### 卸载

```bash
bash -c "$(curl -sSL https://raw.githubusercontent.com/meimolihan/fan-video-ct/main/scripts/uninstall.sh)" -y
```