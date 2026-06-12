#!/usr/bin/env node
const fs = require("fs");
const path = require("path");

function usage() {
  console.error("usage: build_video_timeline.js <output-dir> <video:seconds> [<video:seconds> ...]");
  process.exit(64);
}

if (process.argv.length < 5) usage();

const outputDir = process.argv[2];
const items = process.argv.slice(3).map((item) => {
  const idx = item.lastIndexOf(":");
  if (idx < 1) usage();
  const video = item.slice(0, idx);
  const duration = Number(item.slice(idx + 1));
  if (!Number.isFinite(duration) || duration <= 0) usage();
  return { video, duration };
});

const hfDir = path.join(outputDir, "hyperframes_i2v");
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
const videos = items.map((item, i) => {
  const rel = path.relative(hfDir, item.video).split(path.sep).join("/");
  const html = `    <video id="video-${i}" class="shot-video clip" src="${rel}" data-start="${start.toFixed(3)}" data-duration="${item.duration.toFixed(3)}" data-has-audio="true" preload="auto" playsinline></video>`;
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
  <title>xAI Grok I2V Timeline</title>
  <style>
    * { box-sizing: border-box; }
    html, body { margin: 0; width: 720px; height: 1280px; overflow: hidden; background: #0f1712; }
    #i2v-story { position: relative; width: 720px; height: 1280px; overflow: hidden; background: #0f1712; }
    .shot-video { position: absolute; inset: 0; width: 720px; height: 1280px; object-fit: cover; opacity: 0; display: none; }
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
  <div id="i2v-story" data-composition-id="i2v-story" data-start="0" data-width="720" data-height="1280" data-duration="${start.toFixed(3)}">
${videos.join("\n")}
${subtitles.join("\n")}
  </div>
  <script>
    (function () {
      var videos = Array.prototype.slice.call(document.querySelectorAll('.shot-video')).map(function (el) {
        return { el: el, start: Number(el.dataset.start), duration: Number(el.dataset.duration) };
      });
      var subtitles = Array.prototype.slice.call(document.querySelectorAll('.subtitle')).map(function (el) {
        return { el: el, start: Number(el.dataset.start), duration: Number(el.dataset.duration) };
      });
      function seek(t) {
        t = Number(t) || 0;
        videos.forEach(function (shot) {
          var local = t - shot.start;
          var active = local >= 0 && local < shot.duration;
          shot.el.style.display = active ? 'block' : 'none';
          shot.el.style.opacity = active ? '1' : '0';
          if (active) {
            var target = Math.max(0, Math.min(local, Math.max(0, shot.duration - 0.001)));
            if (Math.abs(shot.el.currentTime - target) > 0.03) {
              shot.el.currentTime = target;
            }
          }
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
      window.__timelines['i2v-story'] = timeline;
      window.__hf = { seek: timeline.seek, pause: timeline.pause };
      seek(0);
    }());
  </script>
</body>
</html>`;

fs.writeFileSync(path.join(hfDir, "index.html"), html);
fs.writeFileSync(
  path.join(hfDir, "package.json"),
  JSON.stringify({ name: "xai-grok-i2v-timeline", version: "1.0.0", private: true }, null, 2) + "\n",
);

console.log(JSON.stringify({
  hyperframes_dir: hfDir,
  index: path.join(hfDir, "index.html"),
  duration_seconds: Number(start.toFixed(3)),
  videos: items.length,
  subtitles: subtitles.length,
}));
