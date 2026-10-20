修复媒体目录删除权限：容器 entrypoint 新增 FCT_MEDIA_GID，把应用用户加入媒体目录属组并补组写权限，使网页端可删除媒体文件（宿主文件属主不变）；删除失败时对只读挂载/权限不足给出可操作提示。

## Docker 安装
```bash
docker pull mobufan/fan-video-ct:latest
docker run -d --name fan-video-ct -p 8788:8788 -v /var/lib/fan-video-ct:/data mobufan/fan-video-ct:v1.1.3
```

```bash
docker pull ghcr.io/meimolihan/fan-video-ct:v1.1.3
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
