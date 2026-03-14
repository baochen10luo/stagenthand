package pipeline

import "encoding/json"

// jsonUnmarshal wraps json.Unmarshal to keep import in one place.
func jsonUnmarshal(data []byte, v any) error {
	return json.Unmarshal(data, v)
}
