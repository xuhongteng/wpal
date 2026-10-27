from playwright.sync_api import sync_playwright
import time

OUT = "/litesight/docs/images"

with sync_playwright() as p:
    browser = p.chromium.launch(args=["--no-sandbox", "--disable-dev-shm-usage"])
    ctx = browser.new_context(viewport={"width": 1600, "height": 1000}, device_scale_factor=1)
    page = ctx.new_page()

    # 清空本地偏好，确保从默认状态开始
    page.goto("http://127.0.0.1:8891/", wait_until="domcontentloaded")
    page.evaluate("() => { localStorage.clear(); }")
    page.reload(wait_until="networkidle")
    time.sleep(3.0)

    # 1) 浅色实时视图
    page.screenshot(path=f"{OUT}/dashboard-light.png", full_page=True)
    print("OK dashboard-light.png")

    # 2) 暗色实时视图
    page.click("#theme-toggle")
    time.sleep(2.0)
    page.screenshot(path=f"{OUT}/dashboard-dark.png", full_page=True)
    print("OK dashboard-dark.png")
    # 切回浅色
    page.click("#theme-toggle")
    time.sleep(1.5)

    # 3) 英文界面
    page.click("#lang-toggle")
    time.sleep(2.0)
    page.screenshot(path=f"{OUT}/dashboard-en.png", full_page=True)
    print("OK dashboard-en.png")
    # 切回中文
    page.click("#lang-toggle")
    time.sleep(1.5)

    # 4) 历史回放视图（1h）
    page.click('button[data-range="1h"]')
    time.sleep(2.5)
    page.screenshot(path=f"{OUT}/dashboard-history.png", full_page=True)
    print("OK dashboard-history.png")

    browser.close()
