package hyperframes

import (
	"path/filepath"
	"strings"
)

// ResolveVirtualPath converts a /shand/<id>/... virtual path back to a real
// disk path under shandHome. Returns path unchanged if it does not start
// with "/shand/". Empty string returns "".
func ResolveVirtualPath(shandHome, path string) string {
	if path == "" {
		return ""
	}
	if !strings.HasPrefix(path, "/shand/") {
		return path
	}
	// /shand/<id>/rest → <shandHome>/projects/<id>/rest
	trimmed := strings.TrimPrefix(path, "/shand/")
	return filepath.Join(shandHome, "projects", trimmed)
}
