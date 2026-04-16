// RemotionProps - must mirror domain.RemotionProps in Go
// This is the contract between shand CLI and the Remotion template.
// Changes here must be reflected in internal/domain/types.go

export type DialogueLine = {
  speaker: string;   // character name; "" = narrator
  text: string;
  emotion?: string;  // happy | sad | angry | whisper | neutral
  start_sec?: number; // subtitle display start (seconds into panel)
  end_sec?: number;   // subtitle display end (seconds into panel)
};

export type PanelDirective = {
  motion_effect?: "ken_burns_in" | "ken_burns_out" | "pan_left" | "pan_right" | "static";
  motion_intensity?: number;
  transition_in?: "fade" | "cut" | "dissolve" | "wipe_left";
  transition_out?: "fade" | "cut" | "dissolve" | "wipe_left";
  transition_duration_ms?: number;
  subtitle_effect?: "fade" | "typewriter" | "none";
  subtitle_font_size?: number;
  subtitle_position?: "bottom" | "top" | "center";
  // Extended visual design fields
  bg_style?: "card" | "fullbleed" | "title";
  text_x?: "left" | "center" | "right";
  text_y?: "top" | "middle" | "bottom";
  text_rotate?: number;
  show_particles?: boolean;
  show_light_leak?: boolean;
};

export type Panel = {
  scene_number: number;
  panel_number: number;
  description: string;
  dialogue: string;
  character_refs: string[];
  image_url: string;
  audio_url?: string;
  duration_sec: number;
  directive?: PanelDirective;
  dialogue_lines?: DialogueLine[]; // structured per-speaker lines with timing
};

export type Directives = {
  bgm_fade_in_sec?: number;
  bgm_fade_out_sec?: number;
  bgm_volume?: number;     // 0.0–1.0
  ducking_depth?: number;  // BGM volume during voiceover 0.0–1.0
  ducking_fade_sec?: number;
  bgm_tags?: string;
  color_filter?: "none" | "cinematic" | "vintage" | "cyberpunk";
  style_prompt?: string;
};

export type RemotionProps = {
  project_id: string;
  title: string;
  bgm_url?: string;
  directives?: Directives;
  panels: Panel[];
  fps: number;
  width: number;
  height: number;
};
