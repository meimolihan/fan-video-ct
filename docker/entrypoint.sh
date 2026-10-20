#!/bin/sh
#
# fan-video-ct 容器入口：权限自愈 + 降权运行
#
# 镜像以 root 启动本脚本，针对通过卷挂载进来的宿主目录（无论其属主/权限如何）
# 统一重建数据目录、封面目录与剪切输出目录，并全部交权给 fct 用户，
# 随后以 fct 身份执行应用（su-exec 降权，避免以 root 跑 web 服务）。
# 这样用户部署时只需挂载卷，无需关心宿主目录的 uid/gid。
set -eu

APP=/usr/local/bin/fan-video-ct
RUN_USER=fct

DATA_DIR="${FCT_APP_DATA_DIR:-/data}"
OUTPUT_DIR="${FCT_APP_OUTPUT_DIR:-output}"
# OUTPUT_DIR 相对路径基于数据目录解析（与应用内逻辑保持一致）
case "${OUTPUT_DIR}" in
    /*) ;;
    *)  OUTPUT_DIR="${DATA_DIR}/${OUTPUT_DIR}" ;;
esac
COVERS_DIR="${DATA_DIR}/covers"

# 1) 重建/校正目录，并把写权限全部交给 fct
mkdir -p "${DATA_DIR}" "${COVERS_DIR}" "${OUTPUT_DIR}" || true
chown -R "${RUN_USER}:${RUN_USER}" "${DATA_DIR}" "${COVERS_DIR}" "${OUTPUT_DIR}" || true
chmod 755 "${DATA_DIR}" || true

# 1b) 媒体目录可写支持（可选）：媒体目录通常挂载宿主的下载大盘，属主/属组与应用
#     用户 fct 不同，导致网页端删除媒体文件时报 permission denied。设置
#     FCT_MEDIA_GID 后，把 fct 加入该附加组，并（在可写前提下）为该组补写权限，
#     使 fct 能删除媒体目录内的文件，而无需改变宿主文件属主。
MEDIA_DIR="${FCT_APP_MEDIA_DIR:-}"
MEDIA_GID="${FCT_MEDIA_GID:-}"
if [ -n "${MEDIA_DIR}" ] && [ -n "${MEDIA_GID}" ] && [ -d "${MEDIA_DIR}" ]; then
    # fct 加入附加组：gid 对应组不存在时先建（BusyBox addgroup/adduser 语义）
    MEDIA_GROUP="$(awk -F: -v g="${MEDIA_GID}" '$3==g{print $1; exit}' /etc/group)"
    if [ -z "${MEDIA_GROUP}" ]; then
        MEDIA_GROUP="fct-media"
        addgroup -g "${MEDIA_GID}" "${MEDIA_GROUP}" 2>/dev/null || true
    fi
    adduser "${RUN_USER}" "${MEDIA_GROUP}" 2>/dev/null || true
    # 为媒体目录组补写权限；失败（如只读挂载）不致命，仅提示
    if chmod g+w "${MEDIA_DIR}" 2>/dev/null; then
        echo "[entrypoint] media=${MEDIA_DIR} 已为组 ${MEDIA_GID} 开放写权限"
    else
        echo "[entrypoint] media=${MEDIA_DIR} 无法开放写权限（可能为只读挂载）" >&2
    fi
fi

# 2) 打印启动信息（经 stdout 传给 docker logs）
echo "[entrypoint] data=${DATA_DIR} covers=${COVERS_DIR} output=${OUTPUT_DIR}"

# 3) 降权运行应用；保留原有命令行参数（如 --port 等）
exec su-exec "${RUN_USER}" "${APP}" "$@"