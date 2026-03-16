package signup

import "encoding/json"

// Codec provides an interface for JSON encoding/decoding to avoid data races
// on package-level mutable variables in parallel tests.
type Codec interface {
	Marshal(v any) ([]byte, error)
	Unmarshal(data []byte, v any) error
}

type stdCodec struct{}

func (stdCodec) Marshal(v any) ([]byte, error)            { return json.Marshal(v) }
func (stdCodec) Unmarshal(data []byte, v any) error      { return json.Unmarshal(data, v) }

var defaultCodec Codec = stdCodec{}
