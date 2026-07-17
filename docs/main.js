/* Isola · isola.run
   "Landfall": a luminous drifting light-field with one still island of deep
   violet carrying the words. WebGL shader with a 2D-canvas fallback, tabs,
   copy chips, small choreography. No dependencies. Reduced motion gets a
   calm curated still. */
"use strict";

document.documentElement.classList.add("js");
const RM = window.matchMedia("(prefers-reduced-motion: reduce)").matches;

/* Shared page state: scroll depth (0 = luminous surface, 1 = deep water). */
const state = {
  depth: 0,
  full: 0,
};

function readScroll() {
  const vh = window.innerHeight || 1;
  const max = document.documentElement.scrollHeight - vh;
  state.depth = Math.min(1, window.scrollY / (vh * 1.15));
  state.full = max > 0 ? Math.min(1, window.scrollY / max) : 0;
}
readScroll();
window.addEventListener("scroll", readScroll, { passive: true });

/* -------------------------------------------------------------- landfall */
/* The light drifts, the island holds. All color mixing happens in OKLab so
   the blush-to-periwinkle passage stays luminous instead of graying out. */
const AURORA_FS = `
precision highp float;
uniform vec2 uRes;
uniform float uTime;
uniform float uDepth;
uniform float uExpo;
uniform vec3 uWave;   // x, y in canvas px, z = ring radius px (inactive < 0)
uniform float uAmp;   // ring displacement in px

float hash(vec2 p) {
  p = fract(p * vec2(234.34, 435.345));
  p += dot(p, p + 34.23);
  return fract(p.x * p.y);
}
float noise(vec2 p) {
  vec2 i = floor(p), f = fract(p);
  f = f * f * (3.0 - 2.0 * f);
  float a = hash(i), b = hash(i + vec2(1.0, 0.0));
  float c = hash(i + vec2(0.0, 1.0)), d = hash(i + vec2(1.0, 1.0));
  return mix(mix(a, b, f.x), mix(c, d, f.x), f.y);
}
float fbm(vec2 p) {
  float v = 0.0, a = 0.5;
  for (int i = 0; i < 4; i++) {
    v += a * noise(p);
    p = p * 2.03 + vec2(7.3, -4.1);
    a *= 0.5;
  }
  return v;
}

/* OKLab stops, precomputed from the brand hexes */
const vec3 PERI  = vec3(0.7331,  0.0229, -0.1056);  // #9EA0EB field body
const vec3 ICE   = vec3(0.8483, -0.0264, -0.0237);  // #B5D3DE lower drift
const vec3 BLUSH = vec3(0.9044,  0.0471,  0.0154);  // #FFD3D3 dawn peak
const vec3 SKY   = vec3(0.7523, -0.0425, -0.0794);  // #7AB6E3 breath
const vec3 SHORE = vec3(0.5206,  0.1014, -0.1821);  // #7D3ECD island dissolve
const vec3 FEATH = vec3(0.4768,  0.0557, -0.1681);  // #5D42B8 island feather
const vec3 BODY  = vec3(0.4115,  0.0454, -0.1513);  // #49349A island body
const vec3 CORE  = vec3(0.3295,  0.0340, -0.1287);  // #322375 island core
const vec3 DEEP  = vec3(0.1962,  0.0115, -0.0959);  // #0E093F depth

vec3 oklab2srgb(vec3 c) {
  float l_ = c.x + 0.3963377774 * c.y + 0.2158037573 * c.z;
  float m_ = c.x - 0.1055613458 * c.y - 0.0638541728 * c.z;
  float s_ = c.x - 0.0894841775 * c.y - 1.2914855480 * c.z;
  vec3 lms = vec3(l_ * l_ * l_, m_ * m_ * m_, s_ * s_ * s_);
  vec3 lin = vec3(
    4.0767416621 * lms.x - 3.3077115913 * lms.y + 0.2309699292 * lms.z,
    -1.2684380046 * lms.x + 2.6097574011 * lms.y - 0.3413193965 * lms.z,
    -0.0041960863 * lms.x - 0.7034186147 * lms.y + 1.7076147010 * lms.z);
  lin = clamp(lin, 0.0, 1.0);
  return pow(lin, vec3(1.0 / 2.2));
}

float smin(float a, float b, float k) {
  float h = clamp(0.5 + 0.5 * (b - a) / k, 0.0, 1.0);
  return mix(b, a, h) - k * h * (1.0 - h);
}

/* island: two blended circles, dark landmass. Landscape hugs the left edge,
   portrait becomes the lower two thirds. Units: vh (q = uv * aspect, y up). */
float islandSDF(vec2 q, float ar) {
  float land = smoothstep(0.75, 1.05, ar);
  vec2 c1 = mix(vec2(ar * 0.5, -0.05), vec2(0.04, 0.66), land);
  float r1 = mix(0.72, 0.68, land);
  vec2 c2 = mix(vec2(ar * 0.1, 0.22), vec2(0.34, 0.06), land);
  float r2 = mix(0.5, 0.86, land);
  return smin(length(q - c1) - r1, length(q - c2) - r2, 0.3);
}

/* sharp rounded-diamond glint (the mark's geometry, far islands of light) */
float glintSDF(vec2 p, float s) {
  p = abs(mat2(0.7071, -0.7071, 0.7071, 0.7071) * p);
  vec2 b = p - vec2(s);
  return length(max(b, 0.0)) + min(max(b.x, b.y), 0.0) - s * 0.28;
}

void main() {
  vec2 uv = gl_FragCoord.xy / uRes;
  float ar = uRes.x / uRes.y;
  vec2 q = vec2(uv.x * ar, uv.y);

  float d = islandSDF(q, ar);
  float m = smoothstep(0.34, -0.10, d);   // 0 in the field, 1 on land

  /* tide pulse: a pressure wave bends the light; the island is excluded */
  vec2 uvW = uv;
  if (uWave.z > 0.0) {
    vec2 dpx = gl_FragCoord.xy - uWave.xy;
    float wd = max(length(dpx), 1.0);
    float ring = exp(-pow((wd - uWave.z) / (uRes.y * 0.15), 2.0));
    uvW += (dpx / wd) * ring * uAmp * (1.0 - m) / uRes;
  }
  vec2 qW = vec2(uvW.x * ar, uvW.y);

  /* the field advects like mist and slows to stillness at the shore */
  float t = uTime;
  vec2 vel = vec2(0.013, 0.005) * (1.0 - m);
  vec2 p = qW * 1.15 + vel * t;
  float n1 = fbm(p);
  float n2 = fbm(qW * 1.8 - vel * t * 0.7 + vec2(4.7, 2.3));

  vec3 field = mix(PERI, ICE,
    smoothstep(0.1, 0.9, (1.0 - uvW.y) * 0.55 + uvW.x * 0.45 + (n1 - 0.5) * 0.55));
  float blushW = smoothstep(0.52, 1.02, uvW.x * 0.58 + uvW.y * 0.60 + (n2 - 0.5) * 0.38);
  field = mix(field, BLUSH, blushW * 0.9);
  float skyW = smoothstep(0.55, 0.95, n2) * smoothstep(0.12, 0.42, uvW.y) * smoothstep(0.95, 0.5, uvW.y);
  field = mix(field, SKY, skyW * 0.5);
  field.x += (n1 - 0.5) * 0.05;

  /* island ramp: shore dissolve, feather, body, core */
  vec3 land = mix(SHORE, FEATH, smoothstep(0.10, -0.06, d));
  land = mix(land, BODY, smoothstep(-0.04, -0.16, d));
  land = mix(land, CORE, smoothstep(-0.22, -0.44, d));
  land.x += (n1 - 0.5) * 0.012;   // barely-there texture so land isn't flat

  vec3 col = mix(field, land, m);

  /* glints: tiny crisp diamonds adrift in the light, one is ephemeral */
  float px = 1.5 / uRes.y;
  for (int i = 0; i < 5; i++) {
    float fi = float(i);
    vec2 gp = vec2(
      mix(0.68, 0.97, fract(fi * 0.618 + 0.21)) * ar,
      mix(0.12, 0.88, fract(fi * 0.414 + 0.37)));
    gp += vec2(sin(t * 0.05 + fi * 2.1), cos(t * 0.04 + fi * 1.3)) * 0.012;
    float gs = 0.007 + 0.005 * fract(fi * 0.73 + 0.29);
    float ga = 0.5 + 0.3 * fract(fi * 0.53 + 0.11);
    if (i == 4) ga *= smoothstep(0.25, 0.65, 0.5 + 0.5 * sin(t * 0.45));
    float gd = glintSDF(q - gp, gs);
    col = mix(col, vec3(0.985, 0.0, 0.0), smoothstep(px, -px, gd) * ga * (1.0 - m));
  }

  /* the dive: everything settles toward deep indigo as you scroll */
  col = mix(col, DEEP, uDepth * 0.85);
  col.x *= mix(1.0, 0.55, uDepth);

  col.x *= uExpo;   // load-time exposure ramp

  vec3 srgb = oklab2srgb(col);
  srgb += (hash(gl_FragCoord.xy + fract(uTime)) - 0.5) * (2.0 / 255.0);
  gl_FragColor = vec4(srgb, 1.0);
}`;

function startAuroraGL(canvas) {
  const gl = canvas.getContext("webgl", {
    alpha: false,
    antialias: false,
    depth: false,
    stencil: false,
    powerPreference: "low-power",
    failIfMajorPerformanceCaveat: true,
  });
  if (!gl) return false;

  const compile = (type, src) => {
    const s = gl.createShader(type);
    gl.shaderSource(s, src);
    gl.compileShader(s);
    if (!gl.getShaderParameter(s, gl.COMPILE_STATUS)) return null;
    return s;
  };
  const vs = compile(gl.VERTEX_SHADER, "attribute vec2 a;void main(){gl_Position=vec4(a,0.,1.);}");
  const fs = compile(gl.FRAGMENT_SHADER, AURORA_FS);
  if (!vs || !fs) return false;
  const prog = gl.createProgram();
  gl.attachShader(prog, vs);
  gl.attachShader(prog, fs);
  gl.linkProgram(prog);
  if (!gl.getProgramParameter(prog, gl.LINK_STATUS)) return false;
  gl.useProgram(prog);

  gl.bindBuffer(gl.ARRAY_BUFFER, gl.createBuffer());
  gl.bufferData(gl.ARRAY_BUFFER, new Float32Array([-1, -1, 3, -1, -1, 3]), gl.STATIC_DRAW);
  const loc = gl.getAttribLocation(prog, "a");
  gl.enableVertexAttribArray(loc);
  gl.vertexAttribPointer(loc, 2, gl.FLOAT, false, 0, 0);

  const uRes = gl.getUniformLocation(prog, "uRes");
  const uTime = gl.getUniformLocation(prog, "uTime");
  const uDepth = gl.getUniformLocation(prog, "uDepth");
  const uExpo = gl.getUniformLocation(prog, "uExpo");
  const uWave = gl.getUniformLocation(prog, "uWave");
  const uAmp = gl.getUniformLocation(prog, "uAmp");

  const scale = 0.5;
  function resize() {
    const w = Math.max(2, Math.round(window.innerWidth * scale));
    const h = Math.max(2, Math.round(window.innerHeight * scale));
    canvas.width = w;
    canvas.height = h;
    gl.viewport(0, 0, w, h);
    gl.uniform2f(uRes, w, h);
  }
  resize();

  /* the tide pulse: one wave at a time, pressed out of the light */
  const wave = { born: -1e9, x: 0, y: 0 };
  const WAVE_LIFE = 4800, WAVE_SPEED = 180, WAVE_COOLDOWN = 3000;
  let start = 0;

  function pulse(cssX, cssY, tMs) {
    if (RM || tMs - wave.born < WAVE_LIFE + WAVE_COOLDOWN - 2400) return;
    wave.born = tMs;
    wave.x = cssX * scale;
    wave.y = canvas.height - cssY * scale;
  }

  function frame(tMs) {
    gl.uniform1f(uTime, tMs / 1000);
    gl.uniform1f(uDepth, Math.min(1, state.depth * 0.85 + state.full * 0.25));
    const age = (tMs - wave.born) / 1000;
    if (age * 1000 < WAVE_LIFE) {
      gl.uniform3f(uWave, wave.x, wave.y, age * WAVE_SPEED * scale);
      gl.uniform1f(uAmp, 12 * scale * (1 - age / (WAVE_LIFE / 1000)));
    } else {
      gl.uniform3f(uWave, 0, 0, -1);
      gl.uniform1f(uAmp, 0);
    }
    const expo = RM ? 1 : Math.min(1, 0.85 + 0.15 * ((tMs - start) / 1200));
    gl.uniform1f(uExpo, expo);
    gl.drawArrays(gl.TRIANGLES, 0, 3);
  }
  gl.uniform3f(uWave, 0, 0, -1);
  frame(RM ? 21000 : 0);
  document.documentElement.classList.add("lit");
  canvas.classList.add("on");

  if (RM) {
    // no motion, but the dive must still track scroll position
    let queued = false;
    const still = () => {
      if (queued) return;
      queued = true;
      requestAnimationFrame(() => {
        queued = false;
        frame(21000);
      });
    };
    window.addEventListener("scroll", still, { passive: true });
    window.addEventListener("resize", () => {
      resize();
      still();
    });
  }

  if (!RM) {
    window.addEventListener("resize", resize);

    // the mark presses the first wave out itself, then the pointer may
    const lockup = document.querySelector(".lockup");
    setTimeout(() => {
      if (!lockup) return;
      const r = lockup.getBoundingClientRect();
      pulse(r.left + r.width * 0.72, r.top + r.height * 0.18, performance.now() - start);
    }, 2300);
    document.querySelector(".hero")?.addEventListener("pointerdown", (e) => {
      pulse(e.clientX, e.clientY, performance.now() - start);
    });

    const loop = (t) => {
      if (!start) start = t;
      frame(t - start);
      requestAnimationFrame(loop);
    };
    requestAnimationFrame(loop);
  }
  return true;
}

function startAurora2D(canvas) {
  const ctx = canvas.getContext("2d", { alpha: false });
  if (!ctx) return false;

  // luminous field masses (drift gently)
  const FIELD = [
    { c: [255, 211, 211], a: 0.85, r: 0.62, x: 0.86, y: 0.16, ax: 0.04, ay: 0.03, s: 0.00007, p: 0.4 },
    { c: [181, 211, 222], a: 0.7, r: 0.6, x: 0.72, y: 0.85, ax: 0.05, ay: 0.03, s: 0.00009, p: 2.2 },
    { c: [122, 182, 227], a: 0.5, r: 0.45, x: 0.55, y: 0.45, ax: 0.05, ay: 0.04, s: 0.00008, p: 4.0 },
  ];
  // the island: still, layered dark masses bottom-left
  const LAND = [
    { c: [50, 35, 117], r: 0.95, x: 0.12, y: 1.05, stop: 0.62 },
    { c: [73, 52, 154], r: 0.78, x: 0.02, y: 0.55, stop: 0.5 },
  ];

  let w = 0, h = 0, portrait = false;
  function resize() {
    w = Math.max(2, Math.ceil(window.innerWidth / 6));
    h = Math.max(2, Math.ceil(window.innerHeight / 6));
    portrait = window.innerWidth / window.innerHeight < 0.75;
    canvas.width = w;
    canvas.height = h;
  }

  function paint(t) {
    const d = Math.min(1, state.depth * 0.85 + state.full * 0.25);
    const dim = Math.max(w, h);

    const base = ctx.createLinearGradient(0, 0, w, h * 0.9);
    base.addColorStop(0, "#8F8FE0");
    base.addColorStop(1, "#A9AEEE");
    ctx.fillStyle = base;
    ctx.fillRect(0, 0, w, h);

    for (const b of FIELD) {
      const cx = (b.x + b.ax * Math.sin(t * b.s + b.p)) * w;
      const cy = (b.y + b.ay * Math.cos(t * b.s * 0.8 + b.p)) * h;
      const g = ctx.createRadialGradient(cx, cy, 0, cx, cy, b.r * dim);
      g.addColorStop(0, `rgb(${b.c[0]} ${b.c[1]} ${b.c[2]} / ${b.a})`);
      g.addColorStop(1, "rgb(255 255 255 / 0)");
      ctx.fillStyle = g;
      ctx.fillRect(0, 0, w, h);
    }

    // the island holds still
    for (const L of LAND) {
      const cx = (portrait ? 0.5 : L.x) * w;
      const cy = (portrait ? 1.35 : L.y) * h;
      const rr = L.r * (portrait ? 1.25 : 1) * dim;
      const g = ctx.createRadialGradient(cx, cy, 0, cx, cy, rr);
      g.addColorStop(0, `rgb(${L.c[0]} ${L.c[1]} ${L.c[2]})`);
      g.addColorStop(L.stop, `rgb(${L.c[0]} ${L.c[1]} ${L.c[2]} / 0.96)`);
      g.addColorStop(1, `rgb(${L.c[0]} ${L.c[1]} ${L.c[2]} / 0)`);
      ctx.fillStyle = g;
      ctx.fillRect(0, 0, w, h);
    }

    // depth dive
    if (d > 0.003) {
      ctx.fillStyle = `rgb(14 9 63 / ${d * 0.88})`;
      ctx.fillRect(0, 0, w, h);
    }
  }

  resize();
  paint(0);
  document.documentElement.classList.add("lit");
  canvas.classList.add("on");
  if (RM) {
    let queued = false;
    const still = () => {
      if (queued) return;
      queued = true;
      requestAnimationFrame(() => {
        queued = false;
        paint(0);
      });
    };
    window.addEventListener("scroll", still, { passive: true });
    window.addEventListener("resize", () => {
      resize();
      still();
    });
  } else {
    window.addEventListener("resize", resize);
    const loop = (t) => {
      paint(t);
      requestAnimationFrame(loop);
    };
    requestAnimationFrame(loop);
  }
  return true;
}

(() => {
  const canvas = document.getElementById("aurora");
  if (!canvas) return;
  let ok = false;
  try {
    ok = startAuroraGL(canvas);
  } catch {
    ok = false;
  }
  if (!ok) startAurora2D(canvas);
})();

/* -------------------------------------------------------------- nav glass */
(() => {
  const head = document.querySelector(".site-head");
  if (!head) return;
  let last = false;
  const check = () => {
    const on = window.scrollY > 24;
    if (on !== last) {
      last = on;
      head.classList.toggle("scrolled", on);
    }
  };
  check();
  window.addEventListener("scroll", check, { passive: true });
})();

/* ------------------------------------------------------------------ tabs */
(() => {
  const tabs = Array.from(document.querySelectorAll(".tab"));
  if (!tabs.length) return;

  function select(tab) {
    for (const t of tabs) {
      const on = t === tab;
      t.setAttribute("aria-selected", String(on));
      t.tabIndex = on ? 0 : -1;
      document.getElementById(t.getAttribute("aria-controls")).hidden = !on;
    }
  }

  tabs.forEach((tab, i) => {
    tab.addEventListener("click", () => select(tab));
    tab.addEventListener("keydown", (e) => {
      let j = null;
      if (e.key === "ArrowRight") j = (i + 1) % tabs.length;
      else if (e.key === "ArrowLeft") j = (i - 1 + tabs.length) % tabs.length;
      else if (e.key === "Home") j = 0;
      else if (e.key === "End") j = tabs.length - 1;
      if (j !== null) {
        e.preventDefault();
        tabs[j].focus();
        select(tabs[j]);
      }
    });
  });
})();

/* ------------------------------------------------------------------ copy */
(() => {
  for (const btn of document.querySelectorAll(".copy")) {
    btn.addEventListener("click", async () => {
      try {
        await navigator.clipboard.writeText(btn.dataset.copy);
      } catch {
        const ta = document.createElement("textarea");
        ta.value = btn.dataset.copy;
        ta.style.position = "fixed";
        ta.style.opacity = "0";
        document.body.appendChild(ta);
        ta.select();
        document.execCommand("copy");
        ta.remove();
      }
      btn.classList.add("done");
      const prev = btn.getAttribute("aria-label");
      btn.setAttribute("aria-label", "Copied");
      setTimeout(() => {
        btn.classList.remove("done");
        btn.setAttribute("aria-label", prev);
      }, 1800);
    });
  }
})();

/* ---------------------------------------------------------- stdout typing */
(() => {
  const out = document.getElementById("stdout");
  const demo = document.querySelector(".demo");
  if (!out || !demo || RM || !("IntersectionObserver" in window)) return;

  const full = out.textContent;
  const io = new IntersectionObserver(
    (entries) => {
      if (!entries[0].isIntersecting) return;
      io.disconnect();
      out.textContent = "";
      out.classList.add("typing");
      let i = 0;
      const tick = () => {
        i += 1;
        out.textContent = full.slice(0, i);
        if (i < full.length) setTimeout(tick, 26);
        else setTimeout(() => out.classList.remove("typing"), 1400);
      };
      setTimeout(tick, 420);
    },
    { threshold: 0.5 }
  );
  io.observe(demo);
})();

/* ------------------------------------------------- archipelago choreography */
(() => {
  const items = Array.from(document.querySelectorAll(".archipelago li"));
  if (!items.length) return;
  document.documentElement.classList.add("js");

  if (RM || !("IntersectionObserver" in window)) {
    items.forEach((li) => li.classList.add("inview"));
    return;
  }
  items.forEach((li, i) => li.style.setProperty("--i", String(i % 3)));
  const io = new IntersectionObserver(
    (entries) => {
      for (const e of entries) {
        if (e.isIntersecting) {
          e.target.classList.add("inview");
          io.unobserve(e.target);
        }
      }
    },
    { threshold: 0.25, rootMargin: "0px 0px -40px 0px" }
  );
  items.forEach((li) => io.observe(li));
  // safety net: content must never stay hidden if observation misfires
  setTimeout(() => items.forEach((li) => li.classList.add("inview")), 2500);
})();

/* ------------------------------------------------------------------ routes */
(() => {
  const svg = document.querySelector(".routes");
  const wrap = document.querySelector(".archipelago-wrap");
  if (!svg || !wrap) return;

  function draw() {
    if (window.innerWidth <= 900) {
      svg.replaceChildren();
      return;
    }
    const wr = wrap.getBoundingClientRect();
    const pts = Array.from(wrap.querySelectorAll(".isle")).map((el) => {
      const r = el.getBoundingClientRect();
      return [r.left - wr.left + r.width / 2, r.top - wr.top + r.height / 2];
    });
    svg.setAttribute("viewBox", `0 0 ${Math.round(wr.width)} ${Math.round(wr.height)}`);
    let d = "";
    for (let i = 0; i < pts.length - 1; i++) {
      const [x1, y1] = pts[i];
      const [x2, y2] = pts[i + 1];
      const mx = (x1 + x2) / 2;
      const my = (y1 + y2) / 2;
      const dx = x2 - x1;
      const dy = y2 - y1;
      const len = Math.hypot(dx, dy) || 1;
      const bow = (i % 2 ? -1 : 1) * Math.min(46, len * 0.16);
      const cx = mx + (-dy / len) * bow;
      const cy = my + (dx / len) * bow;
      d += `M ${x1} ${y1} Q ${cx} ${cy} ${x2} ${y2} `;
    }
    const path = document.createElementNS("http://www.w3.org/2000/svg", "path");
    path.setAttribute("d", d.trim());
    svg.replaceChildren(path);
  }

  if (document.fonts?.ready) document.fonts.ready.then(draw);
  draw();
  let raf = 0;
  new ResizeObserver(() => {
    cancelAnimationFrame(raf);
    raf = requestAnimationFrame(draw);
  }).observe(wrap);
})();
