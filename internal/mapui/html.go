package mapui

const indexHTML = `<!doctype html>
<html lang="en">
<head>
  <meta charset="utf-8">
  <meta name="viewport" content="width=device-width, initial-scale=1">
  <title>ROI Block Painter</title>
  <style>
    :root {
      color-scheme: light;
      --bg: #f6f7f9;
      --panel: #ffffff;
      --text: #1f2937;
      --muted: #667085;
      --line: #d0d5dd;
      --blue: #2563eb;
      --blue-dark: #1d4ed8;
    }

    * { box-sizing: border-box; }

    body {
      margin: 0;
      font-family: system-ui, -apple-system, BlinkMacSystemFont, "Segoe UI", sans-serif;
      color: var(--text);
      background: var(--bg);
    }

    main {
      width: min(1220px, calc(100vw - 32px));
      margin: 20px auto;
      display: grid;
      grid-template-columns: minmax(0, 1fr) 320px;
      gap: 16px;
      align-items: start;
    }

    .stage, .side, .result {
      background: var(--panel);
      border: 1px solid var(--line);
      border-radius: 8px;
      padding: 12px;
    }

    .video-box {
      position: relative;
      width: 100%;
      background: #111827;
      border-radius: 6px;
      overflow: hidden;
      line-height: 0;
    }

    video {
      display: block;
      width: 100%;
      height: auto;
    }

    canvas {
      display: block;
      position: absolute;
      inset: 0;
      width: 100%;
      height: 100%;
      touch-action: none;
      cursor: crosshair;
    }

    .transport {
      display: grid;
      grid-template-columns: auto minmax(0, 1fr) auto;
      gap: 10px;
      align-items: center;
      margin-top: 10px;
    }

    input[type="range"] { width: 100%; }

    .side {
      display: grid;
      gap: 12px;
    }

    .field {
      display: grid;
      gap: 4px;
    }

    label {
      color: var(--muted);
      font-size: 12px;
      font-weight: 600;
    }

    input, select, button {
      font: inherit;
    }

    input, select {
      width: 100%;
      border: 1px solid var(--line);
      border-radius: 6px;
      padding: 8px 10px;
      background: #fff;
      color: var(--text);
    }

    input[readonly] {
      color: var(--muted);
      background: #f9fafb;
    }

    .grid-2 {
      display: grid;
      grid-template-columns: 1fr 1fr;
      gap: 10px;
    }

    .palette {
      display: grid;
      grid-template-columns: repeat(5, 1fr);
      gap: 8px;
    }

    .swatch, .tool, .primary {
      height: 38px;
      border: 1px solid var(--line);
      border-radius: 6px;
      background: #fff;
      color: var(--text);
      cursor: pointer;
    }

    .swatch {
      position: relative;
      color: transparent;
    }

    .swatch.selected, .tool.selected {
      outline: 3px solid rgba(37, 99, 235, 0.25);
      border-color: var(--blue);
    }

    .tool-row {
      display: grid;
      grid-template-columns: repeat(2, minmax(0, 1fr));
      gap: 8px;
    }

    .primary {
      background: var(--blue);
      border-color: var(--blue);
      color: #fff;
      font-weight: 700;
    }

    .primary:hover { background: var(--blue-dark); }

    .tool:disabled, .primary:disabled {
      cursor: not-allowed;
      opacity: 0.62;
    }

    .status {
      min-height: 40px;
      padding: 10px;
      border: 1px solid var(--line);
      border-radius: 6px;
      color: var(--muted);
      background: #f9fafb;
      font-size: 13px;
      overflow-wrap: anywhere;
    }

    .result {
      grid-column: 1 / -1;
      display: grid;
      gap: 8px;
    }

    .result.hidden {
      display: none;
    }

    @media (max-width: 900px) {
      main { grid-template-columns: 1fr; }
    }
  </style>
</head>
<body>
<main>
  <section class="stage">
    <div class="video-box">
      <video id="video" src="/video" muted playsinline preload="metadata"></video>
      <canvas id="overlay"></canvas>
    </div>
    <div class="transport">
      <button id="play" class="tool" type="button">Play</button>
      <input id="scrub" type="range" min="0" max="0" value="0" step="0.01">
      <span id="time" class="status">0.00 / 0.00</span>
    </div>
  </section>

  <aside class="side">
    <div class="field">
      <label for="inputPath">Input</label>
      <input id="inputPath" readonly>
    </div>
    <div class="field">
      <label for="outDir">Output dir</label>
      <input id="outDir">
    </div>
    <div class="grid-2">
      <div class="field">
        <label for="targetBitrate">Target bitrate</label>
        <input id="targetBitrate">
      </div>
      <div class="field">
        <label for="encoder">Encoder</label>
        <select id="encoder">
          <option value="auto">auto</option>
          <option value="libx264">libx264</option>
          <option value="h264_nvenc">h264_nvenc</option>
          <option value="h264_amf">h264_amf</option>
          <option value="h264_videotoolbox">h264_videotoolbox</option>
        </select>
      </div>
    </div>
    <div class="grid-2">
      <div class="field">
        <label for="blockSize">Block size</label>
        <input id="blockSize" type="number" min="2" step="2">
      </div>
      <div class="field">
        <label for="bitrateWindow">Bitrate window</label>
        <input id="bitrateWindow" type="number" min="0.1" step="0.1">
      </div>
    </div>
    <div class="field">
      <label>Paint</label>
      <div id="palette" class="palette"></div>
    </div>
    <div class="tool-row">
      <button id="undo" class="tool" type="button">Undo</button>
      <button id="clear" class="tool" type="button">Clear</button>
      <button id="confirm" class="tool" type="button">Save</button>
      <button id="run" class="primary" type="button">Запустить</button>
    </div>
    <div id="status" class="status">Loading...</div>
  </aside>

  <section id="result" class="result hidden">
    <label for="resultVideo">Итоговый результат</label>
    <video id="resultVideo" controls playsinline preload="metadata"></video>
  </section>
</main>

<script>
(function () {
  var video = document.getElementById('video');
  var canvas = document.getElementById('overlay');
  var ctx = canvas.getContext('2d');
  var scrub = document.getElementById('scrub');
  var play = document.getElementById('play');
  var time = document.getElementById('time');
  var status = document.getElementById('status');
  var paletteBox = document.getElementById('palette');
  var inputPath = document.getElementById('inputPath');
  var outDir = document.getElementById('outDir');
  var targetBitrate = document.getElementById('targetBitrate');
  var encoder = document.getElementById('encoder');
  var blockSize = document.getElementById('blockSize');
  var bitrateWindow = document.getElementById('bitrateWindow');
  var confirm = document.getElementById('confirm');
  var run = document.getElementById('run');
  var result = document.getElementById('result');
  var resultVideo = document.getElementById('resultVideo');

  var state = {
    palette: [],
    selected: null,
    erasing: false,
    cells: {},
    undo: [],
    dragging: false
  };

  function setStatus(text) {
    status.textContent = text;
  }

  function cellKey(col, row) {
    return col + ',' + row;
  }

  function currentBlockSize() {
    var value = parseInt(blockSize.value, 10);
    if (!value || value < 2) return 64;
    return value;
  }

  function formatTime(value) {
    if (!isFinite(value)) value = 0;
    return value.toFixed(2);
  }

  function updateTime() {
    scrub.value = String(video.currentTime || 0);
    time.textContent = formatTime(video.currentTime) + ' / ' + formatTime(video.duration);
  }

  function pushUndo() {
    state.undo.push(JSON.stringify(state.cells));
    if (state.undo.length > 50) state.undo.shift();
  }

  function restoreUndo() {
    if (!state.undo.length) return;
    state.cells = JSON.parse(state.undo.pop());
    draw();
  }

  function resizeCanvas() {
    var rect = video.getBoundingClientRect();
    var dpr = window.devicePixelRatio || 1;
    canvas.style.width = rect.width + 'px';
    canvas.style.height = rect.height + 'px';
    canvas.width = Math.max(1, Math.round(rect.width * dpr));
    canvas.height = Math.max(1, Math.round(rect.height * dpr));
    ctx.setTransform(dpr, 0, 0, dpr, 0, 0);
    draw();
  }

  function draw() {
    var width = video.clientWidth;
    var height = video.clientHeight;
    ctx.clearRect(0, 0, width, height);
    if (!video.videoWidth || !video.videoHeight) return;

    var sx = width / video.videoWidth;
    var sy = height / video.videoHeight;
    var bs = currentBlockSize();

    Object.keys(state.cells).forEach(function (key) {
      var c = state.cells[key];
      var x = c.col * bs * sx;
      var y = c.row * bs * sy;
      var w = Math.min(bs, video.videoWidth - c.col * bs) * sx;
      var h = Math.min(bs, video.videoHeight - c.row * bs) * sy;
      ctx.fillStyle = c.color + '66';
      ctx.strokeStyle = c.color;
      ctx.lineWidth = 2;
      ctx.fillRect(x, y, w, h);
      ctx.strokeRect(x + 1, y + 1, Math.max(0, w - 2), Math.max(0, h - 2));
    });

    ctx.strokeStyle = 'rgba(255,255,255,0.22)';
    ctx.lineWidth = 1;
    for (var gx = bs; gx < video.videoWidth; gx += bs) {
      ctx.beginPath();
      ctx.moveTo(gx * sx, 0);
      ctx.lineTo(gx * sx, height);
      ctx.stroke();
    }
    for (var gy = bs; gy < video.videoHeight; gy += bs) {
      ctx.beginPath();
      ctx.moveTo(0, gy * sy);
      ctx.lineTo(width, gy * sy);
      ctx.stroke();
    }
  }

  function pointerCell(event) {
    if (!video.videoWidth || !video.videoHeight) return null;
    var rect = canvas.getBoundingClientRect();
    var sx = rect.width / video.videoWidth;
    var sy = rect.height / video.videoHeight;
    var x = (event.clientX - rect.left) / sx;
    var y = (event.clientY - rect.top) / sy;
    if (x < 0 || y < 0 || x >= video.videoWidth || y >= video.videoHeight) return null;
    var bs = currentBlockSize();
    return {
      col: Math.floor(x / bs),
      row: Math.floor(y / bs)
    };
  }

  function paint(event) {
    var pos = pointerCell(event);
    if (!pos) return;
    var key = cellKey(pos.col, pos.row);
    if (state.erasing) {
      delete state.cells[key];
    } else if (state.selected) {
      state.cells[key] = {
        col: pos.col,
        row: pos.row,
        qoffset: state.selected.qoffset,
        color: state.selected.color
      };
    }
    draw();
  }

  function cellsArray() {
    return Object.keys(state.cells).map(function (key) {
      var c = state.cells[key];
      return { col: c.col, row: c.row, qoffset: c.qoffset };
    });
  }

  function buildPayload() {
    return {
      out: outDir.value.trim(),
      targetBitrate: targetBitrate.value.trim(),
      encoder: encoder.value,
      bitrateWindow: parseFloat(bitrateWindow.value),
      roiBlockSize: parseInt(blockSize.value, 10),
      cells: cellsArray()
    };
  }

  function postJSON(url, payload) {
    return fetch(url, {
      method: 'POST',
      headers: { 'Content-Type': 'application/json' },
      body: JSON.stringify(payload)
    }).then(function (res) {
      if (!res.ok) {
        return res.text().then(function (text) { throw new Error(text); });
      }
      return res.json();
    });
  }

  function setRunning(running) {
    run.disabled = running;
    confirm.disabled = running;
  }

  function selectTool(entry, button) {
    state.selected = entry;
    state.erasing = false;
    Array.prototype.forEach.call(paletteBox.children, function (child) {
      child.classList.remove('selected');
    });
    button.classList.add('selected');
  }

  function buildPalette() {
    paletteBox.innerHTML = '';
    state.palette.forEach(function (entry, index) {
      var btn = document.createElement('button');
      btn.type = 'button';
      btn.className = 'swatch';
      btn.style.background = entry.color;
      btn.title = entry.name + ' qoffset ' + entry.qoffset;
      btn.textContent = entry.name;
      btn.addEventListener('click', function () {
        selectTool(entry, btn);
      });
      paletteBox.appendChild(btn);
      if (index === 0) selectTool(entry, btn);
    });

    var erase = document.createElement('button');
    erase.type = 'button';
    erase.className = 'tool';
    erase.textContent = 'Erase';
    erase.title = 'Erase blocks';
    erase.addEventListener('click', function () {
      state.erasing = true;
      state.selected = null;
      Array.prototype.forEach.call(paletteBox.children, function (child) {
        child.classList.remove('selected');
      });
      erase.classList.add('selected');
    });
    paletteBox.appendChild(erase);
  }

  canvas.addEventListener('pointerdown', function (event) {
    pushUndo();
    state.dragging = true;
    canvas.setPointerCapture(event.pointerId);
    paint(event);
  });
  canvas.addEventListener('pointermove', function (event) {
    if (state.dragging) paint(event);
  });
  canvas.addEventListener('pointerup', function () {
    state.dragging = false;
  });
  canvas.addEventListener('pointercancel', function () {
    state.dragging = false;
  });

  play.addEventListener('click', function () {
    if (video.paused) {
      video.play();
    } else {
      video.pause();
    }
  });
  video.addEventListener('play', function () { play.textContent = 'Pause'; });
  video.addEventListener('pause', function () { play.textContent = 'Play'; });
  video.addEventListener('loadedmetadata', function () {
    scrub.max = String(video.duration || 0);
    updateTime();
    resizeCanvas();
  });
  video.addEventListener('timeupdate', updateTime);
  scrub.addEventListener('input', function () {
    video.currentTime = parseFloat(scrub.value || '0');
    updateTime();
  });
  window.addEventListener('resize', resizeCanvas);

  blockSize.addEventListener('change', function () {
    pushUndo();
    state.cells = {};
    resizeCanvas();
    setStatus('Block size changed; map cleared.');
  });

  document.getElementById('undo').addEventListener('click', restoreUndo);
  document.getElementById('clear').addEventListener('click', function () {
    pushUndo();
    state.cells = {};
    draw();
  });
  confirm.addEventListener('click', function () {
    var payload = buildPayload();
    setStatus('Saving...');
    postJSON('/api/config', payload).then(function (data) {
      setStatus('Saved ' + data.rectCount + ' rectangles from ' + data.blockCount + ' blocks. ' + data.command);
    }).catch(function (err) {
      setStatus('Error: ' + err.message);
    });
  });

  run.addEventListener('click', function () {
    var payload = buildPayload();
    result.classList.add('hidden');
    resultVideo.removeAttribute('src');
    resultVideo.load();
    setRunning(true);
    setStatus('Запуск обработки... это может занять несколько минут.');
    postJSON('/api/run', payload).then(function (data) {
      resultVideo.src = data.resultUrl;
      result.classList.remove('hidden');
      resultVideo.load();
      setStatus('Готово. Итоговый результат сохранен: ' + data.resultPath);
    }).catch(function (err) {
      setStatus('Error: ' + err.message);
    }).finally(function () {
      setRunning(false);
    });
  });

  fetch('/api/meta').then(function (res) {
    return res.json();
  }).then(function (meta) {
    inputPath.value = meta.input;
    outDir.value = meta.out;
    targetBitrate.value = meta.targetBitrate;
    encoder.value = meta.encoder;
    blockSize.value = meta.roiBlockSize;
    bitrateWindow.value = meta.bitrateWindow;
    state.palette = meta.palette || [];
    buildPalette();
    setStatus('Ready. Painted blocks: 0.');
    resizeCanvas();
  }).catch(function (err) {
    setStatus('Error: ' + err.message);
  });
})();
</script>
</body>
</html>
`
