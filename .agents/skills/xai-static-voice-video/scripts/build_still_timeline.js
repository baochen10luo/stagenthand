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

let start = 0;
const scenes = items.map((item, i) => {
  const rel = path.relative(hfDir, item.image).split(path.sep).join("/");
  const html = `    <img id="scene-${i}" class="scene clip" src="${rel}" data-start="${start.toFixed(3)}" data-duration="${item.duration.toFixed(3)}" alt="scene ${i + 1}">`;
  start += item.duration;
  return html;
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
  </style>
</head>
<body>
  <div id="static-story" data-composition-id="static-story" data-start="0" data-width="720" data-height="1280" data-duration="${start.toFixed(3)}">
${scenes.join("\n")}
  </div>
  <script>
    (function () {
      var scenes = Array.prototype.slice.call(document.querySelectorAll('.scene')).map(function (el) {
        return { el: el, start: Number(el.dataset.start), duration: Number(el.dataset.duration) };
      });
      function seek(t) {
        t = Number(t) || 0;
        scenes.forEach(function (scene) {
          var active = t >= scene.start && t < scene.start + scene.duration;
          scene.el.style.display = active ? 'block' : 'none';
          scene.el.style.opacity = active ? '1' : '0';
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
}));
