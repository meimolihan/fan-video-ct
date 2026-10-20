优化删除按钮图标：文件条目的删除按钮由 emoji 🗑 改为内联 SVG 垃圾桶图标，跨平台渲染一致、可随悬停与明暗主题着色，并新增按压反馈与无障碍标签。

## Docker 安装
```bash
docker pull mobufan/fan-video-ct:latest
docker run -d --name fan-video-ct -p 8788:8788 -v /var/lib/fan-video-ct:/data mobufan/fan-video-ct:v1.1.4
```

```bash
docker pull ghcr.io/meimolihan/fan-video-ct:v1.1.4
```

## 二进制安装
### Linux amd64 / arm64
```bash
bash -c "$(curl -sSL https://raw.githubusercontent.com/meimolihan/fan-video-ct/main/scripts/install.sh)" -p 8788
```

## 二进制卸载
```bash
bash -c "$(curl -sSL https://raw.githubusercontent.com/meimolihan/fan-video-ct/main/scripts/uninstall.sh)" -y
```
