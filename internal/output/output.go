package output

import (
	"encoding/json"
	"io"
)

// Envelope is the universal response wrapper for all CLI output.
// Every command outputs {"data": ..., "meta": {...}}.
type Envelope struct {
	Data any            `json:"data"`
	Meta map[string]any `json:"meta"`
}

// PrintData writes a JSON envelope with data and empty meta to the writer.
func PrintData(w io.Writer, data any) {
	env := Envelope{
		Data: data,
		Meta: map[string]any{},
	}
	enc := json.NewEncoder(w)
	enc.SetEscapeHTML(false)
	_ = enc.Encode(env)
}
