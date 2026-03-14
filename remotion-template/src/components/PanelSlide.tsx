import {
  AbsoluteFill,
  Img,
  interpolate,
  staticFile,
  useCurrentFrame,
  useVideoConfig,
} from "remotion";
import { Panel } from "../types";

type PanelSlideProps = {
  panel: Panel;
};

// PanelSlide renders a single panel:
// - Full-frame background image with Ken Burns subtle zoom
// - Bottom subtitle bar with fade-in dialogue
// CSS transitions are FORBIDDEN per Remotion best practices.
// ALL animations driven by useCurrentFrame().
export const PanelSlide: React.FC<PanelSlideProps> = ({ panel }) => {
  const frame = useCurrentFrame();
  const { fps } = useVideoConfig();

  // Fade in over first 0.3s
  const opacity = interpolate(frame, [0, Math.round(0.3 * fps)], [0, 1], {
    extrapolateRight: "clamp",
    extrapolateLeft: "clamp",
  });

  // Ken Burns: subtle zoom from 1.0 → 1.05 over entire panel duration
  const durationFrames = Math.round(panel.duration_sec * fps);
  const scale = interpolate(frame, [0, durationFrames], [1.0, 1.05], {
    extrapolateRight: "clamp",
    extrapolateLeft: "clamp",
  });

  // Subtitle fade in slightly after image (0.15s delay)
  const subtitleDelay = Math.round(0.15 * fps);
  const subtitleOpacity = interpolate(
    frame,
    [subtitleDelay, subtitleDelay + Math.round(0.3 * fps)],
    [0, 1],
    { extrapolateRight: "clamp", extrapolateLeft: "clamp" }
  );

  return (
    <AbsoluteFill style={{ backgroundColor: "#000", opacity }}>
      {/* Background image with Ken Burns zoom */}
      {panel.image_url ? (
        <AbsoluteFill
          style={{
            transform: `scale(${scale})`,
            transformOrigin: "center center",
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
        // Placeholder when no image generated yet
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

      {/* Subtitle bar — bottom 20% */}
      {panel.dialogue && (
        <AbsoluteFill
          style={{
            justifyContent: "flex-end",
            alignItems: "stretch",
            opacity: subtitleOpacity,
          }}
        >
          <div
            style={{
              background: "linear-gradient(transparent, rgba(0,0,0,0.85))",
              padding: "40px 48px 32px",
              display: "flex",
              justifyContent: "center",
            }}
          >
            <span
              style={{
                color: "#fff",
                fontSize: 36,
                fontWeight: 600,
                fontFamily:
                  '"Noto Sans TC", "PingFang TC", "Microsoft JhengHei", sans-serif',
                textAlign: "center",
                textShadow: "0 2px 8px rgba(0,0,0,0.9)",
                lineHeight: 1.5,
                maxWidth: "80%",
              }}
            >
              {panel.dialogue}
            </span>
          </div>
        </AbsoluteFill>
      )}
    </AbsoluteFill>
  );
};
