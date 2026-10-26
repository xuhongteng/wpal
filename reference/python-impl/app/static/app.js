/* 服务器资源监控 · 前端逻辑（原生 ESM，无构建） */
(() => {
  'use strict';

  // ---------------------------------------------------------------- 基础
  const BASE = (() => {
    const p = location.pathname;
    const i = p.lastIndexOf('/');
    return i > 0 ? p.slice(0, i) : '';
  })();
  const api = (p) => `${BASE}${p}`;
  const wsUrl = () =>
    `${location.protocol === 'https:' ? 'wss' : 'ws'}://${location.host}${BASE}/ws/stream`;

  const $ = (id) => document.getElementById(id);
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
    return d > 0 ? `${d}天 ${h}:${String(m).padStart(2, '0')}`
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

  // 配色收敛：蓝青为中性主体，琥珀只给"需要注意"的指标，紫色只给 steal
  const C = {
    deep: '#1d4ed8', blue: '#3b82f6', soft: '#60a5fa', cyan: '#22d3ee',
    amber: '#f59e0b', violet: '#a78bfa', gray: '#64748b', muted: '#5c636f',
  };
  const levelVar = (v) => (v < 60 ? 'var(--accent)' : v < 85 ? 'var(--warn)' : 'var(--crit)');

  // ---------------------------------------------------------------- 动效
  /** 数值滚动：旧值 → 新值做缓动插值，避免数字硬跳。 */
  function animateNumber(el, to, decimals) {
    if (!el) return;
    const from = parseFloat(el.dataset.v ?? el.textContent);
    el.dataset.v = String(to);
    if (!Number.isFinite(from) || from === to) {
      el.textContent = Number(to).toFixed(decimals);
      return;
    }
    const t0 = performance.now();
    const dur = 480;
    const step = (now) => {
      const t = Math.min(1, (now - t0) / dur);
      const e = 1 - (1 - t) ** 3;
      el.textContent = (from + (to - from) * e).toFixed(decimals);
      if (t < 1) requestAnimationFrame(step);
    };
    requestAnimationFrame(step);
  }

  /** 数值变化时的一次性高亮，让眼睛能捕捉到"这个数动了"。 */
  function flash(el) {
    if (!el) return;
    el.classList.remove('flash');
    void el.offsetWidth;
    el.classList.add('flash');
  }

  // ---------------------------------------------------------------- 图表
  const charts = {};
  let ready = false;
  let timeFmt = (t) => clockText(t * 1000);

  const AXIS_LABEL = { color: C.muted, fontSize: 10 };

  /** 分类轴：手动控制刻度间隔，彻底避免时间标签重叠。 */
  function catAxis(times, fmt) {
    const n = times.length;
    return {
      type: 'category',
      data: times.map(String),
      boundaryGap: false,
      axisLine: { show: false },
      axisTick: { show: false },
      axisLabel: {
        ...AXIS_LABEL,
        formatter: (v) => fmt(Number(v)),
        // 最多 7 个时间标签，间隔 = ceil(n/7)，点少时全显示、点多时稀疏
        interval: n > 1 ? Math.max(0, Math.ceil(n / 7) - 1) : 0,
      },
      splitLine: { show: false },
    };
  }

  function makeTip(valFmt) {
    return (params) => {
      if (!params || !params.length) return '';
      const t = Number(params[0].axisValue);
      const head = `<div style="color:#8b929e;font-size:10.5px;margin-bottom:5px">${timeFmt(t)}</div>`;
      return head + params.map((p) =>
        `<div style="display:flex;gap:10px;align-items:center;justify-content:space-between">` +
        `<span>${p.marker}${p.seriesName}</span><b style="font-weight:600;color:#e9ecf1">${valFmt(p.value)}</b></div>`
      ).join('');
    };
  }

  const flat = (color, alpha = 0.16) => ({ color, opacity: alpha });
  // right 留 30px：分类轴 boundaryGap=false 时末端标签居中于右端点，需要空间完整显示
  const GRID = { left: 52, right: 30, top: 12, bottom: 22 };

  function initCharts() {
    ready = true;
    charts.cpu = echarts.init($('chart-cpu'));
    charts.mem = echarts.init($('chart-mem'));
    charts.net = echarts.init($('chart-net'));
    charts.disk = echarts.init($('chart-disk'));

    const base = {
      grid: GRID,
      animationDuration: 700,
      animationEasing: 'cubicOut',
      axisPointer: { type: 'line', lineStyle: { color: 'rgba(255,255,255,0.12)' } },
    };

    charts.cpu.setOption({
      ...base,
      tooltip: { trigger: 'axis', backgroundColor: 'rgba(18,20,26,0.96)', borderColor: 'rgba(255,255,255,0.08)', borderWidth: 1, padding: [8, 11], textStyle: { color: '#dfe3e9', fontSize: 11 }, formatter: makeTip((v) => `${nf(v, 1)}%`) },
      xAxis: catAxis([], clockText),
      yAxis: { type: 'value', min: 0, max: (v) => Math.min(100, Math.max(20, Math.ceil((v.max * 1.25) / 10) * 10)), axisLine: { show: false }, axisTick: { show: false }, axisLabel: { ...AXIS_LABEL, formatter: '{value}%' }, splitLine: { lineStyle: { color: 'rgba(255,255,255,0.045)' } } },
      series: [
        { name: '用户', type: 'line', stack: 'c', smooth: true, showSymbol: false, lineStyle: { width: 0 }, areaStyle: flat(C.deep, 0.62), data: [] },
        { name: '系统', type: 'line', stack: 'c', smooth: true, showSymbol: false, lineStyle: { width: 0 }, areaStyle: flat(C.blue, 0.52), data: [] },
        { name: 'iowait', type: 'line', stack: 'c', smooth: true, showSymbol: false, lineStyle: { width: 0 }, areaStyle: flat(C.amber, 0.62), data: [] },
        { name: 'steal', type: 'line', stack: 'c', smooth: true, showSymbol: false, lineStyle: { width: 0 }, areaStyle: flat(C.violet, 0.62), data: [] },
        { name: '总占用', type: 'line', smooth: true, showSymbol: false, lineStyle: { width: 1.4, color: '#e9ecf1' }, data: [] },
      ],
    });

    charts.mem.setOption({
      ...base,
      tooltip: { trigger: 'axis', backgroundColor: 'rgba(18,20,26,0.96)', borderColor: 'rgba(255,255,255,0.08)', borderWidth: 1, padding: [8, 11], textStyle: { color: '#dfe3e9', fontSize: 11 }, formatter: makeTip((v) => `${nf(v, 1)}%`) },
      xAxis: catAxis([], clockText),
      yAxis: { type: 'value', min: 0, max: 100, axisLine: { show: false }, axisTick: { show: false }, axisLabel: { ...AXIS_LABEL, formatter: '{value}%' }, splitLine: { lineStyle: { color: 'rgba(255,255,255,0.045)' } } },
      series: [
        { name: '内存', type: 'line', smooth: true, showSymbol: false, lineStyle: { width: 1.5, color: C.blue }, areaStyle: flat(C.blue, 0.26), data: [] },
        { name: 'Swap', type: 'line', smooth: true, showSymbol: false, lineStyle: { width: 1.4, color: C.amber }, areaStyle: flat(C.amber, 0.1), data: [] },
      ],
    });

    charts.net.setOption({
      ...base,
      tooltip: { trigger: 'axis', backgroundColor: 'rgba(18,20,26,0.96)', borderColor: 'rgba(255,255,255,0.08)', borderWidth: 1, padding: [8, 11], textStyle: { color: '#dfe3e9', fontSize: 11 }, formatter: makeTip((v) => rate(v)) },
      xAxis: catAxis([], clockText),
      yAxis: { type: 'value', min: 0, axisLine: { show: false }, axisTick: { show: false }, axisLabel: { ...AXIS_LABEL, formatter: (v) => compactRate(v) }, splitLine: { lineStyle: { color: 'rgba(255,255,255,0.045)' } } },
      series: [
        { name: '下行', type: 'line', smooth: true, showSymbol: false, lineStyle: { width: 1.5, color: C.cyan }, areaStyle: flat(C.cyan, 0.2), data: [] },
        { name: '上行', type: 'line', smooth: true, showSymbol: false, lineStyle: { width: 1.4, color: C.gray }, areaStyle: flat(C.gray, 0.14), data: [] },
      ],
    });

    charts.disk.setOption({
      ...base,
      tooltip: { trigger: 'axis', backgroundColor: 'rgba(18,20,26,0.96)', borderColor: 'rgba(255,255,255,0.08)', borderWidth: 1, padding: [8, 11], textStyle: { color: '#dfe3e9', fontSize: 11 }, formatter: makeTip((v) => rate(v)) },
      xAxis: catAxis([], clockText),
      yAxis: { type: 'value', min: 0, axisLine: { show: false }, axisTick: { show: false }, axisLabel: { ...AXIS_LABEL, formatter: (v) => compactRate(v) }, splitLine: { lineStyle: { color: 'rgba(255,255,255,0.045)' } } },
      series: [
        { name: '读', type: 'line', smooth: true, showSymbol: false, lineStyle: { width: 1.5, color: C.cyan }, areaStyle: flat(C.cyan, 0.2), data: [] },
        { name: '写', type: 'line', smooth: true, showSymbol: false, lineStyle: { width: 1.4, color: C.gray }, areaStyle: flat(C.gray, 0.14), data: [] },
      ],
    });
  }

  // ---------------------------------------------------------------- 迷你折线（SVG）
  function sparkPath(values, w, h, pad = 2) {
    if (!values.length) return { line: '', area: '', last: null };
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

  function setSpark(id, values, color) {
    const svg = $(id);
    if (!svg) return;
    const line = svg.querySelector('.s-line');
    const area = svg.querySelector('.s-area');
    const dot = svg.querySelector('.s-dot');
    const { line: d, area: a, last } = sparkPath(values, 72, 20);
    line.setAttribute('d', d);
    area.setAttribute('d', a);
    if (dot && last) {
      dot.setAttribute('cx', last[0].toFixed(1));
      dot.setAttribute('cy', last[1].toFixed(1));
      dot.style.fill = color || 'var(--accent)';
    }
    if (color) { line.style.stroke = color; area.style.fill = color; }
  }

  // ---------------------------------------------------------------- 实时缓冲
  const MAX = 300;
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
    // 同一秒内的多帧合并成一帧：保证时间轴严格递增，避免轴标签重复
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

  function renderLiveCharts(s) {
    if (!ready) return;
    const ax = catAxis(live.ts, clockText);
    charts.cpu.setOption({ xAxis: ax, series: [
      { data: live.cpu.user }, { data: live.cpu.system }, { data: live.cpu.iowait },
      { data: live.cpu.steal }, { data: live.cpu.total },
    ] });
    charts.mem.setOption({ xAxis: ax, series: [{ data: live.mem }, { data: live.swap }] });
    charts.net.setOption({ xAxis: ax, series: [{ data: live.netRx }, { data: live.netTx }] });
    charts.disk.setOption({ xAxis: ax, series: [{ data: live.ioR }, { data: live.ioW }] });

    const cpu = (s && s.cpu) || {};
    const mem = (s && s.mem) || {};
    const parts = ((s && s.disk) || {}).partitions || [];
    const worst = parts.length ? Math.max(...parts.map((p) => p.percent)) : 0;
    setSpark('spark-cpu', live.sCpu, levelVar(cpu.total || 0));
    setSpark('spark-mem', live.sMem, levelVar(mem.percent || 0));
    setSpark('spark-disk', live.sDisk, levelVar(worst));
    setSpark('spark-net', live.sNet, 'var(--ok)');
  }

  // ---------------------------------------------------------------- 历史回看
  let mode = 'live';
  const RANGE_LABEL = { '5m': '5 分钟', '1h': '1 小时', '6h': '6 小时', '24h': '24 小时', '7d': '7 天' };

  async function loadHistory(range) {
    mode = range;
    timeFmt = (t) => dateText(t);
    const h = await (await fetch(api(`/api/history?range=${range}`))).json();
    if (!ready) return;
    const ts = h.ts || [];
    const brk = h.cpu_brk || [];
    const ax = catAxis(ts, (t) => dateText(t));

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

    const label = `${RANGE_LABEL[range]} · ${h.count || 0} 点`;
    $('mem-hint').textContent = label;
    $('net-hint').textContent = label;
    $('disk-hint').textContent = label;
  }

  function backToLive() {
    mode = 'live';
    timeFmt = (t) => clockText(t * 1000);
    const ax = catAxis(live.ts, clockText);
    ['cpu', 'mem', 'net', 'disk'].forEach((k) => charts[k].setOption({ xAxis: ax }));
    ['mem', 'net', 'disk'].forEach((k) => { $(`${k}-hint`).textContent = '实时'; });
  }

  // ---------------------------------------------------------------- 渲染
  const rowHTML = (k, pct, color, v) =>
    `<div class="row"><span class="r-k">${k}</span><span class="r-bar"><i style="width:${Math.min(100, pct)}%;background:${color}"></i></span><span class="r-v">${v}</span></div>`;

  function renderTop(s) {
    const host = s.host || {};
    $('host-name').textContent = host.label || host.hostname || '—';
    $('uptime').textContent = `运行 ${dur(host.uptime)}`;
  }

  function renderHero(s) {
    const cpu = s.cpu || {};
    const mem = s.mem || {};
    const parts = (s.disk || {}).partitions || [];
    const ifs = (s.net || {}).interfaces || [];

    // CPU
    animateNumber($('cpu-val'), cpu.total || 0, 0);
    $('cpu-bar').style.width = `${Math.min(100, cpu.total || 0)}%`;
    $('cpu-bar').style.background = levelVar(cpu.total || 0);
    $('cpu-sub').textContent = `load ${nf(cpu.load1, 2)} · ${cpu.cores || 0}核`;
    $('metric-cpu').title = `用户 ${nf((cpu.breakdown || {}).user, 1)}% · 系统 ${nf((cpu.breakdown || {}).system, 1)}% · iowait ${nf((cpu.breakdown || {}).iowait, 1)}% · steal ${nf((cpu.breakdown || {}).steal, 1)}% · 上下文切换 ${nf(cpu.ctx_switch_rate, 0)}/s`;

    // 内存
    animateNumber($('mem-val'), mem.percent || 0, 0);
    $('mem-bar').style.width = `${Math.min(100, mem.percent || 0)}%`;
    $('mem-bar').style.background = levelVar(mem.percent || 0);
    $('mem-sub').textContent = `${bytes(mem.used)} / ${bytes(mem.total)}`;
    $('metric-mem').title = `可用 ${bytes(mem.available)} · 缓存 ${bytes(mem.cached)} · Swap ${bytes(mem.swap_used)} (${nf(mem.swap_percent, 1)}%)`;

    // 磁盘
    const worst = parts.length ? parts.reduce((a, b) => (b.percent > a.percent ? b : a)) : null;
    animateNumber($('disk-val'), worst ? worst.percent : 0, 0);
    $('disk-bar').style.width = `${worst ? Math.min(100, worst.percent) : 0}%`;
    $('disk-bar').style.background = levelVar(worst ? worst.percent : 0);
    $('disk-sub').textContent = worst ? `${worst.mount} · 剩 ${bytes(worst.free)}` : '—';
    $('metric-disk').title = worst
      ? `${worst.device} ${worst.fstype} · inode ${nf(worst.inode_percent, 1)}% · 剩余 ${bytes(worst.free)}`
      : '';

    // 网络
    const rx = ifs.reduce((a, i) => a + i.rx_bps, 0);
    const tx = ifs.reduce((a, i) => a + i.tx_bps, 0);
    const total = rx + tx || 1;
    animateNumber($('net-val'), (rx + tx) / 1024 / 1024, 2);
    $('net-bar-rx').style.width = `${(rx / total) * 100}%`;
    $('net-bar-tx').style.width = `${(tx / total) * 100}%`;
    $('net-sub').textContent = `↓${rate(rx)} ↑${rate(tx)}`;
    $('metric-net').title = `连接 ${(s.net || {}).tcp_total || 0} · 错误/丢包 ${ifs.reduce((a, i) => a + i.err_in + i.err_out + i.drop_in + i.drop_out, 0)}`;

    // 整体判定
    const active = s.active_alerts || [];
    const crit = active.filter((a) => a.level === 'critical');
    const warn = active.filter((a) => a.level === 'warning');
    const box = document.querySelector('.verdict');
    box.classList.remove('warn', 'crit');
    const txt = $('v-text');
    const prev = txt.textContent;
    if (crit.length) {
      box.classList.add('crit');
      txt.textContent = `${crit.length} 项严重告警`;
      $('v-sub').textContent = `${crit[0].label} ${nf(crit[0].value, 1)}${crit[0].unit || '%'}`;
    } else if (warn.length) {
      box.classList.add('warn');
      txt.textContent = `${warn.length} 项告警`;
      $('v-sub').textContent = `${warn[0].label} ${nf(warn[0].value, 1)}${warn[0].unit || '%'}`;
    } else {
      txt.textContent = '系统正常';
      $('v-sub').textContent = '全部指标处于阈值内';
    }
    if (txt.textContent !== prev) flash(txt);

    // 指标卡告警态
    const map = { cpu: 'cpu', load: 'cpu', mem: 'mem', swap: 'mem', disk: 'disk', inode: 'disk' };
    document.querySelectorAll('.metric').forEach((el) => {
      const hit = active.find((a) => map[a.key] === el.dataset.metric);
      el.classList.toggle('warn', !!hit && hit.level === 'warning');
      el.classList.toggle('crit', !!hit && hit.level === 'critical');
    });
  }

  function renderCores(s) {
    const cores = (s.cpu || {}).percore || [];
    const box = $('cores');
    if (box.childElementCount !== cores.length) {
      box.innerHTML = cores.map((_, i) =>
        `<div class="core"><div class="core-h"><span>CPU${i}</span><b>—</b></div><div class="core-bar"><i style="width:0%"></i></div></div>`
      ).join('');
    }
    cores.forEach((c, i) => {
      const el = box.children[i];
      if (!el) return;
      el.querySelector('b').textContent = `${nf(c.busy, 0)}%`;
      el.title = `用户 ${nf(c.user, 1)}% · 系统 ${nf(c.system, 1)}% · iowait ${nf(c.iowait, 1)}% · steal ${nf(c.steal, 1)}%`;
      const bar = el.querySelector('.core-bar i');
      bar.style.width = `${Math.min(100, c.busy)}%`;
      bar.style.background = levelVar(c.busy);
    });
  }

  function renderMemRows(s) {
    const m = s.mem || {};
    $('mem-rows').innerHTML = [
      rowHTML('已用', m.percent || 0, levelVar(m.percent || 0), bytes(m.used)),
      rowHTML('可用', 100 - (m.percent || 0), 'var(--ok)', bytes(m.available)),
      rowHTML('缓存', 0, 'transparent', bytes((m.cached || 0) + (m.buffers || 0))),
      rowHTML('Swap', m.swap_percent || 0, levelVar(m.swap_percent || 0), bytes(m.swap_used)),
    ].join('');
  }

  function renderNetRows(s) {
    const ifs = (s.net || {}).interfaces || [];
    $('net-rows').innerHTML = ifs.map((i) => {
      const tot = i.rx_bps + i.tx_bps || 1;
      return `<div class="row split"><span class="r-k">${i.name}</span>` +
        `<span class="r-bar"><i style="width:${(i.rx_bps / tot) * 100}%"></i><i style="width:${(i.tx_bps / tot) * 100}%"></i></span>` +
        `<span class="r-v">↓${compactRate(i.rx_bps)} ↑${compactRate(i.tx_bps)}</span></div>`;
    }).join('');
    const tcp = (s.net || {}).tcp || {};
    const total = Object.values(tcp).reduce((a, b) => a + b, 0);
    if (mode === 'live') $('net-hint').textContent = `${total} 连接`;
  }

  function renderDiskRows(s) {
    const parts = (s.disk || {}).partitions || [];
    $('disk-rows').innerHTML = parts.map((p) =>
      rowHTML(p.mount, p.percent, levelVar(p.percent), `${nf(p.percent, 1)}% · 剩 ${bytes(p.free)}`)
    ).join('');
    const ios = (s.disk || {}).io || [];
    const top = ios[0];
    if (mode === 'live') {
      $('disk-hint').textContent = top ? `${top.name} 写 ${rate(top.write_bps)}` : '—';
    }
  }

  let procSort = 'cpu';
  function renderProcs(s) {
    const list = (procSort === 'mem' ? s.procs_mem : s.procs) || [];
    const top = list.slice(0, 8);
    $('proc-rows').innerHTML =
      `<div class="phead"><span>PID</span><span>进程</span><span style="text-align:right">CPU%</span><span style="text-align:right">内存</span><span>命令</span></div>` +
      top.map((p) =>
        `<div class="prow" title="${(p.cmd || '').replace(/"/g, '&quot;')}">` +
        `<span class="p-pid">${p.pid}</span>` +
        `<span class="p-name">${p.name}</span>` +
        `<span class="p-cpu${p.cpu >= 50 ? ' hot' : ''}">${nf(p.cpu, 1)}</span>` +
        `<span class="p-mem">${bytes(p.rss)}</span>` +
        `<span class="p-cmd">${p.cmd || ''}</span></div>`
      ).join('');
  }

  function renderAlerts(s) {
    const active = s.active_alerts || [];
    $('alert-list').innerHTML = active.length
      ? active.map((a) => `<span class="chip ${a.level}">${a.label}<b>${nf(a.value, 1)}${a.unit || '%'}</b></span>`).join('')
      : '<div class="clear"><span class="v-dot"></span>无活动告警</div>';
  }

  let eventsAll = [];
  let eventsExpanded = false;

  function evHTML(ev) {
    const resolved = ev.status === 'resolved';
    return `<div class="ev ${resolved ? 'resolved' : ev.level}">` +
      `<span class="ev-t">${ev.label} <b>${nf(ev.value, 1)}${ev.unit || '%'}</b></span>` +
      `<span class="ev-m">${dateText(ev.ts)}</span></div>`;
  }

  function renderEvents() {
    $('event-list').innerHTML = (eventsExpanded ? eventsAll : eventsAll.slice(0, 8)).map(evHTML).join('');
    const btn = $('event-toggle');
    btn.style.display = eventsAll.length > 8 ? '' : 'none';
    btn.textContent = eventsExpanded ? '收起' : '展开全部';
  }

  function addEvent(ev) {
    eventsAll.unshift(ev);
    if (eventsAll.length > 80) eventsAll.pop();
    renderEvents();
    const first = $('event-list').firstElementChild;
    if (first) { first.classList.add('fresh'); setTimeout(() => first.classList.remove('fresh'), 1200); }
  }

  function toast(ev) {
    const resolved = ev.status === 'resolved';
    const el = document.createElement('div');
    el.className = `toast ${resolved ? 'resolved' : ev.level}`;
    el.innerHTML =
      `<div class="t1">${resolved ? '已恢复' : (ev.level === 'critical' ? '严重告警' : '告警')} · ${ev.label}</div>` +
      `<div class="t2">${nf(ev.value, 1)}${ev.unit || '%'} · ${dateText(ev.ts)}</div>`;
    $('toasts').appendChild(el);
    setTimeout(() => {
      el.style.transition = 'opacity .3s, transform .3s';
      el.style.opacity = '0';
      el.style.transform = 'translateX(14px)';
      setTimeout(() => el.remove(), 320);
    }, 6500);
  }

  // ---------------------------------------------------------------- 数据流
  let last = null;

  function apply(s) {
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
      renderLiveCharts(s);
    }
    (s.events || []).forEach((ev) => { addEvent(ev); toast(ev); });
  }

  function setConn(on) {
    $('dot').classList.toggle('off', !on);
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
      } catch (err) { /* 脏帧忽略 */ }
    };
    ws.onclose = () => {
      setConn(false);
      retry += 1;
      setTimeout(connect, Math.min(8000, 800 * retry));
    };
    ws.onerror = () => { try { ws.close(); } catch (e) { /* noop */ } };
  }

  async function bootstrap() {
    initCharts();
    try { apply(await (await fetch(api('/api/snapshot'))).json()); } catch (e) { /* 等 WS */ }
    try {
      const d = await (await fetch(api('/api/events?limit=60'))).json();
      eventsAll = (d.events || []).slice();
      renderEvents();
      $('event-count').textContent = `${d.total || 0} 条`;
    } catch (e) { /* noop */ }
    connect();
  }

  // ---------------------------------------------------------------- 交互
  $('ranges').addEventListener('click', (e) => {
    const btn = e.target.closest('button[data-range]');
    if (!btn) return;
    $('ranges').querySelectorAll('button').forEach((b) => b.classList.toggle('on', b === btn));
    const r = btn.dataset.range;
    if (r === 'live') backToLive(); else loadHistory(r);
  });

  $('proc-sort').addEventListener('click', (e) => {
    const btn = e.target.closest('button[data-sort]');
    if (!btn) return;
    $('proc-sort').querySelectorAll('button').forEach((b) => b.classList.toggle('on', b === btn));
    procSort = btn.dataset.sort;
    if (last) renderProcs(last);
  });

  $('event-toggle').addEventListener('click', () => {
    eventsExpanded = !eventsExpanded;
    renderEvents();
  });

  setInterval(() => { $('clock').textContent = clockText(new Date()); }, 1000);
  $('clock').textContent = clockText(new Date());

  let t = null;
  window.addEventListener('resize', () => {
    clearTimeout(t);
    t = setTimeout(() => Object.values(charts).forEach((c) => c && c.resize && c.resize()), 150);
  });

  bootstrap();
})();
