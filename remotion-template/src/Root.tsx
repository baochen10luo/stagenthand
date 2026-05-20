import { CalculateMetadataFunction, Composition } from "remotion";
import type { RemotionProps } from "./types";
import { ShortDrama } from "./components/ShortDrama";

// calculateMetadata dynamically determines total duration from panel data.
// Accounts for scene transition overlap that reduces effective total.
const calculateMetadata: CalculateMetadataFunction<RemotionProps> = ({
  props,
}) => {
  const fps = props.fps ?? 24;
  const totalDurationSec = props.panels.reduce(
    (sum, panel) => sum + (panel.duration_sec > 0 ? panel.duration_sec : 3.0),
    0
  );
  const totalFrames = Math.max(1, Math.round(totalDurationSec * fps));

  // Subtract scene transition overlaps
  const sceneTransMs = props.directives?.scene_transition_duration_ms ?? 0;
  const sceneTransFrames = Math.round((sceneTransMs / 1000) * fps);
  let boundaries = 0;
  for (let i = 1; i < props.panels.length; i++) {
    if (props.panels[i].scene_number !== props.panels[i - 1].scene_number) {
      boundaries++;
    }
  }
  const effectiveFrames = Math.max(1, totalFrames - boundaries * sceneTransFrames);

  return {
    durationInFrames: effectiveFrames,
    fps,
    width: props.width ?? 1024,
    height: props.height ?? 576,
  };
};

// Default props for Remotion Studio preview
const defaultProps: RemotionProps = {
  project_id: "preview",
  title: "Preview Drama",
  fps: 24,
  width: 1024,
  height: 576,
  panels: [
    {
      scene_number: 1,
      panel_number: 1,
      description: "Opening scene",
      dialogue: "這是一個預覽場景",
      character_refs: [],
      image_url: "",
      duration_sec: 3.0,
    },
    {
      scene_number: 1,
      panel_number: 2,
      description: "Second panel",
      dialogue: "故事從這裡開始",
      character_refs: [],
      image_url: "",
      duration_sec: 3.0,
    },
  ],
};

export const RemotionRoot = () => {
  return (
    <Composition
      id="ShortDrama"
      component={ShortDrama}
      durationInFrames={72} // placeholder, overridden by calculateMetadata
      fps={24}
      width={1024}
      height={576}
      defaultProps={defaultProps}
      calculateMetadata={calculateMetadata}
    />
  );
};
