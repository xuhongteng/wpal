Wpal - Lightweight Server Resource Monitoring
==================================================

Files in this directory
-----------------------
wpal.exe        The single-file monitoring binary (no runtime dependencies)
config.yaml          Configuration file
wpal.xml        Service definition used by the MSI installer
wpal-service.exe Optional helper to register/unregister the Windows service

Quick start
-----------
1. Open an elevated (Administrator) command prompt.
2. Run the service directly for a quick test:

   wpal.exe --config "%ProgramData%\Wpal\config.yaml"

3. The dashboard is then available at:

   http://127.0.0.1:8891/

   The service listens on loopback only. To expose it on your network,
   put it behind a reverse proxy (nginx / IIS / Caddy) and terminate TLS there.

Register as a Windows service (optional)
----------------------------------------
   wpal-service.exe install
   wpal-service.exe start

   To remove it again:

   wpal-service.exe stop
   wpal-service.exe remove

Data directory
--------------
History is stored in %ProgramData%\Wpal\data as a bounded ring file,
so the disk usage never grows beyond the configured retention period.

Documentation
-------------
https://github.com/wpal/wpal
