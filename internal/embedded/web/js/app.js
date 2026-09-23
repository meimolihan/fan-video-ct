/* 视频剪切工具 Web 前端 */
(() => {
  'use strict';

  const $ = (sel) => document.querySelector(sel);
  const player = $('#player');
  const timeline = $('#timeline');
  const thumbStart = $('#thumb-start');
  const thumbEnd = $('#thumb-end');
  const timelineProgress = $('#timeline-progress');
  const timelineHit = $('#timeline-hit');
  const pbar = $('#pbar');
  const pbarFill = $('#pbar-fill');
  const pbarSeg = $('#pbar-seg');
  const pbarHit = $('#pbar-hit');
  const pbarStart = $('#pbar-start');
  const pbarEnd = $('#pbar-end');
  const ptime = $('#ptime');
  const btnPlay = $('#btn-play');
  const btnPlayStart = $('#btn-play-start');
  const btnPlayEnd = $('#btn-play-end');
  const btnStop = $('#btn-stop');
  const playerBar = $('#player-bar');

  const state = {
    path: '',        // 当前选中视频绝对路径
    info: null,      // 探测信息 {duration, streams, ...}
    streamUrl: '',   // 可播放流地址
    start: 0,        // 剪切起点（秒）
    end: 0,          // 剪切终点（秒）
    currentDir: '',  // 文件浏览器当前目录
    hasCover: false,
    keyframes: [],   // 关键帧时间点（copy 模式对齐用）
  };

  /* ---------- 工具 ---------- */
  const fmtTime = (s) => {
    if (!isFinite(s) || s < 0) s = 0;
    const h = Math.floor(s / 3600);
    const m = Math.floor((s % 3600) / 60);
    const sec = Math.floor(s % 60);
    const pad = (n) => String(n).padStart(2, '0');
    return `${pad(h)}:${pad(m)}:${pad(sec)}`;
  };

  const fmtSize = (n) => {
    if (!n) return '';
    if (n >= 1024 * 1024 * 1024) return (n / 1024 / 1024 / 1024).toFixed(2) + ' GB';
    if (n >= 1024 * 1024) return (n / 1024 / 1024).toFixed(1) + ' MB';
    if (n >= 1024) return (n / 1024).toFixed(1) + ' KB';
    return n + ' B';
  };

  let toastTimer = null;
  const toast = (msg, type = '') => {
    const el = $('#toast');
    el.textContent = msg;
    el.className = 'toast show ' + type;
    clearTimeout(toastTimer);
    toastTimer = setTimeout(() => { el.className = 'toast ' + type; }, 2800);
  };

  const esc = (s) => String(s).replace(/[&<>"']/g, (c) => ({
    '&': '&amp;', '<': '&lt;', '>': '&gt;', '"': '&quot;', "'": '&#39;',
  }[c]));

  /* ---------- 版本 ---------- */
  const loadVersion = () => {
    fetch('/api/version')
      .then((r) => r.json())
      .then((d) => { $('#version-badge').textContent = 'v' + d.version; })
      .catch(() => {});
  };

  /* ---------- 文件浏览器 ---------- */
  const renderFileList = (data) => {
    const box = $('#file-list');
    box.innerHTML = '';
    if (data.up) {
      const div = document.createElement('div');
      div.className = 'file-item file-up';
      div.innerHTML = '<span>⬆ 返回上级</span>';
      div.onclick = () => loadDir(data.up);
      box.appendChild(div);
    }
    (data.items || []).forEach((it) => {
      const div = document.createElement('div');
      div.className = 'file-item' + (it.is_dir ? ' dir' : (it.is_video ? ' video' : ''));
      div.setAttribute('data-path', it.path);
      div.innerHTML = `<span class="fname">${esc(it.name)}</span>` +
        (it.is_dir ? '' : `<span class="fsize">${fmtSize(it.size)}</span>`);
      div.onclick = () => it.is_dir ? loadDir(it.path) : selectVideo(it.path);
      if (!it.is_dir && it.path === state.path) div.classList.add('active');
      box.appendChild(div);
    });
    state.currentDir = data.current;
  };

  const loadDir = (dir) => {
    const q = dir ? '?path=' + encodeURIComponent(dir) : '';
    fetch('/api/media/dir' + q)
      .then((r) => r.json())
      .then((d) => { if (!d.error) renderFileList(d); else toast(d.error, 'err'); })
      .catch((e) => toast('加载目录失败: ' + e.message, 'err'));
  };

  /* ---------- 选中视频 ---------- */
  const selectVideo = (path) => {
    state.path = path;
    renderFileList$active(path);
    $('#video-empty').style.display = 'none';
    player.removeAttribute('src');
    player.load();

    fetch('/api/media/info?path=' + encodeURIComponent(path))
      .then((r) => r.json())
      .then((d) => {
        if (d.error) { toast(d.error, 'err'); return; }
        state.info = d.info;
        state.hasCover = !!d.hasCover;
        state.streamUrl = d.stream;
        state.start = 0;
        state.end = d.info && d.info.duration > 0 ? Math.floor(d.info.duration * 1000) / 1000 : 0;
        state.keyframes = [];
        $('#time-dur').textContent = '时长 ' + fmtTime(state.end);
        ptime.textContent = '00:00:00 / ' + fmtTime(state.end);
        playerBar.classList.remove('hidden');
        player.src = state.streamUrl.replace(/&/g, '&');
        setPlayIcon();
        refreshTimeline();
        refreshCover();
        loadKeyframes(path);
      })
      .catch((e) => toast('加载视频信息失败: ' + e.message, 'err'));
  };

  // 仅更新激活态（data-path 与当前选中一致）
  const renderFileList$active = (path) => {
    document.querySelectorAll('.file-item.video[data-path]').forEach((el) => {
      el.classList.toggle('active', el.getAttribute('data-path') === path);
    });
  };

  // 拉取关键帧（快速剪切对齐用）；失败时静默降级为自由定位
  const loadKeyframes = (path) => {
    fetch('/api/media/keyframes?path=' + encodeURIComponent(path))
      .then((r) => r.json())
      .then((d) => {
        state.keyframes = (d && d.keyframes) || [];
        if (state.keyframes.length) refreshTimeline();
      })
      .catch(() => { state.keyframes = []; });
  };

  const currentMode = () => document.querySelector('input[name="mode"]:checked').value;
  // copy 模式：起止吸附到最近关键帧，保证输出与预览一致
  const snapToFrame = (t) => {
    if (currentMode() !== 'copy') return t;
    const ks = state.keyframes;
    if (!ks.length) return t;
    let lo = 0, hi = ks.length - 1;
    if (t <= ks[lo]) return ks[lo];
    if (t >= ks[hi]) return ks[hi];
    while (hi - lo > 1) {
      const mid = (lo + hi) >> 1;
      if (ks[mid] <= t) lo = mid; else hi = mid;
    }
    return (t - ks[lo] <= ks[hi] - t) ? ks[lo] : ks[hi];
  };

  /* ---------- 时间轴 ---------- */
  // 关键帧刻度线（copy 模式可见对齐点）；拖动过程不重建以免卡顿
  const renderTicks = () => {
    const box = $('#timeline-ticks');
    if (currentMode() !== 'copy' || !state.keyframes.length ||
        !state.info || !state.info.duration) {
      box.innerHTML = '';
      return;
    }
    const w = timeline.clientWidth;
    const d = state.info.duration;
    if (!w || !d) return;
    const MIN_PX = 2;
    let html = '';
    let lastX = -Infinity;
    for (const kf of state.keyframes) {
      const x = (kf / d) * w;
      if (x - lastX < MIN_PX || x < 0 || x > w) continue;
      lastX = x;
      html += '<i style="left:' + ((kf / d) * 100).toFixed(3) + '%"></i>';
    }
    box.innerHTML = html;
  };

  const refreshTimeline = () => {
    const d = state.info ? state.info.duration : 0;
    if (!d) return;
    const pct = (t) => Math.max(0, Math.min(100, (t / d) * 100));
    thumbStart.style.left = pct(state.start) + '%';
    thumbEnd.style.left = pct(state.end) + '%';
    timelineProgress.style.left = pct(state.start) + '%';
    timelineProgress.style.width = pct(state.end - state.start) + '%';
    $('#time-start').textContent = fmtTime(state.start);
    $('#time-end').textContent = fmtTime(state.end);
    const note = currentMode() === 'copy' && state.keyframes.length
      ? `<div class="stat-msg keyframe-note">实际剪切将对齐关键帧：<b>${fmtTime(snapToFrame(state.start))}</b> ~ <b>${fmtTime(snapToFrame(state.end))}</b></div>`
      : '';
    $('#seg-box').innerHTML =
      `<div class="stat-msg"><span class="seg-badge">剪切片段</span>` +
      `<b>${fmtTime(state.start)}</b> ~ <b>${fmtTime(state.end)}</b>` +
      `<span class="seg-dur">时长 ${fmtTime(state.end - state.start)}</span></div>` + note;
    // 播放条锚点联动
    pbarStart.style.left = pct(state.start) + '%';
    pbarEnd.style.left = pct(state.end) + '%';
    pbarSeg.style.left = pct(state.start) + '%';
    pbarSeg.style.width = Math.max(0, pct(state.end - state.start)) + '%';
    renderTicks();
  };

  const seekFromEvent = (e, el) => {
    const rect = el.getBoundingClientRect();
    const ratio = (e.clientX - rect.left) / rect.width;
    const t = Math.max(0, Math.min(state.info.duration, Math.round(ratio * state.info.duration * 1000) / 1000));
    return t;
  };

  // 拖动/微调锚点后，让视频预览立即切到该帧画面
  let previewTimer = null;
  const previewAt = (t) => {
    if (!state.info || !state.info.duration) return;
    if (!player.paused) player.pause();
    clearTimeout(previewTimer);
    previewTimer = setTimeout(() => {
      player.currentTime = Math.max(0, Math.min(state.info.duration, t));
      setPlayIcon();
    }, 40);
  };

  // 点击设置起点；点击后拖动调整区间（拖动过程自由定位，仅提交时对齐关键帧）
  let dragging = null; // 'start' | 'end'
  timeline.addEventListener('pointerdown', (e) => {
    if (!state.info || !state.info.duration) return;
    const t = seekFromEvent(e, timeline);
    // 靠近哪一端就拖动哪一端，否则设置起点
    const startPix = Math.abs(t - state.start);
    const endPix = Math.abs(t - state.end);
    if (endPix < startPix && t > state.start) {
      dragging = 'end';
      state.end = t;
      previewAt(state.end);
    } else {
      dragging = 'start';
      state.start = t;
      if (state.end <= state.start) state.end = Math.min(state.info.duration, state.start + 1);
      previewAt(state.start);
    }
    refreshTimeline();
    timeline.setPointerCapture(e.pointerId);
  });
  timeline.addEventListener('pointermove', (e) => {
    if (!dragging) return;
    const t = seekFromEvent(e, timeline);
    if (dragging === 'start') {
      state.start = t;
      if (state.end <= state.start) state.end = Math.min(state.info.duration, state.start + 1);
      previewAt(state.start);
    } else {
      state.end = t;
      if (state.end <= state.start) state.start = Math.max(0, state.end - 1);
      previewAt(state.end);
    }
    refreshTimeline();
  });
  timeline.addEventListener('pointerup', () => {
    dragging = null;
    clearTimeout(previewTimer);
    previewTimer = null;
  });

  // 播放进度指示（覆盖视觉 + 播放条）
  const onProgress = () => {
    if (!state.info || !state.info.duration) return;
    const d = state.info.duration;
    const ct = player.currentTime;
    const pct = (t) => Math.max(0, Math.min(100, (t / d) * 100));
    timelineHit.style.display = 'block';
    timelineHit.style.left = pct(ct) + '%';
    // 播放条填充只在 [start,end] 区间内着色：
    // start 之前与 end 之后都保持“清空”（仅剩底色轨道）
    pbarFill.style.left = pct(state.start) + '%';
    pbarFill.style.width = pct(Math.max(0, Math.min(ct, state.end) - state.start)) + '%';
    pbarHit.style.left = pct(ct) + '%';
    ptime.textContent = fmtTime(ct) + ' / ' + fmtTime(d);
  };
  player.addEventListener('timeupdate', onProgress);
  player.addEventListener('loadedmetadata', onProgress);
  player.addEventListener('durationchange', onProgress);

  // 选中后默认暂停：元数据就绪时停在首帧，便于预览画面
  player.addEventListener('loadedmetadata', () => {
    if (player.paused && player.currentTime === 0) player.currentTime = 0.001;
  });

  // 播放到结束锚点自动暂停（预览选段）
  player.addEventListener('timeupdate', () => {
    if (player.paused) return;
    if (state.info && state.info.duration && state.end > state.start) {
      if (player.currentTime >= state.end) {
        player.currentTime = state.end;
        player.pause();
        setPlayIcon();
      }
    }
  });

  /* ---------- 播放控制条 ---------- */
  const setPlayIcon = () => {
    btnPlay.textContent = player.paused ? '▶' : '❚❚';
    document.querySelector('#player-bar').classList.toggle('paused', player.paused);
  };
  const onStateChange = () => {
    setPlayIcon();
    if (state.info && state.info.duration) ptime.textContent = fmtTime(player.currentTime) + ' / ' + fmtTime(state.info.duration);
  };
  player.addEventListener('play', onStateChange);
  player.addEventListener('pause', onStateChange);
  player.addEventListener('ended', () => {
    btnPlay.textContent = '↺';
    pbarHit.style.left = pctOf(state.end) + '%';
  });

  const pctOf = (t) => {
    const d = state.info ? state.info.duration : 0;
    return d ? Math.max(0, Math.min(100, (t / d) * 100)) : 0;
  };
  const pbarSeekFromEvent = (e) => {
    const rect = pbar.getBoundingClientRect();
    const ratio = (e.clientX - rect.left) / rect.width;
    const d = state.info ? state.info.duration : 0;
    return Math.max(0, Math.min(d, Math.round(ratio * d * 1000) / 1000));
  };

  // 点击播放条跳转进度（自由 seek）
  const seekBar = (e) => {
    if (!state.info || !state.info.duration) return;
    const t = pbarSeekFromEvent(e);
    player.currentTime = t;
    if (player.paused) onProgress();
  };
  let pbarDrag = null; // null | 'start' | 'end'
  const grabHandle = pbar.addEventListener('pointerdown', (e) => {
    if (!state.info || !state.info.duration) return;
    const rect = pbar.getBoundingClientRect();
    const x = e.clientX - rect.left;
    // 按像素找最近的锚点（拖动命中半径为手柄宽度 ~12px）
    const stx = (state.start / state.info.duration) * rect.width;
    const enx = (state.end / state.info.duration) * rect.width;
    const GRAB = 14;
    const distSt = Math.abs(x - stx);
    const distEn = Math.abs(x - enx);
    if (distSt < GRAB && distSt <= distEn) {
      pbarDrag = 'start';
      state.start = pbarSeekFromEvent(e);
      previewAt(state.start);
    } else if (distEn < GRAB) {
      pbarDrag = 'end';
      state.end = pbarSeekFromEvent(e);
      previewAt(state.end);
    } else {
      seekBar(e);
      return;
    }
    refreshTimeline();
    pbar.setPointerCapture(e.pointerId);
  });
  pbar.addEventListener('pointermove', (e) => {
    if (!pbarDrag) return;
    const t = pbarSeekFromEvent(e);
    if (pbarDrag === 'start') {
      state.start = Math.max(0, Math.min(t, state.end));
      if (state.end <= state.start) state.end = Math.min(state.info.duration, state.start + 1);
      previewAt(state.start);
    } else {
      state.end = Math.max(t, state.start);
      if (state.end <= state.start) state.start = Math.max(0, state.end - 1);
      previewAt(state.end);
    }
    refreshTimeline();
  });
  const endPbarDrag = () => {
    pbarDrag = null;
    clearTimeout(previewTimer);
    previewTimer = null;
  };
  pbar.addEventListener('pointerup', endPbarDrag);
  pbar.addEventListener('pointercancel', endPbarDrag);

  // 播放按钮：如果当前在 [start,end) 内则继续/暂停，否则从 start 开始
  const togglePlay = () => {
    if (!state.path) { toast('请先选择一个视频', 'err'); return; }
    const d = state.info && state.info.duration;
    if (!d) { player.play().catch(() => {}); return; }
    if (player.paused) {
      if (player.ended || player.currentTime < state.start - 0.2 || player.currentTime >= state.end - 0.05) {
        player.currentTime = state.start;
      }
      player.play().catch((e) => toast('播放失败: ' + e.message, 'err'));
    } else {
      player.pause();
    }
    setPlayIcon();
  };
  btnPlay.addEventListener('click', (e) => { e.stopPropagation(); togglePlay(); });

  // 预览播放：从起点锚点起播，到终点锚点自动暂停
  const playStartClip = () => {
    if (!state.info || !state.info.duration) { toast('请先选择视频', 'err'); return; }
    player.currentTime = Math.min(state.start, Math.max(0, state.info.duration - 0.05));
    player.play().catch((ee) => toast('播放失败: ' + ee.message, 'err'));
    setPlayIcon();
  };
  // 预览播放：从终点锚点前 10 秒起播，到终点锚点自动暂停
  const playEndClip = () => {
    if (!state.info || !state.info.duration) { toast('请先选择视频', 'err'); return; }
    if (state.end <= 0) { toast('请先设置终点锚点', 'err'); return; }
    player.currentTime = Math.max(0, state.end - 10);
    player.play().catch((ee) => toast('播放失败: ' + ee.message, 'err'));
    setPlayIcon();
  };
  btnPlayStart.addEventListener('click', (e) => { e.stopPropagation(); if (!player.paused) player.pause(); playStartClip(); });
  btnPlayEnd.addEventListener('click', (e) => { e.stopPropagation(); if (!player.paused) player.pause(); playEndClip(); });
  player.addEventListener('click', (e) => {
    if (e.target === btnPlay) return;
    togglePlay();
  });

  // 全屏
  $('#btn-fs').addEventListener('click', (e) => {
    e.stopPropagation();
    const wrap = player.closest('.video-wrap');
    if (!document.fullscreenElement) {
      if (wrap.requestFullscreen) wrap.requestFullscreen().catch(() => {});
    } else if (document.exitFullscreen) {
      document.exitFullscreen().catch(() => {});
    }
  });

  /* ---------- 停止并关闭预览 ---------- */
  const closeVideo = () => {
    player.pause();
    player.removeAttribute('src');
    player.load();
    state.path = '';
    state.info = null;
    state.streamUrl = '';
    state.start = 0;
    state.end = 0;
    state.hasCover = false;
    state.keyframes = [];
    $('#video-empty').style.display = 'block';
    playerBar.classList.add('hidden');
    $('#time-dur').textContent = '时长 --';
    $('#time-start').textContent = '00:00:00';
    $('#time-end').textContent = '00:00:00';
    $('#seg-box').innerHTML = '<div class="stat-msg">请选择视频以开始剪切</div>';
    ptime.textContent = '00:00:00 / 00:00:00';
    setPlayIcon();
    document.querySelectorAll('.file-item.video[data-path].active').forEach((el) => el.classList.remove('active'));
    thumbStart.style.left = '0%';
    thumbEnd.style.left = '100%';
    timelineProgress.style.left = '0%';
    timelineProgress.style.width = '100%';
    timelineHit.style.display = 'none';
    pbarStart.style.left = '0%';
    pbarEnd.style.left = '';
    pbarFill.style.width = '0%';
    pbarSeg.style.left = '0%';
    pbarSeg.style.width = '0%';
    pbarHit.style.left = '0%';
    renderTicks();
    refreshCover();
  };
  btnStop.addEventListener('click', (e) => {
    e.stopPropagation();
    closeVideo();
  });

  /* ---------- 精确微调与快捷键 ---------- */
  const MIN_GAP = 0.1; // 起点/终点最小间距
  const clampStart = (t) => Math.max(0, Math.min(t, state.info && state.info.duration ? state.end - MIN_GAP : 0));
  const clampEnd = (t) => Math.min(state.info ? state.info.duration : 0, Math.max(t, state.start + MIN_GAP));
  const currentStep = () => parseFloat($('#adj-step').value) || 0.5;

  document.querySelectorAll('.adj-btn').forEach((b) => {
    b.addEventListener('click', () => {
      if (!state.info || !state.info.duration) { toast('请先选择视频', 'err'); return; }
      const adj = b.getAttribute('data-adj');
      const dir = b.getAttribute('data-dir') === '1' ? 1 : -1;
      const step = currentStep();
      if (adj === 'start') {
        state.start = clampStart(state.start + step * dir);
        previewAt(state.start);
      } else {
        state.end = clampEnd(state.end + step * dir);
        previewAt(state.end);
      }
      refreshTimeline();
    });
  });

  const setStartAtCurrent = () => {
    if (!state.info || !state.info.duration) return;
    state.start = clampStart(player.currentTime || 0);
    refreshTimeline();
  };
  const setEndAtCurrent = () => {
    if (!state.info || !state.info.duration) return;
    state.end = clampEnd(player.currentTime || state.info.duration);
    refreshTimeline();
  };
  $('#btn-set-start').addEventListener('click', setStartAtCurrent);
  $('#btn-set-end').addEventListener('click', setEndAtCurrent);

  document.addEventListener('keydown', (e) => {
    const tag = (e.target.tagName || '').toLowerCase();
    if (tag === 'input' || tag === 'textarea' || tag === 'select' || e.target.isContentEditable) return;
    if (!state.info || !state.info.duration) return;
    if (e.code === 'Space') {
      e.preventDefault();
      togglePlay();
    } else if (e.key === 'ArrowLeft' || e.key === 'ArrowRight') {
      e.preventDefault();
      const step = currentStep() * (e.key === 'ArrowRight' ? 1 : -1);
      player.currentTime = Math.max(0, Math.min(state.info.duration, player.currentTime + step));
      onProgress();
    } else if (e.key === '[' || e.key === '【') {
      setStartAtCurrent();
    } else if (e.key === ']' || e.key === '】') {
      setEndAtCurrent();
    }
  });

  /* ---------- 剪切任务 ---------- */
  // 切换切割模式时，重新吸附/刷新范围提示
  document.querySelectorAll('input[name="mode"]').forEach((r) => {
    r.addEventListener('change', () => {
      if (currentMode() === 'copy' && state.keyframes.length) {
        state.start = snapToFrame(state.start);
        state.end = snapToFrame(state.end);
      }
      refreshTimeline();
    });
  });

  const createTask = () => {
    if (!state.path) { toast('请先选择一个视频', 'err'); return; }
    if (state.end - state.start <= 0) { toast('剪切区间无效', 'err'); return; }
    const mode = currentMode();
    const name = $('#output-name').value.trim();
    const body = {
      path: state.path,
      mode,
      start: snapToFrame(state.start),
      end: snapToFrame(state.end),
      output_name: name,
    };
    if (mode === 'copy' && body.end - body.start <= 0) { toast('剪切区间无效', 'err'); return; }
    fetch('/api/tasks', {
      method: 'POST',
      headers: { 'Content-Type': 'application/json' },
      body: JSON.stringify(body),
    })
      .then((r) => r.json())
      .then((d) => {
        if (d.error) { toast(d.error, 'err'); return; }
        toast('已创建剪切任务', 'ok');
        refreshTasks();
      })
      .catch((e) => toast('创建任务失败: ' + e.message, 'err'));
  };

  const statusMap = {
    queued: '排队中', running: '剪切中', done: '完成', failed: '失败', cancelled: '已取消',
  };

  const renderTasks = (data) => {
    const box = $('#task-list');
    box.innerHTML = '';
    (data.tasks || []).forEach((t) => {
      const item = document.createElement('div');
      item.className = 'task-item';
      const pct = Math.round((t.progress || 0) * 100);
      item.innerHTML = `
        <div class="t-head">
          <span class="t-name" title="${esc(t.path)}">${esc(t.output_name)}</span>
          ${t.status === 'done' && t.output_size ? `<span class="t-size">${fmtSize(t.output_size)}</span>` : ''}
          <span class="t-status ${esc(t.status)}">${statusMap[t.status] || t.status} ${t.status === 'running' ? pct + '%' : ''}</span>
        </div>
        <div class="t-bar"><i style="width:${t.status === 'done' ? 100 : pct}%"></i></div>
        ${t.error ? `<div class="t-err">${esc(t.error)}</div>` : ''}
        <div class="t-actions">
          ${t.status === 'running' || t.status === 'queued' ? `<button class="btn btn-sm" data-act="cancel" data-id="${t.id}">取消</button>` : ''}
          ${t.status === 'done' ? `<a class="btn btn-sm btn-primary" href="/api/tasks/${t.id}/download">下载</a>` : ''}
          ${t.status === 'failed' || t.status === 'cancelled' || t.status === 'done' ? `<button class="btn btn-sm" data-act="del" data-id="${t.id}">删除</button>` : ''}
        </div>`;
      box.appendChild(item);
    });
    if (!(data.tasks || []).length) {
      box.innerHTML = '<div class="video-empty">暂无任务</div>';
    }
  };

  const refreshTasks = () => {
    fetch('/api/tasks')
      .then((r) => r.json())
      .then((d) => { if (!d.error) renderTasks(d); })
      .catch(() => {});
  };

  $('#task-list').addEventListener('click', (e) => {
    const btn = e.target.closest('[data-act]');
    if (!btn) return;
    const id = btn.getAttribute('data-id');
    const act = btn.getAttribute('data-act');
    if (act === 'cancel') {
      fetch('/api/tasks/' + id, { method: 'DELETE' }).then(refreshTasks);
    } else if (act === 'del') {
      // 删除任务记录：终态任务通过清空兜底，逐个删除走 clean 接口
      fetch('/api/tasks', { method: 'DELETE' })
        .then((r) => r.json())
        .then((d) => { toast('已清除 ' + d.removed + ' 条完成任务记录', 'ok'); refreshTasks(); });
    }
  });

  $('#btn-cut').addEventListener('click', createTask);
  $('#btn-clean').addEventListener('click', () => {
    fetch('/api/tasks', { method: 'DELETE' })
      .then((r) => r.json())
      .then((d) => { toast('已清除 ' + d.removed + ' 条已完成任务', 'ok'); refreshTasks(); });
  });

  /* ---------- 封面 ---------- */
  const refreshCover = () => {
    const img = $('#cover-img');
    if (!state.path) { img.classList.add('cover-none'); img.src = ''; return; }
    img.classList.remove('cover-none');
    // 无自定义封面时默认显示首帧预览
    img.src = state.hasCover
      ? '/api/cover?path=' + encodeURIComponent(state.path) + '&_=' + Date.now()
      : '/api/cover/preview?path=' + encodeURIComponent(state.path) + '&t=0&_=' + Date.now();
  };

  $('#btn-cover-current').addEventListener('click', () => {
    if (!state.path) { toast('请先选择视频', 'err'); return; }
    const t = player.currentTime || (state.info ? state.info.duration / 2 : 0);
    fetch('/api/cover?path=' + encodeURIComponent(state.path) + '&t=' + t, { method: 'POST' })
      .then((r) => r.json())
      .then((d) => {
        if (d.error) { toast(d.error, 'err'); return; }
        state.hasCover = true;
        toast('已设置为封面 (t=' + t.toFixed(1) + 's)', 'ok');
        refreshCover();
      })
      .catch((e) => toast('设置封面失败: ' + e.message, 'err'));
  });

  // 上传自定义封面
  $('#cover-file').addEventListener('change', (e) => {
    const file = e.target.files[0];
    if (!file || !state.path) return;
    const fd = new FormData();
    fd.append('file', file);
    fetch('/api/cover/upload?path=' + encodeURIComponent(state.path), {
      method: 'POST',
      body: fd,
    })
      .then((r) => r.json())
      .then((d) => {
        if (d.error) { toast(d.error, 'err'); return; }
        state.hasCover = true;
        toast('自定义封面上传成功', 'ok');
        refreshCover();
      })
      .catch((err) => toast('上传失败: ' + err.message, 'err'));
    e.target.value = '';
  });

  $('#btn-cover-delete').addEventListener('click', () => {
    if (!state.path || !state.hasCover) return;
    fetch('/api/cover?path=' + encodeURIComponent(state.path), { method: 'DELETE' })
      .then((r) => r.json())
      .then((d) => {
        if (d.error) { toast(d.error, 'err'); return; }
        state.hasCover = false;
        toast('已移除封面', 'ok');
        refreshCover();
      })
      .catch((e) => toast('移除封面失败: ' + e.message, 'err'));
  });

  $('#btn-refresh').addEventListener('click', () => loadDir(state.currentDir || ''));

  /* ---------- 明暗主题切换（三态循环：亮 -> 暗 -> 跟随系统） ---------- */
  const THEMES = ['light', 'dark', 'auto'];
  let theme = localStorage.getItem('fvct-theme') || 'auto';
  const systemIsLight = () => window.matchMedia('(prefers-color-scheme: light)').matches;
  const applyTheme = () => {
    const el = document.documentElement;
    if (theme === 'auto') el.removeAttribute('data-theme');
    else el.setAttribute('data-theme', theme);
    localStorage.setItem('fvct-theme', theme);
    const dark = theme === 'dark' || (theme === 'auto' && !systemIsLight());
    const btn = $('#btn-theme');
    btn.textContent = dark ? '☀️' : '🌙';
    btn.title = theme === 'auto'
      ? '明暗主题：跟随系统（' + (dark ? '暗色' : '亮色') + '），点击切换'
      : '明暗主题：' + (dark ? '暗色' : '亮色') + '，点击切换';
  };
  $('#btn-theme').addEventListener('click', () => {
    theme = THEMES[(THEMES.indexOf(theme) + 1) % THEMES.length];
    applyTheme();
  });
  window.matchMedia('(prefers-color-scheme: light)').addEventListener('change', () => {
    if (theme === 'auto') applyTheme();
  });

  /* ---------- 启动 ---------- */
  const init = () => {
    applyTheme();
    loadVersion();
    loadDir('');
    refreshTasks();
    setInterval(refreshTasks, 1500);
  };
  document.addEventListener('DOMContentLoaded', init);

  /* ---------- 自定义可拖拽滚动条（纯增量，不触碰既有 id/class 与业务逻辑） ----------
     Windows「自动隐藏滚动条」会把原生条变成不可拖拽的叠加层；此处为
     文件/任务列表在桌面端(pointer:fine)提供一个始终可见、可鼠标拖动的
     自定义拇指。原生滚轮/触控/键盘滚动全部保留，仅叠加拖动能力。 */
  const initCustomScroll = (mountSel) => {
    const fine = window.matchMedia && window.matchMedia('(pointer: fine)').matches;
    if (!fine) return;
    const mount = document.querySelector(mountSel);
    if (!mount || mount.__cscroll) return;
    const panel = mount.closest('.panel');
    if (!panel || panel.__cscroll) return;
    mount.__cscroll = true;
    panel.__cscroll = true;

    const track = document.createElement('div');
    track.className = 'cscroll';
    const thumb = document.createElement('div');
    thumb.className = 'cscroll-thumb';
    track.appendChild(thumb);
    panel.appendChild(track);

    let drag = null;
    const sync = () => {
      const sh = mount.scrollHeight, ch = mount.clientHeight, st = mount.scrollTop;
      const over = sh - ch;
      if (over <= 4) { track.style.display = 'none'; return; }
      track.style.display = 'block';
      const th = track.clientHeight, mh = Math.max(28, ch * (th / sh));
      thumb.style.height = mh + 'px';
      thumb.style.top = Math.min(th - mh, (st / over) * (th - mh)) + 'px';
    };
    mount.addEventListener('scroll', sync, { passive: true });
    window.addEventListener('resize', sync);
    if (window.ResizeObserver) new ResizeObserver(sync).observe(mount);
    if (window.MutationObserver) {
      new MutationObserver(() => { sync(); }).observe(mount, { childList: true, subtree: true });
    }

    const onMove = (e) => {
      if (!drag) return;
      const th = track.clientHeight, mh = thumb.offsetHeight || 28;
      const top = Math.max(0, Math.min(th - mh, drag.top + (e.clientY - drag.y)));
      thumb.style.top = top + 'px';
      const over = mount.scrollHeight - mount.clientHeight;
      if (over > 0) mount.scrollTop = (top / Math.max(1, th - mh)) * over;
    };
    const onUp = () => {
      drag = null;
      thumb.classList.remove('dragging');
      track.classList.remove('dragging');
      document.body.classList.remove('cscroll-dragging');
      document.removeEventListener('pointermove', onMove);
      document.removeEventListener('pointerup', onUp);
    };
    thumb.addEventListener('pointerdown', (e) => {
      e.preventDefault();
      e.stopPropagation();
      drag = { y: e.clientY, top: parseFloat(thumb.style.top) || 0 };
      thumb.classList.add('dragging');
      track.classList.add('dragging');
      document.body.classList.add('cscroll-dragging');
      document.addEventListener('pointermove', onMove);
      document.addEventListener('pointerup', onUp);
    });
    sync();
  };
  initCustomScroll('#file-list');
  initCustomScroll('#task-list');

  /* ============ 片头片尾预设（纯增量模块，不改既有逻辑） ============ */
  const presetModal = $('#preset-modal');
  const presetFormModal = $('#preset-form-modal');
  const presetListBox = $('#preset-list');
  let presets = [];
  let presetSelectedName = '';
  let presetEditingName = null;

  const openPresetModal = () => {
    presetSelectedName = '';
    presetModal.hidden = false;
    document.body.classList.add('preset-open');
    loadPresets();
  };
  const closePresetModal = () => {
    presetModal.hidden = true;
    if (presetFormModal.hidden) document.body.classList.remove('preset-open');
  };
  const openPresetForm = (editName) => {
    presetEditingName = editName || null;
    $('#preset-form-title').textContent = editName ? '编辑预设' : '新增预设';
    if (editName) {
      const src = presets.find((p) => p.name === editName);
      if (src) {
        $('#preset-name').value = src.name;
        $('#preset-head').value = src.head;
        $('#preset-tail').value = src.tail;
      }
    } else {
      $('#preset-name').value = '';
      $('#preset-head').value = '';
      $('#preset-tail').value = '';
    }
    presetFormModal.hidden = false;
    const first = editName ? $('#preset-head') : $('#preset-name');
    setTimeout(() => first.focus(), 60);
  };
  const closePresetForm = () => {
    presetFormModal.hidden = true;
    if (presetModal.hidden) document.body.classList.remove('preset-open');
  };

  const loadPresets = () => {
    fetch('/api/presets')
      .then((r) => r.json())
      .then((d) => {
        if (d.error) { toast(d.error, 'err'); return; }
        presets = d.presets || [];
        renderPresets();
      })
      .catch((e) => toast('加载预设失败: ' + e.message, 'err'));
  };

  const renderPresets = () => {
    if (!presets.length) {
      presetListBox.innerHTML = '<div class="preset-empty">暂无预设，点击下方「新增预设」添加</div>';
      return;
    }
    presetListBox.innerHTML = '';
    presets.forEach((p) => {
      const el = document.createElement('div');
      el.className = 'preset-item' + (p.name === presetSelectedName ? ' active' : '');
      el.innerHTML =
        `<div class="preset-item-main" data-apply="${esc(p.name)}" title="点击套用：起点=${p.head}s，终点=总时长−${p.tail}s">` +
        `<div class="preset-item-name">${esc(p.name)}</div>` +
        `<div class="preset-item-meta">片头跳过 ${p.head}s · 片尾跳过 ${p.tail}s</div></div>` +
        `<div class="preset-item-actions">` +
        `<button class="btn btn-sm" data-edit="${esc(p.name)}" title="编辑">编辑</button>` +
        `<button class="btn btn-sm btn-danger" data-del="${esc(p.name)}" title="删除">删除</button>` +
        `</div>`;
      presetListBox.appendChild(el);
    });
  };

  // 套用预设：起点=片头，终点=总时长−片尾，沿用原有锚点刷新/预览/剪切逻辑
  const applyPreset = (p) => {
    if (!state.info || !state.info.duration) { toast('请先选择视频', 'err'); return; }
    const d = state.info.duration;
    let s = Math.max(0, Math.min(p.head, d));
    let e = Math.max(0, d - p.tail);
    if (e - s < MIN_GAP) {
      s = 0; e = d;
      toast('片头 + 片尾超出视频范围，已回退为整片', 'err');
    }
    state.start = s;
    state.end = e;
    if (!player.paused) player.pause();
    previewAt(s);
    refreshTimeline();
    closePresetModal();
    toast('已套用「' + p.name + '」：起点 ' + fmtTime(s) + '，终点 ' + fmtTime(e), 'ok');
  };

  const savePreset = () => {
    const name = $('#preset-name').value.trim();
    const head = parseFloat($('#preset-head').value);
    const tail = parseFloat($('#preset-tail').value);
    if (!name) { toast('请输入预设名称', 'err'); return; }
    if (!isFinite(head) || head < 0) { toast('片头时长必须为 ≥0 的数字', 'err'); return; }
    if (!isFinite(tail) || tail < 0) { toast('片尾时长必须为 ≥0 的数字', 'err'); return; }
    fetch('/api/presets', {
      method: 'POST',
      headers: { 'Content-Type': 'application/json' },
      body: JSON.stringify({ name, head, tail }),
    })
      .then((r) => r.json())
      .then((d) => {
        if (d.error) { toast(d.error, 'err'); return; }
        presets = d.presets || [];
        toast('已保存预设「' + name + '」', 'ok');
        closePresetForm();
        renderPresets();
      })
      .catch((e) => toast('保存预设失败: ' + e.message, 'err'));
  };

  const deletePreset = (name) => {
    if (!window.confirm('确定删除预设「' + name + '」？')) return;
    fetch('/api/presets/' + encodeURIComponent(name), { method: 'DELETE' })
      .then((r) => r.json())
      .then((d) => {
        if (d.error) { toast(d.error, 'err'); return; }
        presets = d.presets || [];
        if (presetSelectedName === name) presetSelectedName = '';
        toast('已删除预设「' + name + '」', 'ok');
        renderPresets();
      })
      .catch((e) => toast('删除预设失败: ' + e.message, 'err'));
  };

  $('#btn-presets').addEventListener('click', openPresetModal);
  $('#preset-close').addEventListener('click', closePresetModal);
  $('#preset-add').addEventListener('click', () => openPresetForm(null));

  // 列表点击委托：套用 / 编辑 / 删除
  $('#preset-list').addEventListener('click', (e) => {
    const editBtn = e.target.closest('[data-edit]');
    const delBtn = e.target.closest('[data-del]');
    const applyEl = e.target.closest('[data-apply]');
    if (editBtn) { e.stopPropagation(); openPresetForm(editBtn.getAttribute('data-edit')); return; }
    if (delBtn) { e.stopPropagation(); deletePreset(delBtn.getAttribute('data-del')); return; }
    if (applyEl) {
      const name = applyEl.getAttribute('data-apply');
      const preset = presets.find((p) => p.name === name);
      if (preset) applyPreset(preset);
    }
  });

  // 表单按钮
  $('#preset-form-save').addEventListener('click', savePreset);
  $('#preset-form-cancel').addEventListener('click', closePresetForm);
  $('#preset-form-close').addEventListener('click', closePresetForm);
  $('#preset-name').addEventListener('keydown', (e) => { if (e.key === 'Enter') { e.preventDefault(); savePreset(); } });

  // 点击遮罩关闭；Esc 关闭
  presetModal.addEventListener('click', (e) => { if (e.target === presetModal) closePresetModal(); });
  presetFormModal.addEventListener('click', (e) => { if (e.target === presetFormModal) closePresetForm(); });
  document.addEventListener('keydown', (e) => {
    if (e.key === 'Escape') { closePresetForm(); closePresetModal(); }
  });
})();