package hyperframes

import (
	"testing"
)

func TestResolveVirtualPath(t *testing.T) {
	home := "/home/user/.shand"
	tests := []struct {
		name string
		path string
		want string
	}{
		{
			name: "empty path returns empty",
			path: "",
			want: "",
		},
		{
			name: "already absolute unchanged",
			path: "/abs/path/to/file.png",
			want: "/abs/path/to/file.png",
		},
		{
			name: "relative path unchanged",
			path: "relative/path.mp3",
			want: "relative/path.mp3",
		},
		{
			name: "virtual shand path resolves",
			path: "/shand/proj123/images/scene_1_panel_1.png",
			want: "/home/user/.shand/projects/proj123/images/scene_1_panel_1.png",
		},
		{
			name: "virtual shand path nested",
			path: "/shand/abc/audio/tts.mp3",
			want: "/home/user/.shand/projects/abc/audio/tts.mp3",
		},
	}
	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			got := ResolveVirtualPath(home, tc.path)
			if got != tc.want {
				t.Errorf("got %q, want %q", got, tc.want)
			}
		})
	}
}
