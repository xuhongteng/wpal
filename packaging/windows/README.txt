Wpal - 轻量级服务器资源监控
=========================================

本目录内容
-----------
wpal.exe        单文件监控程序（无运行时依赖）
config.yaml       配置文件
wpal.xml          MSI 安装程序使用的服务定义
wpal-service.exe  注册/注销 Windows 服务的可选辅助工具

快速开始
-----------
1. 以管理员身份打开命令提示符。
2. 先直接运行服务做一次快速验证：

   wpal.exe --config "%ProgramData%\Wpal\config.yaml"

3. 随后即可访问看板：

   http://127.0.0.1:8891/

   服务默认只监听回环地址。若要暴露到局域网，请在前面放一层反向代理
   （nginx / IIS / Caddy），并在那里终结 TLS。

注册为 Windows 服务（可选）
----------------------------------------
   wpal-service.exe install
   wpal-service.exe start

   如需移除：

   wpal-service.exe stop
   wpal-service.exe remove

数据目录
--------------
历史数据以有界环形文件的形式存放在 %ProgramData%\Wpal\data，
因此磁盘占用不会超过配置的留存时长。

文档
--------------
https://github.com/xuhongteng/wpal
