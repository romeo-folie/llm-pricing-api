package signup

import "encoding/json"

// jsonMarshal and jsonUnmarshal are package-level vars so tests can swap the
// encoding implementation without touching the token logic.
var (
	jsonMarshal   = json.Marshal
	jsonUnmarshal = func(b []byte, v any) error { return json.Unmarshal(b, v) }
)
