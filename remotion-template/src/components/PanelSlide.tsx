import {
  AbsoluteFill,
  Audio,
  Img,
  interpolate,
  staticFile,
  useCurrentFrame,
  useVideoConfig,
} from "remotion";
import type { DialogueLine, Panel, PanelDirective } from "../types";

const huninnFont = "ZCOOLQingKeHuangYou";

// ─── Painted Sky Background ───
const PaintedSky: React.FC = () => (
  <AbsoluteFill>
    {/* Sky base gradient */}
    <div style={{ position: "absolute", inset: 0, background: "linear-gradient(to bottom, #4DAEC8 0%, #7ECFE0 28%, #B8E4EE 50%, #EEF5E8 78%, #F5EDE0 100%)" }} />
    {/* Main cloud mass - upper center */}
    <div style={{ position: "absolute", top: "6%", left: "8%", width: "84%", height: "42%", background: "radial-gradient(ellipse at 45% 55%, rgba(255,255,255,0.96) 0%, rgba(255,250,240,0.75) 38%, transparent 68%)" }} />
    {/* Secondary cloud right */}
    <div style={{ position: "absolute", top: "10%", left: "42%", width: "58%", height: "36%", background: "radial-gradient(ellipse at 35% 45%, rgba(255,255,255,0.88) 0%, rgba(255,248,235,0.55) 45%, transparent 72%)" }} />
    {/* Warm pink glow in clouds */}
    <div style={{ position: "absolute", top: "14%", left: "18%", width: "55%", height: "28%", background: "radial-gradient(ellipse at 55% 60%, rgba(255,200,160,0.35) 0%, transparent 62%)" }} />
    {/* Light blue haze left */}
    <div style={{ position: "absolute", top: "20%", left: "0%", width: "40%", height: "30%", background: "radial-gradient(ellipse at 70% 40%, rgba(180,225,240,0.45) 0%, transparent 65%)" }} />
    {/* Bottom warm haze */}
    <div style={{ position: "absolute", bottom: "0%", left: "0%", width: "100%", height: "35%", background: "linear-gradient(to top, rgba(245,220,195,0.5) 0%, transparent 100%)" }} />
  </AbsoluteFill>
);

// ─── Emoji Decorations ───
// Each emoji: x/y position (%), size, rotation, phase offsets, amplitude
const EMOJI_SETS = [
  [
    { e: "✨", x: 6,  y: 4,  s: 68, r: 15,  py: 0.0, px: 0.5, fy: 0.6, fx: 0.3, ay: 7, ax: 3 },
    { e: "💛", x: 82, y: 6,  s: 58, r: -12, py: 1.1, px: 0.2, fy: 0.9, fx: 0.4, ay: 9, ax: 4 },
    { e: "🌈", x: 3,  y: 20, s: 74, r: 0,   py: 2.0, px: 0.8, fy: 0.5, fx: 0.7, ay: 6, ax: 5 },
    { e: "🎊", x: 76, y: 24, s: 60, r: 10,  py: 0.7, px: 1.4, fy: 1.1, fx: 0.5, ay: 8, ax: 3 },
    { e: "💕", x: 86, y: 54, s: 56, r: -8,  py: 1.6, px: 0.3, fy: 0.7, fx: 0.9, ay: 7, ax: 4 },
    { e: "🌸", x: 4,  y: 60, s: 64, r: -6,  py: 0.4, px: 1.0, fy: 0.8, fx: 0.6, ay: 9, ax: 5 },
    { e: "⭐", x: 80, y: 77, s: 52, r: 20,  py: 2.3, px: 0.6, fy: 1.3, fx: 0.4, ay: 6, ax: 3 },
    { e: "✨", x: 10, y: 80, s: 48, r: 8,   py: 1.2, px: 1.8, fy: 0.6, fx: 1.0, ay: 8, ax: 4 },
    { e: "🎈", x: 46, y: 1,  s: 72, r: 5,   py: 0.9, px: 0.4, fy: 1.0, fx: 0.3, ay:10, ax: 5 },
    { e: "💫", x: 91, y: 38, s: 54, r: -15, py: 1.5, px: 0.7, fy: 0.7, fx: 0.8, ay: 7, ax: 3 },
  ],
  [
    { e: "🎉", x: 5,  y: 5,  s: 66, r: -10, py: 0.3, px: 0.9, fy: 0.8, fx: 0.5, ay: 8, ax: 4 },
    { e: "🌼", x: 80, y: 7,  s: 60, r: 8,   py: 1.4, px: 0.2, fy: 1.1, fx: 0.3, ay: 7, ax: 5 },
    { e: "✨", x: 88, y: 26, s: 50, r: 18,  py: 2.1, px: 1.1, fy: 0.6, fx: 0.7, ay: 9, ax: 3 },
    { e: "💖", x: 4,  y: 32, s: 58, r: -5,  py: 0.6, px: 1.6, fy: 0.9, fx: 0.4, ay: 6, ax: 4 },
    { e: "🎀", x: 76, y: 58, s: 56, r: 12,  py: 1.8, px: 0.5, fy: 0.7, fx: 0.9, ay: 8, ax: 5 },
    { e: "🌟", x: 7,  y: 66, s: 64, r: -8,  py: 0.5, px: 1.3, fy: 1.0, fx: 0.6, ay: 7, ax: 3 },
    { e: "💐", x: 83, y: 80, s: 58, r: 6,   py: 2.4, px: 0.8, fy: 0.8, fx: 0.4, ay: 9, ax: 4 },
    { e: "🎊", x: 48, y: 2,  s: 70, r: -3,  py: 1.0, px: 0.3, fy: 1.2, fx: 0.7, ay: 8, ax: 5 },
    { e: "💝", x: 89, y: 46, s: 52, r: 15,  py: 1.7, px: 1.2, fy: 0.6, fx: 0.5, ay: 7, ax: 3 },
    { e: "🌺", x: 8,  y: 84, s: 60, r: -10, py: 0.8, px: 0.6, fy: 0.9, fx: 0.8, ay: 6, ax: 4 },
  ],
];

const EmojiDecorations: React.FC<{ frame: number; fps: number; setIndex: number }> = ({ frame, fps, setIndex }) => {
  const set = EMOJI_SETS[setIndex % EMOJI_SETS.length];
  const t = frame / fps;
  return (
    <>
      {set.map((d, i) => {
        // Irregular: two sin waves at different frequencies combined
        const floatY = Math.sin(t * Math.PI * d.fy + d.py) * d.ay
                     + Math.sin(t * Math.PI * d.fy * 1.63 + d.py * 0.7) * (d.ay * 0.45);
        const floatX = Math.sin(t * Math.PI * d.fx + d.px) * d.ax
                     + Math.sin(t * Math.PI * d.fx * 2.1 + d.px * 1.3) * (d.ax * 0.3);
        // Irregular scale pulse
        const scale = 1 + Math.sin(t * Math.PI * 0.8 + d.py * 1.2) * 0.1
                        + Math.sin(t * Math.PI * 1.7 + d.px) * 0.05;
        const appear = interpolate(frame, [i * 2, i * 2 + 10], [0, 1], { extrapolateRight: "clamp", extrapolateLeft: "clamp" });
        return (
          <div key={i} style={{
            position: "absolute",
            left: `${d.x + floatX}%`,
            top: `${d.y + floatY}%`,
            fontSize: d.s,
            transform: `rotate(${d.r}deg) scale(${scale})`,
            opacity: appear * 0.92,
            userSelect: "none",
            pointerEvents: "none",
            lineHeight: 1,
          }}>{d.e}</div>
        );
      })}
    </>
  );
};

const COLOR_FILTERS: Record<string, string> = {
  none: "none",
  cinematic: "contrast(1.1) saturate(0.85) brightness(0.95) sepia(0.1)",
  vintage: "sepia(0.4) contrast(1.05) brightness(0.9) saturate(0.7)",
  cyberpunk: "contrast(1.2) saturate(1.4) hue-rotate(10deg) brightness(0.9)",
};

const D: Required<PanelDirective> = {
  motion_effect: "ken_burns_in",
  motion_intensity: 0.05,
  transition_in: "fade",
  transition_out: "fade",
  transition_duration_ms: 400,
  subtitle_effect: "fade",
  subtitle_font_size: 38,
  subtitle_position: "bottom",
  bg_style: "fullbleed",
  text_x: "center",
  text_y: "bottom",
  text_rotate: 0,
  show_particles: false,
  show_light_leak: false,
};

function d(panel: Panel): Required<PanelDirective> {
  return { ...D, ...(panel.directive ?? {}) };
}

// ─── Floating Hearts ───
const FloatingHearts: React.FC<{ frame: number; fps: number; durationFrames: number }> = ({ frame, fps, durationFrames }) => {
  const hearts = [
    { x: 12, delay: 0.0, size: 15, op: 0.35 },
    { x: 78, delay: 0.9, size: 11, op: 0.28 },
    { x: 88, delay: 1.6, size: 17, op: 0.32 },
    { x: 22, delay: 2.3, size: 10, op: 0.22 },
    { x: 62, delay: 0.4, size: 13, op: 0.30 },
  ];
  return (
    <>
      {hearts.map((h, i) => {
        const sf = Math.round(h.delay * fps);
        const ef = durationFrames;
        const y = interpolate(frame, [sf, ef], [88, 15], { extrapolateLeft: "clamp", extrapolateRight: "clamp" });
        const opacity = interpolate(
          frame,
          [sf, sf + Math.round(0.4 * fps), ef - Math.round(fps * 0.8), ef],
          [0, h.op, h.op, 0],
          { extrapolateLeft: "clamp", extrapolateRight: "clamp" }
        );
        return (
          <div key={i} style={{
            position: "absolute", left: `${h.x}%`, top: `${y}%`,
            fontSize: h.size, opacity, color: "#FFB6C1", pointerEvents: "none",
            userSelect: "none",
          }}>♥</div>
        );
      })}
    </>
  );
};

// ─── Film Grain ───
const FilmGrain: React.FC<{ seed: number }> = ({ seed }) => (
  <AbsoluteFill style={{ pointerEvents: "none", opacity: 0.045, mixBlendMode: "overlay" }}>
    <svg width="100%" height="100%" style={{ position: "absolute" }}>
      <filter id={`grain-${seed}`}>
        <feTurbulence type="fractalNoise" baseFrequency="0.88" numOctaves="4" seed={seed} stitchTiles="stitch" result="noise" />
        <feColorMatrix type="saturate" values="0" in="noise" />
      </filter>
      <rect width="100%" height="100%" filter={`url(#grain-${seed})`} fill="white" />
    </svg>
  </AbsoluteFill>
);

// ─── Vignette ───
const Vignette: React.FC = () => (
  <AbsoluteFill style={{
    background: "radial-gradient(ellipse at 50% 50%, transparent 48%, rgba(0,0,0,0.42) 100%)",
    pointerEvents: "none",
  }} />
);

export const PanelSlide: React.FC<{ panel: Panel; colorFilter?: string }> = ({ panel, colorFilter }) => {
  const frame = useCurrentFrame();
  const { fps } = useVideoConfig();
  const dir = d(panel);
  const durationFrames = Math.round(panel.duration_sec * fps);

  // ─── Transition opacity ───
  const transFrames = Math.round((dir.transition_duration_ms / 1000) * fps);
  const inOp = (dir.transition_in === "fade" || dir.transition_in === "dissolve")
    ? interpolate(frame, [0, transFrames], [0, 1], { extrapolateRight: "clamp", extrapolateLeft: "clamp" })
    : 1;
  const outOp = (dir.transition_out === "fade" || dir.transition_out === "dissolve")
    ? interpolate(frame, [durationFrames - transFrames, durationFrames], [1, 0], { extrapolateRight: "clamp", extrapolateLeft: "clamp" })
    : 1;
  const opacity = Math.min(inOp, outOp);

  // ─── Clip path (wipe) ───
  let clipPath: string | undefined;
  if (dir.transition_in === "wipe_left") {
    const wp = interpolate(frame, [0, transFrames], [0, 100], { extrapolateRight: "clamp", extrapolateLeft: "clamp" });
    clipPath = `inset(0 ${100 - wp}% 0 0)`;
  }

  // ─── Camera motion ───
  const intensity = dir.motion_intensity;
  let imgTransform = "none";
  if (dir.motion_effect === "ken_burns_in") {
    const s = interpolate(frame, [0, durationFrames], [1.0, 1.0 + intensity], { extrapolateRight: "clamp", extrapolateLeft: "clamp" });
    imgTransform = `scale(${s})`;
  } else if (dir.motion_effect === "ken_burns_out") {
    const s = interpolate(frame, [0, durationFrames], [1.0 + intensity, 1.0], { extrapolateRight: "clamp", extrapolateLeft: "clamp" });
    imgTransform = `scale(${s})`;
  } else if (dir.motion_effect === "pan_left") {
    const tx = interpolate(frame, [0, durationFrames], [0, -(intensity * 100)], { extrapolateRight: "clamp", extrapolateLeft: "clamp" });
    imgTransform = `translateX(${tx}%)`;
  } else if (dir.motion_effect === "pan_right") {
    const tx = interpolate(frame, [0, durationFrames], [0, intensity * 100], { extrapolateRight: "clamp", extrapolateLeft: "clamp" });
    imgTransform = `translateX(${tx}%)`;
  }

  // ─── Subtitle logic ───
  const sanitize = (text: string) => {
    if (!text) return "";
    let c = text.trim();
    c = c.replace(/^(?:VO|V\.O\.|Narrator|Voiceover|\[.*?\])\s*[:\-]*\s*/i, "");
    c = c.replace(/^["']+(.*?)["']+$/s, "$1");
    return c.trim();
  };

  const getActiveLine = (lines: DialogueLine[] | undefined): string => {
    if (!lines || lines.length === 0) return "";
    const sec = frame / fps;
    for (const l of lines) {
      if (sec >= (l.start_sec ?? 0) && sec < (l.end_sec ?? durationFrames / fps)) return sanitize(l.text);
    }
    // Only fall back to last line if we're past its end_sec (lingering display)
    // Never show text before the first line's start_sec
    const firstStart = lines[0]?.start_sec ?? 0;
    if (sec < firstStart) return "";
    const last = lines[lines.length - 1];
    const lastEnd = last?.end_sec;
    if (last && lastEnd !== undefined && sec >= lastEnd) return sanitize(last.text);
    return "";
  };

  const hasTimedLines = (panel.dialogue_lines?.length ?? 0) > 0 && panel.dialogue_lines?.[0]?.start_sec !== undefined;
  const delay = Math.round(0.08 * fps);
  const rawText = hasTimedLines ? getActiveLine(panel.dialogue_lines) : sanitize(panel.dialogue);
  let subtitleText = rawText;

  let subOp = 1;
  if (dir.subtitle_effect === "fade") {
    subOp = interpolate(frame, [delay, delay + Math.round(0.22 * fps)], [0, 1], { extrapolateRight: "clamp", extrapolateLeft: "clamp" });
  } else if (dir.subtitle_effect === "typewriter" && rawText) {
    const chars = Math.round(interpolate(frame, [delay, delay + Math.round(rawText.length * 0.1 * fps)], [0, rawText.length], { extrapolateRight: "clamp", extrapolateLeft: "clamp" }));
    subtitleText = rawText.substring(0, chars);
  }

  // Underline expand animation
  const underlineW = interpolate(frame, [delay + Math.round(0.2 * fps), delay + Math.round(0.65 * fps)], [0, 100], { extrapolateRight: "clamp", extrapolateLeft: "clamp" });

  // Light leak
  const lightOp = dir.show_light_leak
    ? interpolate(frame, [0, Math.round(0.25 * fps), Math.round(0.55 * fps), Math.round(0.9 * fps)], [0, 0.3, 0.15, 0], { extrapolateRight: "clamp", extrapolateLeft: "clamp" })
    : 0;

  const filterCSS = COLOR_FILTERS[colorFilter ?? "none"] ?? "none";
  const fontFamily = `${huninnFont}, "PingFang TC", "Noto Sans TC", sans-serif`;
  const fontSize = dir.subtitle_font_size;
  const washiColors = ["#F4C2C2", "#FFE4A0", "#B8DDD0"];
  const washiColor = washiColors[panel.panel_number % 3];

  // ─── Dynamic text position ───
  // Gradient bg = always light background → dark text
  // Fullbleed/card = depends on position
  const textColor = "#FFFFFF";
  const textShadowStyle = "0 2px 16px rgba(0,0,0,0.9), 0 0 36px rgba(0,0,0,0.6)";

  const buildTextStyle = (): React.CSSProperties => {
    const base: React.CSSProperties = {
      position: "absolute",
      opacity: subOp,
      color: textColor,
      fontSize,
      fontWeight: 500,
      fontFamily,
      textAlign: dir.text_x === "left" ? "left" : dir.text_x === "right" ? "right" : "center",
      maxWidth: "78%",
      textShadow: textShadowStyle,
      lineHeight: 1.7,
      whiteSpace: "pre-wrap",
      letterSpacing: "0.05em",
    };
    // X
    if (dir.text_x === "left") { base.left = "8%"; base.transform = `rotate(${dir.text_rotate}deg)`; }
    else if (dir.text_x === "right") { base.right = "8%"; base.transform = `rotate(${dir.text_rotate}deg)`; }
    else { base.left = "50%"; base.transform = `translateX(-50%) rotate(${dir.text_rotate}deg)`; }
    // Y
    if (dir.text_y === "top") base.top = "10%";
    else if (dir.text_y === "middle") base.top = "40%";
    else base.bottom = "10%";
    return base;
  };

  const TextNode = subtitleText ? (
    <div style={buildTextStyle()}>
      {subtitleText}
      <div style={{
        height: 1,
        backgroundColor: "rgba(255,255,255,0.55)",
        width: `${underlineW}%`,
        margin: "5px auto 0",
      }} />
    </div>
  ) : null;

  // ─── Card text: lighter shadow, darker color for cream bg ───
  const buildCardTextStyle = (): React.CSSProperties => ({
    position: "absolute",
    bottom: "18%",
    left: "50%",
    transform: `translateX(-50%) rotate(${dir.text_rotate}deg)`,
    opacity: subOp,
    color: "#FFFFFF",
    fontSize: fontSize * 0.84,
    fontWeight: 500,
    fontFamily,
    textAlign: "center",
    maxWidth: "75%",
    textShadow: "0 2px 10px rgba(0,0,0,0.75)",
    lineHeight: 1.6,
    whiteSpace: "pre-wrap",
    letterSpacing: "0.04em",
  });

  const CardTextNode = subtitleText ? (
    <div style={buildCardTextStyle()}>
      {subtitleText}
    </div>
  ) : null;

  return (
    <AbsoluteFill style={{ opacity, clipPath }}>

      {/* ══════════════ LETTERBOX: CREAM / POLAROID ══════════════ */}
      {dir.bg_style === "card" && panel.image_url && (
        <>
          <AbsoluteFill style={{ backgroundColor: "#FAF5EE" }} />
          <AbsoluteFill style={{
            backgroundImage: "radial-gradient(circle, rgba(0,0,0,0.05) 1px, transparent 1px)",
            backgroundSize: "30px 30px", pointerEvents: "none",
          }} />
          <AbsoluteFill style={{ display: "flex", alignItems: "center", justifyContent: "center" }}>
            <div style={{
              width: "80%", backgroundColor: "#FFFFFF", borderRadius: 4,
              boxShadow: "0 10px 40px rgba(0,0,0,0.14), 0 2px 8px rgba(0,0,0,0.08)",
              position: "relative", transform: imgTransform, transformOrigin: "center center",
            }}>
              <div style={{ position: "absolute", top: -16, left: "50%", transform: "translateX(-50%) rotate(-2deg)", width: 72, height: 26, backgroundColor: washiColor, opacity: 0.88, zIndex: 10, borderRadius: 3 }} />
              <div style={{ width: "100%", aspectRatio: "3/4", overflow: "hidden", borderRadius: "4px 4px 0 0" }}>
                <Img src={staticFile(panel.image_url)} style={{ width: "100%", height: "100%", objectFit: "cover", filter: filterCSS }} />
              </div>
              <div style={{ padding: "10px 18px 14px", display: "flex", justifyContent: "flex-end" }}>
                <span style={{ fontSize: 15, color: "#BBBBBB", fontFamily, letterSpacing: "0.07em" }}>'26.03.30</span>
              </div>
            </div>
          </AbsoluteFill>
          {CardTextNode}
        </>
      )}

      {/* ══════════════ LETTERBOX: BLUR BACKGROUND ══════════════ */}
      {dir.bg_style === "blur" && panel.image_url && (
        <>
          {/* Blurred full-size background */}
          <AbsoluteFill>
            <Img src={staticFile(panel.image_url)} style={{ width: "100%", height: "100%", objectFit: "cover", filter: `blur(24px) brightness(0.6) saturate(1.2) ${filterCSS}`, transform: "scale(1.08)" }} />
          </AbsoluteFill>
          {/* Sharp photo inset */}
          <AbsoluteFill style={{ display: "flex", alignItems: "center", justifyContent: "center" }}>
            <div style={{
              width: "92%", borderRadius: 12,
              overflow: "hidden",
              boxShadow: "0 16px 48px rgba(0,0,0,0.4)",
              transform: imgTransform, transformOrigin: "center center",
            }}>
              <Img src={staticFile(panel.image_url)} style={{ width: "100%", display: "block", filter: filterCSS }} />
            </div>
          </AbsoluteFill>
          {TextNode}
        </>
      )}

      {/* ══════════════ LETTERBOX: PAINTED SKY + EMOJI ══════════════ */}
      {dir.bg_style === "gradient" && panel.image_url && (
        <>
          <PaintedSky />
          <EmojiDecorations frame={frame} fps={fps} setIndex={panel.scene_number} />
          {/* Photo inset with white border, slight tilt */}
          <AbsoluteFill style={{ display: "flex", alignItems: "center", justifyContent: "center" }}>
            <div style={{
              width: "84%",
              padding: 10,
              backgroundColor: "#FFFFFF",
              borderRadius: 6,
              boxShadow: "0 10px 40px rgba(0,0,0,0.18), 0 2px 10px rgba(0,0,0,0.1)",
              transform: `${imgTransform} rotate(${panel.panel_number % 2 === 0 ? 1.5 : -1.5}deg)`,
              transformOrigin: "center center",
            }}>
              <Img src={staticFile(panel.image_url)} style={{ width: "100%", display: "block", borderRadius: 3, filter: filterCSS }} />
            </div>
          </AbsoluteFill>
          {/* Text in sky area — dark color */}
          {TextNode}
        </>
      )}

      {/* ══════════════ FULLBLEED ══════════════ */}
      {dir.bg_style === "fullbleed" && panel.image_url && (
        <>
          <AbsoluteFill style={{ backgroundColor: "#000" }}>
            <AbsoluteFill style={{ transform: imgTransform, transformOrigin: "center center", filter: filterCSS }}>
              <Img src={staticFile(panel.image_url)} style={{ width: "100%", height: "100%", objectFit: "cover" }} />
            </AbsoluteFill>
            <Vignette />
            {/* Directional gradient based on text position */}
            <AbsoluteFill style={{
              background: dir.text_y === "top"
                ? "linear-gradient(to bottom, rgba(255,255,255,0.22) 0%, rgba(255,255,255,0) 38%)"
                : "linear-gradient(to top, rgba(0,0,0,0.55) 0%, rgba(0,0,0,0) 46%)",
              pointerEvents: "none",
            }} />
            {/* Light leak flash */}
            {lightOp > 0 && (
              <AbsoluteFill style={{
                background: "linear-gradient(130deg, rgba(255,215,130,0.9) 0%, rgba(255,180,80,0.4) 25%, transparent 55%)",
                opacity: lightOp, pointerEvents: "none",
              }} />
            )}
          </AbsoluteFill>
          <FilmGrain seed={panel.scene_number * 7 + panel.panel_number} />
          {dir.show_particles && <FloatingHearts frame={frame} fps={fps} durationFrames={durationFrames} />}
          {TextNode}
        </>
      )}

      {/* ══════════════ TITLE CARD ══════════════ */}
      {dir.bg_style === "title" && (
        <>
          <AbsoluteFill style={{ backgroundColor: "#1a1612" }} />
          <AbsoluteFill style={{
            background: "radial-gradient(ellipse at 50% 50%, rgba(190,150,100,0.18) 0%, transparent 65%)",
            pointerEvents: "none",
          }} />
          <AbsoluteFill style={{ display: "flex", alignItems: "center", justifyContent: "center" }}>
            <div style={{ opacity: subOp, textAlign: "center" }}>
              {/* Names */}
              <div style={{
                color: "#F5EFE6",
                fontSize: 60,
                fontWeight: 400,
                fontFamily,
                letterSpacing: "0.16em",
                lineHeight: 1.3,
              }}>Paul & Anli</div>
              {/* Expanding line */}
              <div style={{
                height: 1,
                backgroundColor: "rgba(245,239,230,0.38)",
                width: `${underlineW}%`,
                margin: "20px auto 18px",
              }} />
              {/* Date */}
              <div style={{
                color: "rgba(245,239,230,0.68)",
                fontSize: 26,
                fontWeight: 300,
                fontFamily,
                letterSpacing: "0.24em",
              }}>2026.07.26</div>
              {/* Decoration */}
              <div style={{
                color: "rgba(245,239,230,0.35)",
                fontSize: 18,
                marginTop: 28,
                letterSpacing: "0.18em",
                fontFamily,
              }}>✦ forever ✦</div>
            </div>
          </AbsoluteFill>
        </>
      )}

      {panel.audio_url && <Audio src={staticFile(panel.audio_url)} />}
    </AbsoluteFill>
  );
};
