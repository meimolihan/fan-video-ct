v1.0.0 正式版：UI 第三轮修复（Toast 点击穿透/空态覆盖/触屏热区 44px/超窄屏溢出/播放条轨道/移动端 hover 移除）

## Docker 安装
```bash
docker pull mobufan/fan-video-ct:latest
docker run -d --name fan-video-ct -p 8788:8788 -v /var/lib/fan-video-ct:/data mobufan/fan-video-ct:v1.0.0
```

```bash
docker pull ghcr.io/meimolihan/fan-video-ct:v1.0.0
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
