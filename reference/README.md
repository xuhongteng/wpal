# 参考实现（Python）

`python-impl/` 是 Litesight 的**参考实现**，用于跨实现验证：

- `tools/compare.py` 用它作为基准，逐字段比对 Go 采集器的输出；
- `tools/verify_web.py` + `tools/compare_shots.py` 用**运行中的**参考实例
  （`/home/ubuntu/monitor` 目录下的完整副本，含前端资源）做前端截图比对，
  证明同一份前端在两个后端下渲染一致。

归档在此的副本只用于字段比对，因此省掉了 1 MB 的前端 vendor 目录。

它不是运行时依赖，也不在发布产物里。

## 为什么保留

跨语言重写最大的风险是"看着对、其实算法不同"。
把参考实现放进仓库，可以让任何人都复现这套比对：

```bash
# 1) 起参考实现（另一个端口）
docker compose -f reference/python-impl/docker-compose.yml up -d

# 2) 与 Go 版逐字段比对
python3 tools/compare.py --python http://127.0.0.1:8893 --go ./bin/wpal
```

## 口径说明

参考实现基于 psutil，Go 实现基于直读 `/proc`（Linux）与 gopsutil（其他平台）。
两者的字段语义在 `internal/collector/collector.go` 中有明确约定，
比对脚本按"累计量严格相等、百分比取窗口均值、速率宽松"三类分别判定。

修改任一边的采集逻辑时，记得同步另一侧并重跑 `make verify`。
