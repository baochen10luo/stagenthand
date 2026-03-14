// RemotionProps - must mirror domain.RemotionProps in Go
// This is the contract between shand CLI and the Remotion template.
// Changes here must be reflected in internal/domain/types.go

export type Panel = {
  scene_number: number;
  panel_number: number;
  description: string;
  dialogue: string;
  character_refs: string[];
  image_url: string;
  audio_url?: string;
  duration_sec: number; // display duration in seconds
};

export type RemotionProps = {
  project_id: string;
  title: string;
  panels: Panel[];
  fps: number;    // default 24
  width: number;  // default 1024
  height: number; // default 576
};
