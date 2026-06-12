#!/usr/bin/env node
const fs = require("fs");
const path = require("path");

function usage() {
  console.error("usage: build_still_timeline.js <output-dir> <image:seconds> [<image:seconds> ...]");
  process.exit(64);
}

if (process.argv.length < 5) usage();

const outputDir = process.argv[2];
const items = process.argv.slice(3).map((item) => {
  const idx = item.lastIndexOf(":");
  if (idx < 1) usage();
  const image = item.slice(0, idx);
  const duration = Number(item.slice(idx + 1));
  if (!Number.isFinite(duration) || duration <= 0) usage();
  return { image, duration };
});

const hfDir = path.join(outputDir, "hyperframes");
fs.mkdirSync(hfDir, { recursive: true });

function escapeHTML(value) {
  return String(value)
    .replace(/&/g, "&amp;")
    .replace(/</g, "&lt;")
    .replace(/>/g, "&gt;")
    .replace(/"/g, "&quot;");
}

function assTimeToSeconds(value) {
  const match = String(value).trim().match(/^(\d+):(\d{2}):(\d{2})(?:\.(\d{1,3}))?$/);
  if (!match) return NaN;
  const frac = (match[4] || "").padEnd(3, "0").slice(0, 3);
  return Number(match[1]) * 3600 + Number(match[2]) * 60 + Number(match[3]) + Number(frac || 0) / 1000;
}

function readASSSubtitles(file) {
  if (!fs.existsSync(file)) return [];
  return fs.readFileSync(file, "utf8")
    .split(/\r?\n/)
    .filter((line) => line.startsWith("Dialogue:"))
    .map((line) => {
      const body = line.replace(/^Dialogue:\s*/, "");
      const parts = body.split(",");
      if (parts.length < 10) return null;
      const start = assTimeToSeconds(parts[1]);
      const end = assTimeToSeconds(parts[2]);
      const text = parts.slice(9).join(",").replace(/\\N/g, "\n").trim();
      if (!Number.isFinite(start) || !Number.isFinite(end) || end <= start || !text) return null;
      return { start, duration: end - start, text };
    })
    .filter(Boolean);
}

let start = 0;
const scenes = items.map((item, i) => {
  const rel = path.relative(hfDir, item.image).split(path.sep).join("/");
  const html = `    <img id="scene-${i}" class="scene clip" src="${rel}" data-start="${start.toFixed(3)}" data-duration="${item.duration.toFixed(3)}" alt="scene ${i + 1}">`;
  start += item.duration;
  return html;
});
const subtitles = readASSSubtitles(path.join(outputDir, "subtitles.ass")).map((subtitle, i) => {
  return `    <div id="subtitle-${i}" class="subtitle clip" data-start="${subtitle.start.toFixed(3)}" data-duration="${subtitle.duration.toFixed(3)}">${escapeHTML(subtitle.text)}</div>`;
});

const html = `<!DOCTYPE html>
<html lang="zh-Hant">
<head>
  <meta charset="UTF-8">
  <title>xAI Grok Static Timeline</title>
  <style>
    * { box-sizing: border-box; }
    html, body { margin: 0; width: 720px; height: 1280px; overflow: hidden; background: #0f1712; }
    #static-story { position: relative; width: 720px; height: 1280px; overflow: hidden; background: #0f1712; }
    .scene { position: absolute; inset: 0; width: 720px; height: 1280px; object-fit: cover; opacity: 0; display: none; }
    .subtitle {
      position: absolute;
      left: 48px;
      right: 48px;
      bottom: 92px;
      display: none;
      opacity: 0;
      padding: 10px 18px;
      border-radius: 8px;
      background: rgba(16, 23, 18, 0.64);
      color: white;
      font-family: "Noto Sans TC", sans-serif;
      font-size: 48px;
      font-weight: 700;
      line-height: 1.25;
      text-align: center;
      text-shadow: 0 2px 6px rgba(0, 0, 0, 0.86);
    }
  </style>
</head>
<body>
  <div id="static-story" data-composition-id="static-story" data-start="0" data-width="720" data-height="1280" data-duration="${start.toFixed(3)}">
${scenes.join("\n")}
${subtitles.join("\n")}
  </div>
  <script>
    (function () {
      var scenes = Array.prototype.slice.call(document.querySelectorAll('.scene')).map(function (el) {
        return { el: el, start: Number(el.dataset.start), duration: Number(el.dataset.duration) };
      });
      var subtitles = Array.prototype.slice.call(document.querySelectorAll('.subtitle')).map(function (el) {
        return { el: el, start: Number(el.dataset.start), duration: Number(el.dataset.duration) };
      });
      function seek(t) {
        t = Number(t) || 0;
        scenes.forEach(function (scene) {
          var active = t >= scene.start && t < scene.start + scene.duration;
          scene.el.style.display = active ? 'block' : 'none';
          scene.el.style.opacity = active ? '1' : '0';
        });
        subtitles.forEach(function (subtitle) {
          var active = t >= subtitle.start && t < subtitle.start + subtitle.duration;
          subtitle.el.style.display = active ? 'block' : 'none';
          subtitle.el.style.opacity = active ? '1' : '0';
        });
      }
      var timeline = {
        seek: function (t) { seek(t); return timeline; },
        pause: function () { return timeline; }
      };
      window.__timelines = window.__timelines || {};
      window.__timelines['static-story'] = timeline;
      window.__hf = { seek: timeline.seek, pause: timeline.pause };
      seek(0);
    }());
  </script>
</body>
</html>`;

fs.writeFileSync(path.join(hfDir, "index.html"), html);
fs.writeFileSync(
  path.join(hfDir, "package.json"),
  JSON.stringify({ name: "xai-grok-static-timeline", version: "1.0.0", private: true }, null, 2) + "\n",
);

console.log(JSON.stringify({
  hyperframes_dir: hfDir,
  index: path.join(hfDir, "index.html"),
  duration_seconds: Number(start.toFixed(3)),
  scenes: items.length,
  subtitles: subtitles.length,
}));
