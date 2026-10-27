/* ==========================================================================
   Wpal 极简主控台 · 交互与数据引擎
   ========================================================================== */
(() => {
  'use strict';

  // ---------------------------------------------------------------- 路由与辅助
  const BASE = (() => {
    const p = location.pathname;
    const i = p.lastIndexOf('/');
    return i > 0 ? p.slice(0, i) : '';
  })();
  const api = (p) => `${BASE}${p}`;
  const wsUrl = () =>
    `${location.protocol === 'https:' ? 'wss' : 'ws'}://${location.host}${BASE}/ws/stream`;

  const $ = (id) => document.getElementById(id);
  const t = (key, params) => (window.I18N ? window.I18N.t(key, params) : key);
  const nf = (v, d = 1) =>
    v === undefined || v === null || Number.isNaN(Number(v)) ? '—' : Number(v).toFixed(d);

  function bytes(b) {
    if (b === undefined || b === null) return '—';
    const u = ['B', 'KiB', 'MiB', 'GiB', 'TiB'];
    let i = 0; let n = Number(b);
    while (n >= 1024 && i < u.length - 1) { n /= 1024; i += 1; }
    return `${n.toFixed(i === 0 ? 0 : 1)} ${u[i]}`;
  }
  function rate(bps) {
    if (bps === undefined || bps === null) return '—';
    const u = ['B/s', 'KB/s', 'MB/s', 'GB/s'];
    let i = 0; let n = Number(bps);
    while (n >= 1024 && i < u.length - 1) { n /= 1024; i += 1; }
    return `${n.toFixed(i === 0 ? 0 : 1)} ${u[i]}`;
  }
  function compactRate(v) {
    const n = Number(v) || 0;
    if (n >= 1024 ** 3) return `${(n / 1024 ** 3).toFixed(1)}G`;
    if (n >= 1024 ** 2) return `${(n / 1024 ** 2).toFixed(1)}M`;
    if (n >= 1024) return `${(n / 1024).toFixed(0)}K`;
    return `${n.toFixed(0)}B`;
  }
  function dur(sec) {
    if (!sec && sec !== 0) return '—';
    const s = Math.floor(sec);
    const d = Math.floor(s / 86400);
    const h = Math.floor((s % 86400) / 3600);
    const m = Math.floor((s % 3600) / 60);
    return d > 0 ? `${d}d ${h}:${String(m).padStart(2, '0')}`
                 : `${h}:${String(m).padStart(2, '0')}:${String(s % 60).padStart(2, '0')}`;
  }

  const toDate = (v) => {
    if (v instanceof Date) return v;
    const n = Number(v);
    if (!Number.isFinite(n)) return new Date();
    return new Date(n < 1e12 ? n * 1000 : n);
  };
  const clockText = (d) => {
    const dt = toDate(d);
    return `${String(dt.getHours()).padStart(2, '0')}:${String(dt.getMinutes()).padStart(2, '0')}:${String(dt.getSeconds()).padStart(2, '0')}`;
  };
  const dateText = (ts) => {
    const d = toDate(ts);
    return `${String(d.getMonth() + 1).padStart(2, '0')}-${String(d.getDate()).padStart(2, '0')} ${clockText(d)}`;
  };

  // ---------------------------------------------------------------- 主题系统
  const THEME_KEY = 'wpal.theme';
  let currentTheme = localStorage.getItem(THEME_KEY) || 'light';
  document.documentElement.setAttribute('data-theme', currentTheme);

  function updateThemeUI() {
    const btn = $('theme-toggle');
    if (!btn) return;
    const icon = btn.querySelector('.theme-icon');
    if (icon) icon.textContent = currentTheme === 'light' ? '🌙' : '☀️';
  }

  function toggleTheme() {
    currentTheme = currentTheme === 'light' ? 'dark' : 'light';
    document.documentElement.setAttribute('data-theme', currentTheme);
    localStorage.setItem(THEME_KEY, currentTheme);
    updateThemeUI();
    updateChartTheme();
  }

  function getThemeColors() {
    const isDark = currentTheme === 'dark';
    return {
      text: isDark ? '#f5f5f7' : '#1d1d1f',
      muted: isDark ? '#a1a1a6' : '#86868b',
      grid: isDark ? 'rgba(255,255,255,0.06)' : 'rgba(0,0,0,0.045)',
      tooltipBg: isDark ? 'rgba(30,30,32,0.92)' : 'rgba(255,255,255,0.95)',
      tooltipBorder: isDark ? 'rgba(255,255,255,0.12)' : 'rgba(0,0,0,0.08)',
      cpu: isDark ? '#0a84ff' : '#007aff',
      cpuUser: isDark ? '#0a84ff' : '#007aff',
      cpuSys: isDark ? '#5e5ce6' : '#5856d6',
      mem: isDark ? '#bf5af2' : '#af52de',
      net: isDark ? '#ff9f0a' : '#ff9500',
      disk: isDark ? '#30d158' : '#34c759',
      warn: isDark ? '#ffd60a' : '#ffcc00',
      crit: isDark ? '#ff453a' : '#ff3b30',
      ok: isDark ? '#30d158' : '#34c759',
    };
  }

  // ---------------------------------------------------------------- 弹性数字过渡
  function animateNumber(el, to, decimals) {
    if (!el) return;
    const from = parseFloat(el.dataset.v ?? el.textContent);
    el.dataset.v = String(to);
    if (!Number.isFinite(from) || from === to) {
      el.textContent = Number(to).toFixed(decimals);
      return;
    }
    const t0 = performance.now();
    const dur = 350;
    const step = (now) => {
      const k = Math.min(1, (now - t0) / dur);
      const e = 1 - (1 - k) ** 3;
      el.textContent = (from + (to - from) * e).toFixed(decimals);
      if (k < 1) requestAnimationFrame(step);
    };
    requestAnimationFrame(step);
  }

  // ---------------------------------------------------------------- 2×2 四宫格全景监控系统
  // 四大核心维度（CPU / 内存 / 网络 / 磁盘）同屏同时呈现，无需切换标签页

  // ---------------------------------------------------------------- 图表引擎（ECharts）
  const charts = {};
  let ready = false;
  let timeFmt = (ts) => clockText(ts * 1000);

  function hexToRgba(hex, alpha = 1) {
    if (!hex || hex === 'transparent') return 'rgba(0,0,0,0)';
    if (hex.startsWith('rgba')) return hex;
    let c = hex.replace('#', '');
    if (c.length === 3) c = c.split('').map(x => x + x).join('');
    const num = parseInt(c, 16);
    if (isNaN(num)) return 'rgba(0,0,0,0)';
    return `rgba(${(num >> 16) & 255}, ${(num >> 8) & 255}, ${num & 255}, ${alpha})`;
  }

  const flatArea = (color, topAlpha = 0.35, bottomAlpha = 0.02) => ({
    color: {
      type: 'linear', x: 0, y: 0, x2: 0, y2: 1,
      colorStops: [
        { offset: 0, color: hexToRgba(color, topAlpha) },
        { offset: 1, color: hexToRgba(color, bottomAlpha) }
      ]
    },
    opacity: 1
  });

  function catAxis(times, fmt) {
    const n = times.length;
    const colors = getThemeColors();
    return {
      type: 'category',
      data: times.map(String),
      boundaryGap: false,
      axisLine: { show: false },
      axisTick: { show: false },
      axisLabel: {
        color: colors.muted,
        fontSize: 10,
        formatter: (v) => fmt(Number(v)),
        interval: n > 1 ? Math.max(0, Math.ceil(n / 6) - 1) : 0,
      },
      splitLine: { show: false },
    };
  }

  function makeTip(valFmt) {
    return (params) => {
      if (!params || !params.length) return '';
      const colors = getThemeColors();
      const ts = Number(params[0].axisValue);
      const head = `<div style="color:${colors.muted};font-size:11px;margin-bottom:6px;font-weight:600">${timeFmt(ts)}</div>`;
      return head + params.map((p) =>
        `<div style="display:flex;gap:12px;align-items:center;justify-content:space-between;margin:3px 0;">` +
        `<span>${p.marker} <span style="color:${colors.muted}">${p.seriesName}</span></span>` +
        `<b style="font-weight:700;color:${colors.text};font-family:var(--mono);">${valFmt(p.value)}</b></div>`
      ).join('');
    };
  }

  const GRID = { left: 44, right: 18, top: 16, bottom: 22 };

  function initCharts() {
    if (ready) return;
    ready = true;
    charts.cpu = echarts.init($('chart-cpu'));
    charts.mem = echarts.init($('chart-mem'));
    charts.net = echarts.init($('chart-net'));
    charts.disk = echarts.init($('chart-disk'));
    updateChartTheme();
  }

  function updateChartTheme() {
    if (!ready) return;
    const colors = getThemeColors();
    const base = {
      grid: GRID,
      animationDuration: 500,
      animationEasing: 'cubicOut',
      axisPointer: { type: 'line', lineStyle: { color: colors.muted, opacity: 0.3 } },
    };
    const tip = (valFmt) => ({
      trigger: 'axis',
      backgroundColor: colors.tooltipBg,
      borderColor: colors.tooltipBorder,
      borderWidth: 1,
      padding: [10, 14],
      textStyle: { color: colors.text, fontSize: 11.5 },
      extraCssText: 'box-shadow: 0 10px 25px -5px rgba(0,0,0,0.1); border-radius: 8px;',
      formatter: makeTip(valFmt),
    });

    charts.cpu.setOption({
      ...base,
      tooltip: tip((v) => `${nf(v, 1)}%`),
      xAxis: catAxis([], (ts) => clockText(ts * 1000)),
      yAxis: {
        type: 'value', min: 0,
        max: (v) => Math.min(100, Math.max(20, Math.ceil((v.max * 1.25) / 10) * 10)),
        axisLine: { show: false }, axisTick: { show: false },
        axisLabel: { color: colors.muted, fontSize: 10, formatter: '{value}%' },
        splitLine: { lineStyle: { color: colors.grid } }
      },
      series: [
        { name: t('lg.user'), type: 'line', stack: 'c', smooth: true, showSymbol: false, lineStyle: { width: 0 }, areaStyle: flatArea(colors.cpuUser, 0.45, 0.05), data: [] },
        { name: t('lg.system'), type: 'line', stack: 'c', smooth: true, showSymbol: false, lineStyle: { width: 0 }, areaStyle: flatArea(colors.cpuSys, 0.35, 0.05), data: [] },
        { name: 'iowait', type: 'line', stack: 'c', smooth: true, showSymbol: false, lineStyle: { width: 0 }, areaStyle: flatArea(colors.warn, 0.5, 0.05), data: [] },
        { name: 'steal', type: 'line', stack: 'c', smooth: true, showSymbol: false, lineStyle: { width: 0 }, areaStyle: flatArea('#8b5cf6', 0.5, 0.05), data: [] },
        { name: 'Total', type: 'line', smooth: true, showSymbol: false, lineStyle: { width: 2, color: colors.cpu }, data: [] },
      ],
    });

    charts.mem.setOption({
      ...base,
      tooltip: tip((v) => `${nf(v, 1)}%`),
      xAxis: catAxis([], (ts) => clockText(ts * 1000)),
      yAxis: {
        type: 'value', min: 0, max: 100,
        axisLine: { show: false }, axisTick: { show: false },
        axisLabel: { color: colors.muted, fontSize: 10, formatter: '{value}%' },
        splitLine: { lineStyle: { color: colors.grid } }
      },
      series: [
        { name: t('metric.memory'), type: 'line', smooth: true, showSymbol: false, lineStyle: { width: 2, color: colors.mem }, areaStyle: flatArea(colors.mem, 0.25, 0.02), data: [] },
        { name: 'Swap', type: 'line', smooth: true, showSymbol: false, lineStyle: { width: 1.6, color: colors.warn, type: 'dashed' }, areaStyle: flatArea(colors.warn, 0.1, 0.01), data: [] },
      ],
    });

    charts.net.setOption({
      ...base,
      tooltip: tip((v) => rate(v)),
      xAxis: catAxis([], (ts) => clockText(ts * 1000)),
      yAxis: {
        type: 'value', min: 0,
        axisLine: { show: false }, axisTick: { show: false },
        axisLabel: { color: colors.muted, fontSize: 10, formatter: (v) => compactRate(v) },
        splitLine: { lineStyle: { color: colors.grid } }
      },
      series: [
        { name: t('table.down'), type: 'line', smooth: true, showSymbol: false, lineStyle: { width: 2, color: colors.ok }, areaStyle: flatArea(colors.ok, 0.25, 0.02), data: [] },
        { name: t('table.up'), type: 'line', smooth: true, showSymbol: false, lineStyle: { width: 1.8, color: colors.net }, areaStyle: flatArea(colors.net, 0.18, 0.02), data: [] },
      ],
    });

    charts.disk.setOption({
      ...base,
      tooltip: tip((v) => rate(v)),
      xAxis: catAxis([], (ts) => clockText(ts * 1000)),
      yAxis: {
        type: 'value', min: 0,
        axisLine: { show: false }, axisTick: { show: false },
        axisLabel: { color: colors.muted, fontSize: 10, formatter: (v) => compactRate(v) },
        splitLine: { lineStyle: { color: colors.grid } }
      },
      series: [
        { name: t('lg.read'), type: 'line', smooth: true, showSymbol: false, lineStyle: { width: 2, color: colors.disk }, areaStyle: flatArea(colors.disk, 0.25, 0.02), data: [] },
        { name: t('lg.write'), type: 'line', smooth: true, showSymbol: false, lineStyle: { width: 1.8, color: colors.cpu }, areaStyle: flatArea(colors.cpu, 0.18, 0.02), data: [] },
      ],
    });
  }

  // ---------------------------------------------------------------- 触控岛 Sparkline
  function sparkPath(values, w, h, pad = 3) {
    if (!values || !values.length) return { line: '', area: '', last: null };
    const min = Math.min(...values);
    const max = Math.max(...values);
    const span = (max - min) || 1;
    const n = values.length;
    const pts = values.map((v, i) => [
      pad + (i / (n - 1 || 1)) * (w - pad * 2),
      h - pad - ((v - min) / span) * (h - pad * 2),
    ]);
    const line = pts.map((p, i) => `${i ? 'L' : 'M'}${p[0].toFixed(1)},${p[1].toFixed(1)}`).join('');
    const area = `${line}L${pts[n - 1][0].toFixed(1)},${h}L${pts[0][0].toFixed(1)},${h}Z`;
    return { line, area, last: pts[n - 1] };
  }

  function setSpark(id, values) {
    const svg = $(id);
    if (!svg) return;
    const line = svg.querySelector('.s-line');
    const area = svg.querySelector('.s-area');
    const dot = svg.querySelector('.s-dot');
    const { line: d, area: a, last } = sparkPath(values, 80, 24);
    if (line) line.setAttribute('d', d);
    if (area) area.setAttribute('d', a);
    if (dot && last) {
      dot.setAttribute('cx', last[0].toFixed(1));
      dot.setAttribute('cy', last[1].toFixed(1));
    }
  }

  // ---------------------------------------------------------------- 实时数据缓冲
  const MAX = 240;
  const live = {
    ts: [],
    cpu: { user: [], system: [], iowait: [], steal: [], total: [] },
    mem: [], swap: [], netRx: [], netTx: [], ioR: [], ioW: [],
    sCpu: [], sMem: [], sDisk: [], sNet: [],
  };
  const push = (a, v) => { a.push(v); if (a.length > MAX) a.shift(); };
  let lastFrameTs = 0;

  function pushLive(s) {
    const ts = s.ts;
    const same = Math.floor(lastFrameTs) === Math.floor(ts);
    lastFrameTs = ts;
    const put = (a, v) => {
      if (same && a.length) a[a.length - 1] = v;
      else push(a, v);
    };

    const cpu = s.cpu || {};
    const br = cpu.breakdown || {};
    put(live.ts, ts);
    put(live.cpu.user, br.user || 0);
    put(live.cpu.system, br.system || 0);
    put(live.cpu.iowait, br.iowait || 0);
    put(live.cpu.steal, br.steal || 0);
    put(live.cpu.total, cpu.total || 0);
    put(live.mem, (s.mem || {}).percent || 0);
    put(live.swap, (s.mem || {}).swap_percent || 0);

    const ifs = (s.net || {}).interfaces || [];
    const rx = ifs.reduce((a, i) => a + i.rx_bps, 0);
    const tx = ifs.reduce((a, i) => a + i.tx_bps, 0);
    put(live.netRx, rx);
    put(live.netTx, tx);

    const ios = (s.disk || {}).io || [];
    put(live.ioR, ios.reduce((a, d) => a + d.read_bps, 0));
    put(live.ioW, ios.reduce((a, d) => a + d.write_bps, 0));

    put(live.sCpu, cpu.total || 0);
    put(live.sMem, (s.mem || {}).percent || 0);
    const parts = (s.disk || {}).partitions || [];
    put(live.sDisk, parts.length ? Math.max(...parts.map((p) => p.percent)) : 0);
    put(live.sNet, (rx + tx) / 1024 / 1024);
  }

  function renderLiveCharts() {
    if (!ready) return;
    const ax = catAxis(live.ts, (ts) => clockText(ts * 1000));
    charts.cpu.setOption({ xAxis: ax, series: [
      { data: live.cpu.user }, { data: live.cpu.system }, { data: live.cpu.iowait },
      { data: live.cpu.steal }, { data: live.cpu.total },
    ] });
    charts.mem.setOption({ xAxis: ax, series: [{ data: live.mem }, { data: live.swap }] });
    charts.net.setOption({ xAxis: ax, series: [{ data: live.netRx }, { data: live.netTx }] });
    charts.disk.setOption({ xAxis: ax, series: [{ data: live.ioR }, { data: live.ioW }] });

    setSpark('spark-cpu', live.sCpu);
    setSpark('spark-mem', live.sMem);
    setSpark('spark-disk', live.sDisk);
    setSpark('spark-net', live.sNet);
  }

  // ---------------------------------------------------------------- 历史序列回看
  let mode = 'live';
  const RANGE_LABEL = () => ({
    '5m': t('nav.5m'), '1h': t('nav.1h'), '6h': t('nav.6h'),
    '24h': t('nav.24h'), '7d': t('nav.7d'),
  });

  async function loadHistory(range) {
    mode = range;
    timeFmt = (ts) => dateText(ts);
    const h = await (await fetch(api(`/api/history?range=${range}`))).json();
    if (!ready) return;
    const ts = h.ts || [];
    const brk = h.cpu_brk || [];
    const ax = catAxis(ts, (v) => dateText(v));

    const pick = (key) => ts.map((_, i) => (brk[i] || {})[key] || 0);
    charts.cpu.setOption({
      xAxis: ax,
      series: [
        { data: pick('user') }, { data: pick('system') }, { data: pick('iowait') },
        { data: pick('steal') }, { data: h.cpu || [] },
      ],
    });
    charts.mem.setOption({ xAxis: ax, series: [{ data: h.mem || [] }, { data: h.swap || [] }] });

    const netRx = [], netTx = [];
    (h.net || []).forEach((m) => {
      let rx = 0; let tx = 0;
      Object.values(m || {}).forEach((v) => { rx += v[0] || 0; tx += v[1] || 0; });
      netRx.push(rx); netTx.push(tx);
    });
    charts.net.setOption({ xAxis: ax, series: [{ data: netRx }, { data: netTx }] });

    const ioR = [], ioW = [];
    (h.diskio || []).forEach((m) => {
      let r = 0; let w = 0;
      Object.values(m || {}).forEach((v) => { r += v[0] || 0; w += v[1] || 0; });
      ioR.push(r); ioW.push(w);
    });
    charts.disk.setOption({ xAxis: ax, series: [{ data: ioR }, { data: ioW }] });

    const label = t('hint.history', { range: RANGE_LABEL()[range], n: h.count || 0 });
    ['cpu', 'mem', 'net', 'disk'].forEach(k => {
      const hint = $(`${k}-hint`);
      if (hint) hint.textContent = label;
    });
  }

  function backToLive() {
    mode = 'live';
    timeFmt = (ts) => clockText(ts * 1000);
    const ax = catAxis(live.ts, (ts) => clockText(ts * 1000));
    ['cpu', 'mem', 'net', 'disk'].forEach((k) => charts[k].setOption({ xAxis: ax }));
    ['mem', 'net', 'disk'].forEach((k) => {
      const el = $(`${k}-hint`);
      if (el) el.textContent = t('hint.live');
    });
  }

  // ---------------------------------------------------------------- 渲染各区域
  const rowHTML = (k, pct, color, v) =>
    `<div class="row"><span class="r-k">${k}</span><span class="r-bar"><i style="width:${Math.min(100, Math.max(0, pct))}%;background:${color}"></i></span><span class="r-v mono">${v}</span></div>`;

  function renderTop(s) {
    const host = s.host || {};
    const name = host.label || host.hostname || '—';
    if ($('host-name')) $('host-name').textContent = name;
    if ($('window-title-host')) $('window-title-host').textContent = name;
    if ($('uptime')) $('uptime').textContent = dur(host.uptime);
  }

  function renderHero(s) {
    const cpu = s.cpu || {};
    const mem = s.mem || {};
    const parts = (s.disk || {}).partitions || [];
    const ifs = (s.net || {}).interfaces || [];

    // CPU 象限
    const cpuPct = Math.min(100, Math.max(0, cpu.total || 0));
    animateNumber($('cpu-val'), cpuPct, 0);
    if ($('cpu-sub')) {
      $('cpu-sub').textContent = t('quad.cpu.sub', {
        l1: nf(cpu.load1, 2), l5: nf(cpu.load5, 2), cores: cpu.cores || 0,
      });
    }

    // 内存 象限
    const memPct = Math.min(100, Math.max(0, mem.percent || 0));
    animateNumber($('mem-val'), memPct, 0);
    if ($('mem-sub')) $('mem-sub').textContent = `${bytes(mem.used)} / ${bytes(mem.total)}`;

    // 磁盘 象限
    const worst = parts.length ? parts.reduce((a, b) => (b.percent > a.percent ? b : a)) : null;
    const diskPct = worst ? Math.min(100, Math.max(0, worst.percent)) : 0;
    animateNumber($('disk-val'), diskPct, 0);
    if ($('disk-sub')) {
      $('disk-sub').textContent = worst
        ? t('quad.disk.sub', { mount: worst.mount, free: bytes(worst.free) })
        : '—';
    }

    // 网络 象限
    const rx = ifs.reduce((a, i) => a + i.rx_bps, 0);
    const tx = ifs.reduce((a, i) => a + i.tx_bps, 0);
    animateNumber($('net-val'), (rx + tx) / 1024 / 1024, 2);
    if ($('net-sub')) $('net-sub').textContent = `↓ ${compactRate(rx)}  ↑ ${compactRate(tx)}`;

    // 状态判定胶囊
    const active = s.active_alerts || [];
    const crit = active.filter((a) => a.level === 'critical');
    const warn = active.filter((a) => a.level === 'warning');
    const statusCap = $('status');
    if (statusCap) {
      statusCap.classList.remove('warn', 'crit');
      const txt = $('v-text');
      const sub = $('v-sub');
      if (crit.length) {
        statusCap.classList.add('crit');
        if (txt) txt.textContent = t('verdict.crit', { n: crit.length });
        if (sub) sub.textContent = alertText(crit[0]);
      } else if (warn.length) {
        statusCap.classList.add('warn');
        if (txt) txt.textContent = t('verdict.warn', { n: warn.length });
        if (sub) sub.textContent = alertText(warn[0]);
      } else {
        if (txt) txt.textContent = t('verdict.ok');
        if (sub) sub.textContent = t('verdict.ok.sub');
      }
    }
  }

  function alertText(a) {
    const label = a.label_key ? t(a.label_key) : a.label;
    const detail = a.detail_key ? t(a.detail_key, a.detail_params) : a.detail;
    return detail ? `${label} · ${detail}` : label;
  }

  function renderCores(s) {
    const cores = (s.cpu || {}).percore || [];
    const box = $('cores');
    if (!box) return;
    if (box.childElementCount !== cores.length) {
      box.innerHTML = cores.map((_, i) =>
        `<div class="core"><div class="core-h"><span>CPU${i}</span><b>—</b></div><div class="core-bar"><i style="width:0%"></i></div></div>`
      ).join('');
    }
    cores.forEach((c, i) => {
      const el = box.children[i];
      if (!el) return;
      el.querySelector('b').textContent = `${nf(c.busy, 0)}%`;
      const bar = el.querySelector('.core-bar i');
      bar.style.width = `${Math.min(100, Math.max(0, c.busy))}%`;
    });
    const note = $('cores-note');
    if (note) note.textContent = t('cores.steal', { v: nf((s.cpu || {}).breakdown?.steal, 1) });
    const hint = $('cpu-hint');
    if (hint) {
      hint.textContent = t('cores.load', {
        load1: nf((s.cpu || {}).load1, 2), cores: (s.cpu || {}).cores || 0,
      });
    }
  }

  function renderMemRows(s) {
    const m = s.mem || {};
    const box = $('mem-rows');
    if (!box) return;
    box.innerHTML = [
      rowHTML(t('row.used'), m.percent || 0, 'var(--mem)', bytes(m.used)),
      rowHTML(t('row.avail'), 100 - (m.percent || 0), 'var(--ok)', bytes(m.available)),
      rowHTML(t('row.cached'), 0, 'transparent', bytes((m.cached || 0) + (m.buffers || 0))),
      rowHTML(t('row.swap'), m.swap_percent || 0, 'var(--warn)', bytes(m.swap_used)),
    ].join('');
  }

  function renderNetRows(s) {
    const ifs = (s.net || {}).interfaces || [];
    const box = $('net-rows');
    if (!box) return;
    box.innerHTML = ifs.map((i) => {
      const tot = i.rx_bps + i.tx_bps || 1;
      return `<div class="row"><span class="r-k">${i.name}</span>` +
        `<span class="r-bar"><i style="width:${(i.rx_bps / tot) * 100}%;background:var(--ok)"></i><i style="width:${(i.tx_bps / tot) * 100}%;background:var(--net)"></i></span>` +
        `<span class="r-v mono">↓${compactRate(i.rx_bps)} ↑${compactRate(i.tx_bps)}</span></div>`;
    }).join('') || `<div class="clear-notice">${t('empty.data')}</div>`;
    
    const tcp = (s.net || {}).tcp || {};
    const total = Object.values(tcp).reduce((a, b) => a + b, 0);
    if (mode === 'live' && $('net-hint')) $('net-hint').textContent = t('hint.connections', { n: total });
  }

  function renderDiskRows(s) {
    const parts = (s.disk || {}).partitions || [];
    const box = $('disk-rows');
    if (!box) return;
    box.innerHTML = parts.map((p) =>
      rowHTML(p.mount, p.percent, 'var(--disk)', t('quad.disk.row', { pct: nf(p.percent, 1), free: bytes(p.free) }))
    ).join('') || `<div class="clear-notice">${t('empty.data')}</div>`;
    const ios = (s.disk || {}).io || [];
    const top = ios[0];
    if (mode === 'live' && $('disk-hint')) {
      $('disk-hint').textContent = top
        ? t('hint.diskwrite', { dev: top.name, rate: rate(top.write_bps) }) : t('hint.live');
    }
  }

  // ---------------------------------------------------------------- iStat Menus 极简原生资源消耗大户呈现
  let procSort = 'cpu';
  let procSearchQuery = '';

  function getProcIcon(name, cmd) {
    const s = `${name || ''} ${cmd || ''}`.toLowerCase();
    if (s.includes('docker') || s.includes('containerd') || s.includes('runc')) return '🐳';
    if (s.includes('python') || s.includes('gunicorn') || s.includes('uvicorn') || s.includes('celery')) return '🐍';
    if (s.includes('node') || s.includes('npm') || s.includes('vite') || s.includes('next') || s.includes('bun')) return '🌐';
    if (s.includes('nginx') || s.includes('caddy') || s.includes('traefik') || s.includes('apache')) return '🔀';
    if (s.includes('redis') || s.includes('memcached')) return '🔺';
    if (s.includes('postgres') || s.includes('mysql') || s.includes('mariadb') || s.includes('mongod')) return '💾';
    if (s.includes('wpal') || s.includes('sub2api') || s.includes('minimax')) return '⚡';
    if (s.includes('java') || s.includes('spring') || s.includes('jar')) return '☕';
    if (s.includes('systemd') || s.includes('journald') || s.includes('sshd') || s.includes('bash') || s.includes('zsh')) return '⚙️';
    return '📦';
  }

  function renderProcs(s) {
    const box = $('proc-rows');
    if (!box) return;
    const list = (procSort === 'mem' ? s.procs_mem : s.procs) || [];
    
    let filtered = list;
    if (procSearchQuery) {
      const q = procSearchQuery.toLowerCase();
      filtered = list.filter(p => 
        (p.name && p.name.toLowerCase().includes(q)) ||
        String(p.pid).includes(q) ||
        (p.cmd && p.cmd.toLowerCase().includes(q))
      );
    }
    const top = filtered.slice(0, 8);

    box.className = 'istat-consumers';

    if (!top.length) {
      box.innerHTML = `<div class="clear-notice">${t('empty.data')}</div>`;
      return;
    }

    box.innerHTML = top.map((p) => {
      const icon = getProcIcon(p.name, p.cmd);
      const cpuVal = p.cpu || 0;
      const memPct = p.mem_pct || 0;
      const isHot = cpuVal >= 50;
      const isMemMode = procSort === 'mem';

      const fillPct = isMemMode 
        ? Math.min(100, Math.max(2, memPct * 3))
        : Math.min(100, Math.max(2, cpuVal));

      const valText = isMemMode ? bytes(p.rss) : `${nf(cpuVal, 1)}%`;
      const cleanCmd = (p.cmd || p.name || '—').replace(/"/g, '&quot;');

      return (
        `<div class="istat-row prow" title="${cleanCmd}">` +
          `<div class="istat-left">` +
            `<div class="istat-app-icon">${icon}</div>` +
            `<div class="istat-info">` +
              `<div class="istat-name-line">` +
                `<span class="istat-name">${p.name}</span>` +
                `<span class="istat-pid mono">#${p.pid}</span>` +
              `</div>` +
              `<span class="istat-cmd-sub mono">${cleanCmd}</span>` +
            `</div>` +
          `</div>` +
          `<div class="istat-right">` +
            `<div class="istat-meter-track">` +
              `<i class="istat-meter-fill ${isMemMode ? 'mem' : (isHot ? 'hot' : '')}" style="width:${fillPct}%"></i>` +
            `</div>` +
            `<span class="istat-val mono ${!isMemMode && isHot ? 'hot' : ''}">${valText}</span>` +
          `</div>` +
        `</div>`
      );
    }).join('');
  }

  // ---------------------------------------------------------------- 告警与事件
  function renderAlerts(s) {
    const active = s.active_alerts || [];
    const box = $('alert-list');
    if (!box) return;
    box.innerHTML = active.length
      ? active.map((a) => `<div class="chip ${a.level}"><span>${a.label_key ? t(a.label_key) : a.label}</span><b>${nf(a.value, 1)}${a.unit || '%'}</b></div>`).join('')
      : `<div class="clear-notice">${t('alerts.none')}</div>`;
    
    const countBadge = $('event-count');
    if (countBadge) {
      if (active.length) {
        countBadge.textContent = String(active.length);
        countBadge.classList.add('has-alert');
      } else {
        countBadge.classList.remove('has-alert');
      }
    }
  }

  let eventsAll = [];
  let eventsExpanded = false;

  function evHTML(ev) {
    const resolved = ev.status === 'resolved';
    const label = ev.label_key ? t(ev.label_key) : ev.label;
    const detail = ev.detail_key ? t(ev.detail_key, ev.detail_params) : '';
    return `<div class="ev ${resolved ? 'resolved' : ev.level}">` +
      `<span class="ev-t">${label}<b>${nf(ev.value, 1)}${ev.unit || '%'}</b></span>` +
      `<span class="ev-m">${dateText(ev.ts)}</span></div>`;
  }

  function renderEvents() {
    const list = $('event-list');
    if (!list) return;
    const items = eventsExpanded ? eventsAll : eventsAll.slice(0, 5);
    list.innerHTML = items.map(evHTML).join('') || `<div class="clear-notice">${t('alerts.none')}</div>`;
    const btn = $('event-toggle');
    if (btn) {
      btn.style.display = eventsAll.length > 5 ? '' : 'none';
      btn.textContent = eventsExpanded ? t('events.collapse') : t('events.expand');
    }
  }

  function addEvent(ev) {
    eventsAll.unshift(ev);
    if (eventsAll.length > 50) eventsAll.pop();
    renderEvents();
  }

  function toast(ev) {
    const resolved = ev.status === 'resolved';
    const el = document.createElement('div');
    el.className = `toast ${resolved ? 'resolved' : ev.level}`;
    const label = ev.label_key ? t(ev.label_key) : ev.label;
    const kind = resolved ? t('toast.resolved')
      : (ev.level === 'critical' ? t('toast.critical') : t('toast.warning'));
    el.innerHTML =
      `<div class="t1">${kind} · ${label}</div>` +
      `<div class="t2">${nf(ev.value, 1)}${ev.unit || '%'} · ${dateText(ev.ts)}</div>`;
    const box = $('toasts');
    if (box) {
      box.appendChild(el);
      setTimeout(() => {
        el.style.transition = 'opacity .3s, transform .3s';
        el.style.opacity = '0';
        el.style.transform = 'translateY(12px)';
        setTimeout(() => el.remove(), 320);
      }, 5000);
    }
  }

  // ---------------------------------------------------------------- 多语言与静态渲染
  function applyStaticText() {
    document.querySelectorAll('[data-i18n]').forEach((el) => {
      el.textContent = t(el.dataset.i18n);
    });
    document.querySelectorAll('[data-i18n-ph]').forEach((el) => {
      el.setAttribute('placeholder', t(el.dataset.i18nPh));
    });
    document.querySelectorAll('[data-i18n-title]').forEach((el) => {
      el.setAttribute('title', t(el.dataset.i18nTitle));
    });
    document.title = t('app.title');
    const langLabel = $('lang-label');
    if (langLabel && window.I18N) {
      langLabel.textContent = window.I18N.lang === 'zh-CN' ? '中' : 'EN';
    }
    updateThemeUI();
  }

  // ---------------------------------------------------------------- 总体流水线
  let last = null;

  function apply(s) {
    if (!s) return;
    last = s;
    renderTop(s);
    renderHero(s);
    renderCores(s);
    renderMemRows(s);
    renderNetRows(s);
    renderDiskRows(s);
    renderProcs(s);
    renderAlerts(s);
    if (mode === 'live') {
      pushLive(s);
      renderLiveCharts();
    }
    (s.events || []).forEach((ev) => { addEvent(ev); toast(ev); });
  }

  function setConn(on) {
    const dot = $('dot');
    if (dot) dot.classList.toggle('off', !on);
  }

  let ws = null;
  let retry = 0;
  function connect() {
    try { ws = new WebSocket(wsUrl()); } catch (e) { setConn(false); return; }
    ws.onopen = () => { retry = 0; setConn(true); };
    ws.onmessage = (e) => {
      try {
        const d = JSON.parse(e.data);
        if (d.type === 'error') { setConn(false); return; }
        setConn(true);
        apply(d);
      } catch (err) { /* ignore */ }
    };
    ws.onclose = () => {
      setConn(false);
      retry += 1;
      setTimeout(connect, Math.min(8000, 800 * retry));
    };
    ws.onerror = () => { try { ws.close(); } catch (e) { /* noop */ } };
  }

  async function bootstrap() {
    try {
      apply(await (await fetch(api('/api/snapshot'))).json());
    } catch (e) {
      console.error('[wpal] 快照加载失败:', e);
    }
    try {
      const d = await (await fetch(api('/api/events?limit=40'))).json();
      eventsAll = (d.events || []).slice();
      renderEvents();
      if ($('event-count')) $('event-count').textContent = String(d.total || (d.events || []).length || 0);
    } catch (e) { /* ignore */ }
    connect();
  }

  // ---------------------------------------------------------------- 交互事件绑定
  // 1. 时间跨度切换
  const ranges = $('ranges');
  if (ranges) {
    ranges.addEventListener('click', (e) => {
      const btn = e.target.closest('button[data-range]');
      if (!btn) return;
      ranges.querySelectorAll('button').forEach((b) => b.classList.toggle('on', b === btn));
      const r = btn.dataset.range;
      if (r === 'live') backToLive(); else loadHistory(r);
    });
  }

  // 4. 进程视图切换、排序与过滤
  const procViewModeEl = $('proc-view-mode');
  if (procViewModeEl) {
    procViewModeEl.addEventListener('click', (e) => {
      const btn = e.target.closest('button[data-view]');
      if (!btn) return;
      procViewModeEl.querySelectorAll('button').forEach((b) => b.classList.toggle('on', b === btn));
      procViewMode = btn.dataset.view;
      if (last) renderProcs(last);
    });
  }

  const procSortEl = $('proc-sort');
  if (procSortEl) {
    procSortEl.addEventListener('click', (e) => {
      const btn = e.target.closest('button[data-sort]');
      if (!btn) return;
      procSortEl.querySelectorAll('button').forEach((b) => b.classList.toggle('on', b === btn));
      procSort = btn.dataset.sort;
      if (last) renderProcs(last);
    });
  }

  const procFilterInput = $('proc-filter');
  if (procFilterInput) {
    procFilterInput.addEventListener('input', (e) => {
      procSearchQuery = (e.target.value || '').trim();
      if (last) renderProcs(last);
    });
  }

  // 5. 抽屉系统控制器 (Slide-over Drawers: 进程抽屉 + 事件抽屉)
  const procDrawer = $('proc-drawer');
  const eventDrawer = $('event-drawer');
  const drawerBackdrop = $('drawer-backdrop');

  const procToggleBtn = $('proc-drawer-toggle');
  const procCloseBtn = $('drawer-close');

  const eventToggleBtn = $('event-drawer-toggle');
  const eventCloseBtn = $('event-drawer-close');
  const statusCapsule = $('status');

  function closeAllDrawers() {
    if (procDrawer) { procDrawer.classList.remove('open'); procDrawer.setAttribute('aria-hidden', 'true'); }
    if (eventDrawer) { eventDrawer.classList.remove('open'); eventDrawer.setAttribute('aria-hidden', 'true'); }
    if (drawerBackdrop) { drawerBackdrop.classList.remove('open'); }
  }

  function openProcDrawer() {
    closeAllDrawers();
    if (!procDrawer || !drawerBackdrop) return;
    procDrawer.classList.add('open');
    drawerBackdrop.classList.add('open');
    procDrawer.setAttribute('aria-hidden', 'false');
    if (last) renderProcs(last);
  }

  function openEventDrawer() {
    closeAllDrawers();
    if (!eventDrawer || !drawerBackdrop) return;
    eventDrawer.classList.add('open');
    drawerBackdrop.classList.add('open');
    eventDrawer.setAttribute('aria-hidden', 'false');
    if (last) renderAlerts(last);
    renderEvents();
  }

  if (procToggleBtn) procToggleBtn.addEventListener('click', openProcDrawer);
  if (procCloseBtn) procCloseBtn.addEventListener('click', closeAllDrawers);

  if (eventToggleBtn) eventToggleBtn.addEventListener('click', openEventDrawer);
  if (statusCapsule) statusCapsule.addEventListener('click', openEventDrawer);
  if (eventCloseBtn) eventCloseBtn.addEventListener('click', closeAllDrawers);

  if (drawerBackdrop) drawerBackdrop.addEventListener('click', closeAllDrawers);

  window.addEventListener('keydown', (e) => {
    if (e.key === 'Escape') closeAllDrawers();
  });

  // 6. 展开历史
  const eventToggle = $('event-toggle');
  if (eventToggle) {
    eventToggle.addEventListener('click', () => {
      eventsExpanded = !eventsExpanded;
      renderEvents();
    });
  }

  // 7. 语言与主题切换
  const langToggle = $('lang-toggle');
  if (langToggle) {
    langToggle.addEventListener('click', async () => {
      if (!window.I18N) return;
      await window.I18N.toggle();
    });
  }

  const themeToggle = $('theme-toggle');
  if (themeToggle) {
    themeToggle.addEventListener('click', () => {
      toggleTheme();
    });
  }

  // 6.1 macOS 窗口原生级微交互
  const macWin = document.querySelector('.macos-window');
  const btnClose = document.querySelector('.tl-btn.close');
  const btnMin = document.querySelector('.tl-btn.minimize');
  const btnMax = document.querySelector('.tl-btn.zoom');
  const winHeader = document.querySelector('.macos-titlebar');

  if (btnMax) {
    btnMax.addEventListener('click', () => {
      if (document.fullscreenElement) {
        document.exitFullscreen().catch(() => {});
      } else {
        document.documentElement.requestFullscreen().catch(() => {
          if (macWin) macWin.classList.toggle('is-fullscreen');
        });
      }
    });
  }

  if (winHeader) {
    winHeader.addEventListener('dblclick', (e) => {
      if (e.target.closest('button, input, select, .theme-toggle, .lang-toggle')) return;
      if (macWin) macWin.classList.toggle('is-fullscreen');
    });
  }

  if (btnMin) {
    btnMin.addEventListener('click', () => {
      if (!macWin) return;
      macWin.style.transition = 'transform 0.25s cubic-bezier(0.16, 1, 0.3, 1)';
      macWin.style.transform = 'scale(0.97)';
      setTimeout(() => {
        macWin.style.transform = '';
      }, 250);
    });
  }

  if (btnClose) {
    btnClose.addEventListener('click', () => {
      const hint = $('window-title-host');
      if (!hint) return;
      const original = hint.textContent;
      hint.textContent = '● Daemon Active (Running in Background)';
      hint.style.color = 'var(--mac-green)';
      setTimeout(() => {
        hint.textContent = original;
        hint.style.color = '';
      }, 2400);
    });
  }

  // 7. 时钟
  setInterval(() => {
    const c = $('clock');
    if (c) c.textContent = clockText(new Date());
  }, 1000);
  if ($('clock')) $('clock').textContent = clockText(new Date());

  let rt = null;
  window.addEventListener('resize', () => {
    clearTimeout(rt);
    rt = setTimeout(() => Object.values(charts).forEach((c) => c && c.resize && c.resize()), 150);
  });

  async function start() {
    updateThemeUI();
    if (window.I18N) {
      await window.I18N.init();
      window.I18N.onChange(() => {
        applyStaticText();
        updateChartTheme();
        if (last) apply(last);
        renderEvents();
        if (mode === 'live') backToLive(); else loadHistory(mode);
      });
    }
    applyStaticText();
    initCharts();
    bootstrap();
  }

  start();
})();
