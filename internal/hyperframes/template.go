package hyperframes

// htmlTemplate is the Go text/template for a HyperFrames HTML composition.
// It produces a self-contained index.html that:
//   - Shows each panel's image with Ken Burns / pan motion via GSAP
//   - Fades transitions between panels
//   - Displays per-panel subtitle lines with fade-in/out
//   - Registers window.__hf.seek so HyperFrames can drive the timeline
//
// No audio is embedded here — audio mixing is handled separately by FFmpeg.
const htmlTemplate = `<!DOCTYPE html>
<html lang="en">
<head>
  <meta charset="UTF-8">
  <title>{{.Title}}</title>
  <style>
    * { margin: 0; padding: 0; box-sizing: border-box; }
    body { width: {{.Width}}px; height: {{.Height}}px; overflow: hidden; background: #000; }
    .scene {
      position: absolute;
      width: {{.Width}}px; height: {{.Height}}px;
      overflow: hidden;
      background: #000;
      display: none;
      opacity: 0;
    }
    .bg-img {
      position: absolute;
      width: 100%; height: 100%;
      object-fit: cover;
      transform-origin: center center;
    }
    .vignette {
      position: absolute; inset: 0;
      background: radial-gradient(ellipse at 50% 50%, transparent 48%, rgba(0,0,0,0.42) 100%);
      pointer-events: none;
    }
    .grad-overlay {
      position: absolute; inset: 0;
      background: linear-gradient(to top, rgba(0,0,0,0.55) 0%, rgba(0,0,0,0) 46%);
      pointer-events: none;
    }
    .subtitle-wrap {
      position: absolute;
      bottom: 10%;
      width: 100%;
      text-align: center;
      pointer-events: none;
    }
    .subtitle-line {
      display: none;
      color: #fff;
      font-weight: 500;
      font-family: "Noto Sans TC", "PingFang TC", "Microsoft JhengHei", sans-serif;
      text-shadow: 0 2px 16px rgba(0,0,0,0.9), 0 0 36px rgba(0,0,0,0.6);
      line-height: 1.7;
      white-space: pre-wrap;
      letter-spacing: 0.05em;
      max-width: 78%;
      margin: 0 auto;
      opacity: 0;
    }
  </style>
  <script src="https://cdn.jsdelivr.net/npm/gsap@3/dist/gsap.min.js"></script>
</head>
<body>
  <div data-composition-id="short-drama"
       data-width="{{.Width}}"
       data-height="{{.Height}}"
       data-duration="{{printf "%.3f" .TotalDuration}}">
{{range .Panels}}    <div id="scene-{{.Index}}" class="scene">
      <img id="img-{{.Index}}" class="bg-img" src="{{.ImagePath}}" alt="">
      <div class="vignette"></div>
      <div class="grad-overlay"></div>
      <div class="subtitle-wrap">
{{$pi := .Index}}{{range $li, $line := .SubtitleLines}}        <div id="sub-{{$pi}}-{{$li}}" class="subtitle-line" style="font-size: 38px;">{{$line.Text}}</div>
{{end}}      </div>
    </div>
{{end}}  </div>
  <script>
  (function () {
    var tl = gsap.timeline({ paused: true });
{{range .Panels}}    (function () {
      var sc  = document.getElementById("scene-{{.Index}}");
      var im  = document.getElementById("img-{{.Index}}");
      var s   = {{printf "%.3f" .StartSec}};
      var dur = {{printf "%.3f" .DurationSec}};
      var td  = {{printf "%.3f" (divBy .TransitionInMS 1000.0)}};

      tl.set(sc, { display: "block", opacity: 0 }, s);
      tl.to(sc,  { opacity: 1, duration: td, ease: "power2.inOut" }, s);
{{if eq .MotionEffect "ken_burns_in"}}      tl.fromTo(im,
        { scale: 1.0, transformOrigin: "center center" },
        { scale: {{printf "%.4f" (addFloat 1.0 .MotionIntensity)}}, duration: dur, ease: "none" },
        s);
{{else if eq .MotionEffect "ken_burns_out"}}      tl.fromTo(im,
        { scale: {{printf "%.4f" (addFloat 1.0 .MotionIntensity)}}, transformOrigin: "center center" },
        { scale: 1.0, duration: dur, ease: "none" },
        s);
{{else if eq .MotionEffect "pan_left"}}      tl.fromTo(im,
        { x: 0, scale: 1.06 },
        { x: "-{{printf "%.1f" (mulFloat .MotionIntensity 100.0)}}%", duration: dur, ease: "none" },
        s);
{{else if eq .MotionEffect "pan_right"}}      tl.fromTo(im,
        { x: 0, scale: 1.06 },
        { x: "{{printf "%.1f" (mulFloat .MotionIntensity 100.0)}}%", duration: dur, ease: "none" },
        s);
{{end}}      tl.to(sc,  { opacity: 0, duration: td, ease: "power2.inOut" }, s + dur - td);
      tl.set(sc, { display: "none" }, s + dur);
{{$pi := .Index}}{{range $li, $line := .SubtitleLines}}      (function () {
        var sub = document.getElementById("sub-{{$pi}}-{{$li}}");
        var ss  = s + {{printf "%.3f" $line.StartSec}};
        var se  = s + {{printf "%.3f" $line.EndSec}};
        tl.set(sub, { display: "block", opacity: 0 }, ss);
        tl.to(sub,  { opacity: 1, duration: 0.22, ease: "power2.out" }, ss);
        tl.to(sub,  { opacity: 0, duration: 0.15, ease: "power2.in" }, se - 0.15);
        tl.set(sub, { display: "none", opacity: 0 }, se);
      }());
{{end}}    }());
{{end}}    window.__hf = { seek: function (t) { tl.seek(t, false); } };
  }());
  </script>
</body>
</html>`
