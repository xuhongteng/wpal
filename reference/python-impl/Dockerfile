# syntax=docker/dockerfile:1
#
# 服务器资源监控看板服务镜像
#
# 关键约束：
#   * 容器以 --pid host --network host 运行 → /proc、/proc/net 即宿主的
#   * 宿主根目录只读挂载到 /host/root → 读取宿主真实挂载表与磁盘容量
#   * 宿主 python3.14 无 pip，依赖全部在镜像内安装
#
FROM python:3.12-slim

ENV PYTHONDONTWRITEBYTECODE=1 \
    PYTHONUNBUFFERED=1 \
    PYTHONPATH=/app \
    PIP_INDEX_URL=https://mirrors.cloud.tencent.com/pypi/simple/ \
    PIP_DISABLE_PIP_VERSION_CHECK=1 \
    PIP_ROOT_USER_ACTION=ignore \
    MON_HOST_ROOT=/host/root \
    MON_DATA_DIR=/app/data

WORKDIR /app

COPY requirements.txt /app/requirements.txt
RUN pip install --no-cache-dir -r /app/requirements.txt

COPY app/ /app/app/

RUN mkdir -p /app/data

EXPOSE 8891

# 采集需要读取宿主任意进程的 /proc 数据 → 以 root 运行（容器内 root 即宿主 root）
CMD ["uvicorn", "app.main:app", "--host", "127.0.0.1", "--port", "8891", "--proxy-headers", "--ws", "auto"]
