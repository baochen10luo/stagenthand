import { AbsoluteFill, Series, Audio, staticFile, interpolate, useCurrentFrame, useVideoConfig } from "remotion";
import type { RemotionProps, Directives } from "../types";
import { PanelSlide } from "./PanelSlide";

// Default directives when none provided
const DD: Required<Directives> = {
  bgm_fade_in_sec: 2.0,
  bgm_fade_out_sec: 3.0,
  bgm_volume: 0.6,
  ducking_depth: 0.15,
  ducking_fade_sec: 0.5,
  color_filter: "none",
  bgm_tags: "",
  style_prompt: "",
};

function dd(d?: Directives): Required<Directives> {
  return { ...DD, ...(d ?? {}) };
}

// BGMAudio handles fade-in, fade-out, and anticipatory auto-ducking of background music.
const BGMAudio: React.FC<{
  bgmUrl: string;
  directives: Required<Directives>;
  panels: RemotionProps["panels"];
  fps: number;
  totalFrames: number;
}> = ({ bgmUrl, directives, panels, fps, totalFrames }) => {
  const frame = useCurrentFrame();

  // 1. Fade envelope (global BGM fade in/out)
  const fadeInFrames = Math.round(directives.bgm_fade_in_sec * fps);
  const fadeOutFrames = Math.round(directives.bgm_fade_out_sec * fps);

  const fadeIn = interpolate(frame, [0, fadeInFrames], [0, 1], {
    extrapolateRight: "clamp",
    extrapolateLeft: "clamp",
  });
  const fadeOut = interpolate(
    frame,
    [totalFrames - fadeOutFrames, totalFrames],
    [1, 0],
    { extrapolateRight: "clamp", extrapolateLeft: "clamp" }
  );
  const fadeEnvelope = Math.min(fadeIn, fadeOut);

  // 2. Anticipatory Ducking: gracefully lower BGM volume spanning voiceovers
  let duckFactor = 1.0;
  let accumulatedFrames = 0;
  const duckFadeFrames = Math.round(directives.ducking_fade_sec * fps);
  // Calculate Target Duck Factor
  const targetDuckRatio = directives.ducking_depth / (directives.bgm_volume || 1);

  for (const panel of panels) {
    const panelFrames = Math.max(1, Math.round(panel.duration_sec * fps));
    const panelStart = accumulatedFrames;
    const panelEnd = accumulatedFrames + panelFrames;

    if (panel.audio_url) {
      const distanceToStart = panelStart - frame;
      const distanceFromEnd = frame - panelEnd;

      if (frame >= panelStart && frame <= panelEnd) {
        // Deep in the voiceover
        duckFactor = Math.min(duckFactor, targetDuckRatio);
      } else if (distanceToStart > 0 && distanceToStart <= duckFadeFrames) {
        // Anticipatory fade down BEFORE the voiceover begins
        const duck = interpolate(
          distanceToStart,
          [0, duckFadeFrames],
          [targetDuckRatio, 1.0],
          { extrapolateRight: "clamp", extrapolateLeft: "clamp" }
        );
        duckFactor = Math.min(duckFactor, duck);
      } else if (distanceFromEnd > 0 && distanceFromEnd <= duckFadeFrames) {
        // Lingering fade up AFTER the voiceover ends
        const duck = interpolate(
          distanceFromEnd,
          [0, duckFadeFrames],
          [targetDuckRatio, 1.0],
          { extrapolateRight: "clamp", extrapolateLeft: "clamp" }
        );
        duckFactor = Math.min(duckFactor, duck);
      }
    }
    accumulatedFrames = panelEnd;
  }

  const finalVolume = directives.bgm_volume * fadeEnvelope * duckFactor;

  return <Audio src={staticFile(bgmUrl)} loop volume={Math.max(0, finalVolume)} />;
};


// ShortDrama is the main composition component.
// It uses <Series> to play panels one after another.
// Duration is driven dynamically by calculateMetadata in Root.tsx.
export const ShortDrama: React.FC<RemotionProps> = ({
  panels,
  fps,
  bgm_url,
  directives: rawDirectives,
}) => {
  const { durationInFrames } = useVideoConfig();
  const dir = dd(rawDirectives);

  if (!panels || panels.length === 0) {
    return (
      <AbsoluteFill
        style={{
          backgroundColor: "#000",
          display: "flex",
          alignItems: "center",
          justifyContent: "center",
        }}
      >
        <div style={{ color: "#666", fontFamily: "sans-serif", fontSize: 28 }}>
          No panels provided
        </div>
      </AbsoluteFill>
    );
  }

  return (
    <AbsoluteFill style={{ backgroundColor: "#000" }}>
      {bgm_url && (
        <BGMAudio
          bgmUrl={bgm_url}
          directives={dir}
          panels={panels}
          fps={fps}
          totalFrames={durationInFrames}
        />
      )}
      <Series>
        {panels.map((panel, i) => {
          const durationInFrames = Math.max(
            1,
            Math.round(panel.duration_sec * fps)
          );
          return (
            <Series.Sequence
              key={`${panel.scene_number}-${panel.panel_number}-${i}`}
              durationInFrames={durationInFrames}
              premountFor={fps}
            >
              <PanelSlide panel={panel} colorFilter={dir.color_filter} />
            </Series.Sequence>
          );
        })}
      </Series>
    </AbsoluteFill>
  );
};
