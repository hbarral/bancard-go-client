package bancard

import (
	"bytes"
	"encoding/json"
	"fmt"
	"io"
)

// marshalJSON encodes v to JSON, enforcing the message format expected by
// VPOS (standard \uXXXX escaping for special characters, as produced by
// encoding/json).
func marshalJSON(v any) (io.Reader, error) {
	buf, err := json.Marshal(v)
	if err != nil {
		return nil, fmt.Errorf("marshal: %w", err)
	}
	return bytes.NewReader(buf), nil
}

// decodeJSON decodes a JSON body into out. A limited number of bytes is
// read to avoid unbounded responses.
func decodeJSON(r io.Reader, out any) error {
	dec := json.NewDecoder(io.LimitReader(r, 1<<20))
	if err := dec.Decode(out); err != nil {
		return fmt.Errorf("decode: %w", err)
	}
	return nil
}
