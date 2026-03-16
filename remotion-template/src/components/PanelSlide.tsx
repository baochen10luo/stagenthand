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

type PanelSlideProps = {
  panel: Panel;
  colorFilter?: string;
};

// CSS filter presets for color grading
const COLOR_FILTERS: Record<string, string> = {
  none: "none",
  cinematic: "contrast(1.1) saturate(0.85) brightness(0.95) sepia(0.1)",
  vintage: "sepia(0.4) contrast(1.05) brightness(0.9) saturate(0.7)",
  cyberpunk: "contrast(1.2) saturate(1.4) hue-rotate(10deg) brightness(0.9)",
};

// Defaults for when directive is missing
const D: Required<PanelDirective> = {
  motion_effect: "ken_burns_in",
  motion_intensity: 0.05,
  transition_in: "fade",
  transition_out: "fade",
  transition_duration_ms: 300,
  subtitle_effect: "fade",
  subtitle_font_size: 36,
  subtitle_position: "bottom",
};

function d(panel: Panel): Required<PanelDirective> {
  return { ...D, ...(panel.directive ?? {}) };
}

export const PanelSlide: React.FC<PanelSlideProps> = ({ panel, colorFilter }) => {
  const frame = useCurrentFrame();
  const { fps, width, height } = useVideoConfig();
  const isPortrait = height > width;
  const dir = d(panel);
  const durationFrames = Math.round(panel.duration_sec * fps);

  // ─── Transition In ───
  const transInFrames = Math.round((dir.transition_duration_ms / 1000) * fps);
  let inOpacity = 1;
  if (dir.transition_in === "fade" || dir.transition_in === "dissolve") {
    inOpacity = interpolate(frame, [0, transInFrames], [0, 1], {
      extrapolateRight: "clamp",
      extrapolateLeft: "clamp",
    });
  } else if (dir.transition_in === "wipe_left") {
    inOpacity = interpolate(frame, [0, transInFrames], [0, 1], {
      extrapolateRight: "clamp",
      extrapolateLeft: "clamp",
    });
  }
  // "cut" → inOpacity stays 1

  // ─── Transition Out ───
  const transOutFrames = Math.round((dir.transition_duration_ms / 1000) * fps);
  let outOpacity = 1;
  if (dir.transition_out === "fade" || dir.transition_out === "dissolve") {
    outOpacity = interpolate(
      frame,
      [durationFrames - transOutFrames, durationFrames],
      [1, 0],
      { extrapolateRight: "clamp", extrapolateLeft: "clamp" }
    );
  }

  const opacity = Math.min(inOpacity, outOpacity);

  // ─── Camera Motion ───
  let transform = "none";
  const intensity = dir.motion_intensity;
  if (dir.motion_effect === "ken_burns_in") {
    const scale = interpolate(frame, [0, durationFrames], [1.0, 1.0 + intensity], {
      extrapolateRight: "clamp",
      extrapolateLeft: "clamp",
    });
    transform = `scale(${scale})`;
  } else if (dir.motion_effect === "ken_burns_out") {
    const scale = interpolate(frame, [0, durationFrames], [1.0 + intensity, 1.0], {
      extrapolateRight: "clamp",
      extrapolateLeft: "clamp",
    });
    transform = `scale(${scale})`;
  } else if (dir.motion_effect === "pan_left") {
    const tx = interpolate(frame, [0, durationFrames], [0, -(intensity * 100)], {
      extrapolateRight: "clamp",
      extrapolateLeft: "clamp",
    });
    transform = `translateX(${tx}%)`;
  } else if (dir.motion_effect === "pan_right") {
    const tx = interpolate(frame, [0, durationFrames], [0, intensity * 100], {
      extrapolateRight: "clamp",
      extrapolateLeft: "clamp",
    });
    transform = `translateX(${tx}%)`;
  }
  // "static" → transform stays "none"

  // ─── Wipe-Left (clip-path based) ───
  let clipPath: string | undefined;
  if (dir.transition_in === "wipe_left") {
    const wipeProgress = interpolate(frame, [0, transInFrames], [0, 100], {
      extrapolateRight: "clamp",
      extrapolateLeft: "clamp",
    });
    clipPath = `inset(0 ${100 - wipeProgress}% 0 0)`;
  }

  // ─── Subtitle animation ───
  // Sanitize dialogue: remove prefixes like "VO:", "V.O.", "VO - ", "[Narrator]" and surrounding quotes.
  const sanitizeDialogue = (text: string) => {
    if (!text) return "";
    let clean = text.trim();
    // Strip common prefixes
    clean = clean.replace(/^(?:VO|V\.O\.|Narrator|Voiceover|Voice Over|\[.*?\])\s*[:\-]*\s*/i, "");
    // Also remove any stray starting or ending quotes loosely
    clean = clean.replace(/^["']+(.*?)["']+$/s, "$1");
    // Strip again in case it was "VO: 'Hello'"
    clean = clean.replace(/^(?:VO|V\.O\.|Narrator|Voiceover|Voice Over|\[.*?\])\s*[:\-]*\s*/i, "");
    return clean.trim();
  };

  // ─── Multi-line subtitle with per-line timing (Strategy C + D) ───
  // If dialogue_lines are present and have start_sec/end_sec, use them for timed display.
  // Otherwise fall back to the legacy single dialogue string.
  const getActiveDialogueLine = (lines: DialogueLine[] | undefined): string => {
    if (!lines || lines.length === 0) return "";
    const currentSec = frame / fps;
    // Find the active line for the current time position
    for (const line of lines) {
      const start = line.start_sec ?? 0;
      const end = line.end_sec ?? durationFrames / fps;
      if (currentSec >= start && currentSec < end) {
        return sanitizeDialogue(line.text);
      }
    }
    // If past all lines, show the last line until end of panel
    const lastLine = lines[lines.length - 1];
    if (lastLine && frame < durationFrames) {
      return sanitizeDialogue(lastLine.text);
    }
    return "";
  };

  const hasTimedLines = (panel.dialogue_lines?.length ?? 0) > 0 &&
    (panel.dialogue_lines?.[0]?.start_sec !== undefined || panel.dialogue_lines?.[0]?.end_sec !== undefined);

  const subtitleDelay = Math.round(0.15 * fps);
  let subtitleOpacity = 1;
  const cleanDialogue = hasTimedLines
    ? getActiveDialogueLine(panel.dialogue_lines)
    : sanitizeDialogue(panel.dialogue);
  let subtitleText = cleanDialogue;

  if (dir.subtitle_effect === "fade") {
    subtitleOpacity = interpolate(
      frame,
      [subtitleDelay, subtitleDelay + Math.round(0.3 * fps)],
      [0, 1],
      { extrapolateRight: "clamp", extrapolateLeft: "clamp" }
    );
  } else if (dir.subtitle_effect === "typewriter" && cleanDialogue) {
    subtitleOpacity = 1;
    const totalChars = cleanDialogue.length;
    // Linus architectural fix: decouple speed from duration.
    // Assume roughly 100ms (0.1s) per character to match natural reading/speaking speed.
    const typewriterFrames = Math.round(totalChars * (0.1 * fps));
    const charsVisible = Math.round(
      interpolate(frame, [subtitleDelay, subtitleDelay + typewriterFrames], [0, totalChars], {
        extrapolateRight: "clamp",
        extrapolateLeft: "clamp",
      })
    );
    subtitleText = cleanDialogue.substring(0, charsVisible);
  } else if (dir.subtitle_effect === "none") {
    subtitleOpacity = 1;
  }

  // ─── Subtitle position mapping ───
  const subtitleJustify =
    dir.subtitle_position === "top"
      ? "flex-start"
      : dir.subtitle_position === "center"
        ? "center"
        : "flex-end";
  const subtitlePaddingTop = dir.subtitle_position === "top" ? "32px" : "0";

  // ─── Color filter ───
  const filterCSS = COLOR_FILTERS[colorFilter ?? "none"] ?? "none";

  return (
    <AbsoluteFill style={{ backgroundColor: "#000", opacity, clipPath }}>
      {/* Background image with camera motion */}
      {panel.image_url ? (
        <AbsoluteFill
          style={{
            transform,
            transformOrigin: "center center",
            filter: filterCSS,
          }}
        >
          <Img
            src={staticFile(panel.image_url)}
            style={{
              width: "100%",
              height: "100%",
              objectFit: "cover",
            }}
          />
        </AbsoluteFill>
      ) : (
        <AbsoluteFill
          style={{
            backgroundColor: "#1a1a2e",
            display: "flex",
            alignItems: "center",
            justifyContent: "center",
          }}
        >
          <div style={{ color: "#666", fontSize: 24, fontFamily: "sans-serif" }}>
            {panel.description}
          </div>
        </AbsoluteFill>
      )}

      {/* Subtitle bar */}
      {(panel.dialogue || (panel.dialogue_lines?.length ?? 0) > 0) && subtitleText && (
        <AbsoluteFill
          style={{
            justifyContent: subtitleJustify,
            alignItems: "stretch",
            opacity: subtitleOpacity,
          }}
        >
          <div
            style={{
              background:
                dir.subtitle_position === "bottom"
                  ? "linear-gradient(transparent, rgba(0,0,0,0.85))"
                  : dir.subtitle_position === "top"
                    ? "linear-gradient(rgba(0,0,0,0.85), transparent)"
                    : "rgba(0,0,0,0.6)",
              padding: `40px 48px 32px`,
              paddingTop: subtitlePaddingTop,
              display: "flex",
              justifyContent: "center",
            }}
          >
            <span
              style={{
                color: "#fff",
                fontSize: dir.subtitle_font_size !== D.subtitle_font_size
                  ? dir.subtitle_font_size
                  : (isPortrait ? 32 : 40),
                fontWeight: 600,
                fontFamily:
                  '"Noto Sans TC", "PingFang TC", "Microsoft JhengHei", sans-serif',
                textAlign: "center",
                textShadow: "0 2px 8px rgba(0,0,0,0.9)",
                lineHeight: 1.5,
                maxWidth: isPortrait ? "92%" : "80%",
              }}
            >
              {subtitleText}
            </span>
          </div>
        </AbsoluteFill>
      )}

      {/* TTS Audio */}
      {panel.audio_url && <Audio src={staticFile(panel.audio_url)} />}
    </AbsoluteFill>
  );
};
