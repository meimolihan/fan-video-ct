明暗主题切换增加中心辐射羽化动画：点击顶栏明暗按钮时，新旧主题快照以屏幕中心为圆心、带 24% 羽化带的径向遮罩向外扩张铺满全屏（View Transitions API）。不支持该 API 或系统开启「减弱动效」时回退为即时切换。

## Docker 安装
```bash
docker pull mobufan/fan-video-ct:latest
docker run -d --name fan-video-ct -p 8788:8788 -v /var/lib/fan-video-ct:/data mobufan/fan-video-ct:v1.1.1
```

```bash
docker pull ghcr.io/meimolihan/fan-video-ct:v1.1.1
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
