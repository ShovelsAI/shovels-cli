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

// ErrorPayload is the structured JSON error written to stderr.
type ErrorPayload struct {
	Error     string `json:"error"`
	Code      int    `json:"code"`
	ErrorType string `json:"error_type,omitempty"`
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

// PrintError writes a structured JSON error to the writer.
func PrintError(w io.Writer, msg string, code int) {
	payload := ErrorPayload{
		Error: msg,
		Code:  code,
	}
	enc := json.NewEncoder(w)
	enc.SetEscapeHTML(false)
	_ = enc.Encode(payload)
}

// PrintErrorTyped writes a structured JSON error with an error_type field
// for machine classification of the error category.
func PrintErrorTyped(w io.Writer, msg string, code int, errorType string) {
	payload := ErrorPayload{
		Error:     msg,
		Code:      code,
		ErrorType: errorType,
	}
	enc := json.NewEncoder(w)
	enc.SetEscapeHTML(false)
	_ = enc.Encode(payload)
}
