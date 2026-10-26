# A2 前端验证：同一份前端分别连 Go 后端与 Python 参考后端，产出截图供像素比对。
#
# 约定：
#   Go 后端      → http://127.0.0.1:8891（systemd 服务）
#   Python 参考  → http://127.0.0.1:8893（参考实现容器）
set +e
cd /litesight
pip install -q --no-cache-dir -i https://mirrors.cloud.tencent.com/pypi/simple/ playwright==1.49.0
mkdir -p /out
echo "===== Go 后端 (8891) ====="
python3 tools/verify_web.py --url http://127.0.0.1:8891 --out /out/go.png
echo "===== Python 参考后端 (8893) ====="
python3 tools/verify_web.py --url http://127.0.0.1:8893 --out /out/py.png
