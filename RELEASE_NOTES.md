补全 Docker 映射目录

## Docker 安装
```bash
docker pull mobufan/fan-video-ct:latest
docker run -d --name fan-video-ct -p 8788:8788 -v /var/lib/fan-video-ct:/data mobufan/fan-video-ct:v1.0.6
```

```bash
docker pull ghcr.io/meimolihan/fan-video-ct:v1.0.6
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
