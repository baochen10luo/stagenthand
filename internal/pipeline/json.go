package pipeline

import "encoding/json"

// jsonUnmarshal wraps json.Unmarshal to keep import in one place.
func jsonUnmarshal(data []byte, v any) error {
	return json.Unmarshal(data, v)
}

// jsonMarshal wraps json.Marshal.
func jsonMarshal(v any) ([]byte, error) {
	return json.Marshal(v)
}
