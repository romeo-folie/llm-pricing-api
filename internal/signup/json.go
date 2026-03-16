package signup

import "encoding/json"

// jsonMarshal and jsonUnmarshal are package-level vars so tests can swap the
// encoding implementation without touching the token logic.
//
// Tests that swap these variables must NOT use t.Parallel() and must restore
// the original values via t.Cleanup to avoid data races with other tests.
var (
	jsonMarshal   = json.Marshal
	jsonUnmarshal = func(b []byte, v any) error { return json.Unmarshal(b, v) }
)
