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
  scene_transition_in: "crossfade",
  scene_transition_duration_ms: 0,
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
  const targetDuckRatio = directives.ducking_depth / (directives.bgm_volume || 1);

  for (const panel of panels) {
    const panelFrames = Math.max(1, Math.round(panel.duration_sec * fps));
    const panelStart = accumulatedFrames;
    const panelEnd = accumulatedFrames + panelFrames;

    if (panel.audio_url) {
      const distanceToStart = panelStart - frame;
      const distanceFromEnd = frame - panelEnd;

      if (frame >= panelStart && frame <= panelEnd) {
        duckFactor = Math.min(duckFactor, targetDuckRatio);
      } else if (distanceToStart > 0 && distanceToStart <= duckFadeFrames) {
        const duck = interpolate(
          distanceToStart,
          [0, duckFadeFrames],
          [targetDuckRatio, 1.0],
          { extrapolateRight: "clamp", extrapolateLeft: "clamp" }
        );
        duckFactor = Math.min(duckFactor, duck);
      } else if (distanceFromEnd > 0 && distanceFromEnd <= duckFadeFrames) {
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

// PanelWithTransition wraps a panel and applies scene-level crossfade.
const PanelWithTransition: React.FC<{
  panel: RemotionProps["panels"][0];
  colorFilter: string;
  localFrame: number;
  panelDurationFrames: number;
  isSceneStart: boolean;
  isSceneEnd: boolean;
  sceneTransFrames: number;
}> = ({ panel, colorFilter, localFrame, panelDurationFrames, isSceneStart, isSceneEnd, sceneTransFrames }) => {
  let opacity = 1;

  if (isSceneStart && sceneTransFrames > 0) {
    // Fade in from 0→1 over the first sceneTransFrames
    opacity = Math.min(opacity, interpolate(
      localFrame, [0, sceneTransFrames], [0, 1],
      { extrapolateLeft: "clamp", extrapolateRight: "clamp" }
    ));
  }
  if (isSceneEnd && sceneTransFrames > 0) {
    // Fade out from 1→0 over the last sceneTransFrames
    opacity = Math.min(opacity, interpolate(
      localFrame, [panelDurationFrames - sceneTransFrames, panelDurationFrames], [1, 0],
      { extrapolateLeft: "clamp", extrapolateRight: "clamp" }
    ));
  }

  return (
    <AbsoluteFill style={{ opacity, pointerEvents: "none" }}>
      <PanelSlide panel={panel} colorFilter={colorFilter} />
    </AbsoluteFill>
  );
};

// ShortDrama is the main composition component.
// Uses <Series> for within-scene panels and manual overlapping for scene crossfade.
export const ShortDrama: React.FC<RemotionProps> = ({
  panels,
  fps,
  bgm_url,
  directives: rawDirectives,
}) => {
  const { durationInFrames } = useVideoConfig();
  const frame = useCurrentFrame();
  const dir = dd(rawDirectives);

  const sceneTransMs = rawDirectives?.scene_transition_duration_ms ?? 0;
  const sceneTransFrames = Math.round((sceneTransMs / 1000) * fps);

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

  // When no scene transitions configured, use simple Series layout (original behavior)
  if (sceneTransFrames === 0) {
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
            const durationInFrames = Math.max(1, Math.round(panel.duration_sec * fps));
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
  }

  // Scene transitions enabled: compute timeline with crossfade overlap
  const panelDurations = panels.map(p => Math.max(1, Math.round(p.duration_sec * fps)));
  const naturalOffsets: number[] = [];
  let acc = 0;
  for (const d of panelDurations) {
    naturalOffsets.push(acc);
    acc += d;
  }

  // Determine scene boundaries
  const isSceneStart = panels.map((p, i) => i > 0 && p.scene_number !== panels[i - 1].scene_number);
  const isSceneEnd = panels.map((p, i) => i < panels.length - 1 && panels[i + 1].scene_number !== p.scene_number);

  // Find visible panels at current frame (accounting for overlap)
  const visibleIndices: number[] = [];
  for (let i = 0; i < panels.length; i++) {
    const start = naturalOffsets[i];
    const end = start + panelDurations[i];
    const effStart = isSceneStart[i] ? start - sceneTransFrames : start;
    const effEnd = isSceneEnd[i] ? end + sceneTransFrames : end;
    if (frame >= effStart && frame < effEnd) {
      visibleIndices.push(i);
    }
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
      {visibleIndices.map(idx => (
        <PanelWithTransition
          key={`${panels[idx].scene_number}-${panels[idx].panel_number}-${idx}`}
          panel={panels[idx]}
          colorFilter={dir.color_filter}
          localFrame={frame - naturalOffsets[idx]}
          panelDurationFrames={panelDurations[idx]}
          isSceneStart={isSceneStart[idx]}
          isSceneEnd={isSceneEnd[idx]}
          sceneTransFrames={sceneTransFrames}
        />
      ))}
    </AbsoluteFill>
  );
};
