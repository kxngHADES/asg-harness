/* The hero's 3D hermit-crab shell: a seashell surface sampled as points,
   rotated and projected by hand on a 2D canvas. No WebGL library, about 4 KB.
   Drag to spin it. It sleeps when off screen or when the tab is hidden, and
   draws a single still frame under reduced motion or data saver. */
(() => {
  const canvas = document.getElementById('shell');
  if (!canvas) return;
  const ctx = canvas.getContext('2d');
  const reduce = matchMedia('(prefers-reduced-motion: reduce)').matches;
  const saveData = navigator.connection && navigator.connection.saveData;
  const still = reduce || saveData;

  // Brand colours: lime, cyan, magenta (additively blended where points overlap).
  const palette = [[198, 244, 50], [77, 225, 255], [255, 95, 216]];

  // Seashell surface. u runs along the spiral, v around the tube.
  const U = window.innerWidth < 700 ? 110 : 170;
  const V = window.innerWidth < 700 ? 30 : 42;
  const pts = new Float32Array(U * V * 3);
  const col = new Array(U * V);
  let i = 0;
  for (let a = 0; a < U; a++) {
    const u = a / (U - 1);
    const s = u * 5.3 * Math.PI;               // a little under three whorls
    const e1 = Math.exp(s / (6 * Math.PI)), e2 = Math.exp(s / (3 * Math.PI));
    for (let b = 0; b < V; b++) {
      const v = (b / V) * Math.PI * 2;
      const c2 = Math.cos(v / 2) ** 2;
      // conical seashell surface, normalised to roughly a unit sphere
      pts[i * 3] = (2 * (1 - e1) * Math.cos(s) * c2) / 3.4;
      pts[i * 3 + 1] = (2 * (e1 - 1) * Math.sin(s) * c2) / 3.4;
      pts[i * 3 + 2] = (1 - e2 - Math.sin(v) + e1 * Math.sin(v) + 2.7) / 3.4;
      // colour: blend across the spiral, lime → cyan → magenta
      const t = u * 2;
      const c0 = palette[Math.min(1, Math.floor(t))], c1 = palette[Math.min(2, Math.floor(t) + 1)];
      const f = t - Math.floor(t);
      col[i] = `rgb(${c0[0] + (c1[0] - c0[0]) * f | 0},${c0[1] + (c1[1] - c0[1]) * f | 0},${c0[2] + (c1[2] - c0[2]) * f | 0})`;
      i++;
    }
  }
  // Group points by colour bucket so fillStyle changes rarely.
  const buckets = new Map();
  for (let n = 0; n < col.length; n++) {
    if (!buckets.has(col[n])) buckets.set(col[n], []);
    buckets.get(col[n]).push(n);
  }

  let w = 0, h = 0, dpr = 1;
  function size() {
    dpr = Math.min(window.devicePixelRatio || 1, 1.5); // cap for battery + bandwidth
    w = canvas.clientWidth; h = canvas.clientHeight;
    canvas.width = w * dpr; canvas.height = h * dpr;
    ctx.setTransform(dpr, 0, 0, dpr, 0, 0);
  }

  let ry = 0.6, rx = -0.55, vy = still ? 0 : 0.0032, drag = null;
  let mx = 0, my = 0, scrollT = 0;

  function frame() {
    ctx.clearRect(0, 0, w, h);
    ctx.globalCompositeOperation = 'lighter';
    const narrow = w < 760;
    const cx = narrow ? w * 0.62 : w * 0.7 + mx * 18;
    const cy = h * (narrow ? 0.34 : 0.44) + my * 12 - scrollT * h * 0.25;
    const scale = Math.min(w, h) * (narrow ? 0.32 : 0.36) * (1 + scrollT * 0.5);
    const cY = Math.cos(ry), sY = Math.sin(ry), cX = Math.cos(rx + my * 0.08), sX = Math.sin(rx + my * 0.08);
    const dot = narrow ? 1.4 : 1.7;
    ctx.globalAlpha = 0.55 * (1 - scrollT * 0.6);
    for (const [fill, list] of buckets) {
      ctx.fillStyle = fill;
      for (let q = 0; q < list.length; q++) {
        const n = list[q] * 3;
        const x = pts[n], y = pts[n + 1], z = pts[n + 2];
        // rotate around z (the shell's own axis), then tilt toward the viewer
        const x1 = x * cY - y * sY, y1 = x * sY + y * cY;
        const y2 = y1 * cX - z * sX, z2 = y1 * sX + z * cX;
        const p = 5 / (5 + y2);               // perspective (camera well outside the shell)
        const s = dot * p;
        ctx.fillRect(cx + x1 * scale * p, cy - z2 * scale * p, s, s);
      }
    }
    ctx.globalCompositeOperation = 'source-over';
    ctx.globalAlpha = 1;
  }

  let running = false, visible = true, raf = 0;
  function loop() {
    if (!drag) { ry += vy; vy += ((still ? 0 : 0.0032) - vy) * 0.02; }
    frame();
    raf = running ? requestAnimationFrame(loop) : 0;
  }
  function start() { if (still || running || !visible || document.hidden) return; running = true; raf = requestAnimationFrame(loop); }
  function stop() { running = false; cancelAnimationFrame(raf); }

  canvas.addEventListener('pointerdown', (e) => { drag = { x: e.clientX, y: e.clientY, ry, rx }; canvas.setPointerCapture(e.pointerId); });
  canvas.addEventListener('pointermove', (e) => {
    if (!drag) return;
    const dx = e.clientX - drag.x;
    const nry = drag.ry + dx * 0.008;
    vy = (nry - ry) * 0.6; ry = nry;
    rx = Math.max(-1.9, Math.min(-0.3, drag.rx + (e.clientY - drag.y) * 0.004));
    if (still) frame();
  });
  const end = () => { drag = null; };
  canvas.addEventListener('pointerup', end);
  canvas.addEventListener('pointercancel', end);

  window.addEventListener('pointermove', (e) => {
    mx = e.clientX / window.innerWidth - 0.5; my = e.clientY / window.innerHeight - 0.5;
  }, { passive: true });

  // Shared with main.js: hero scroll progress 0..1
  window.addEventListener('asg:hero', (e) => { scrollT = e.detail; if (still) frame(); });

  new IntersectionObserver(([en]) => { visible = en.isIntersecting; visible ? start() : stop(); }).observe(canvas);
  document.addEventListener('visibilitychange', () => (document.hidden ? stop() : start()));
  window.addEventListener('resize', () => { size(); frame(); });

  size(); frame(); start();
})();
