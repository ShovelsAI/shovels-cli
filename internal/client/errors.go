package client

import "fmt"

// Exit codes for structured error reporting.
const (
	ExitSuccess         = 0
	ExitClientError     = 1
	ExitAuthError       = 2
	ExitRateLimit       = 3
	ExitCreditExhausted = 4
	ExitTransientError  = 5
)

// Error type strings for machine classification.
const (
	ErrorTypeAuth       = "auth_error"
	ErrorTypeRateLimit  = "rate_limited"
	ErrorTypeCredit     = "credit_exhausted"
	ErrorTypeValidation = "validation_error"
	ErrorTypeServer     = "server_error"
	ErrorTypeNetwork    = "network_error"
)

// APIError represents an error returned by the Shovels API or the HTTP
// transport layer. It carries the exit code and error type needed for
// structured JSON error output.
type APIError struct {
	Message   string
	ExitCode  int
	ErrorType string
}

func (e *APIError) Error() string {
	return e.Message
}

// statusToError maps an HTTP status code to an APIError. The msg parameter
// is used as a fallback message when the response body is empty.
func statusToError(statusCode int, msg string) *APIError {
	switch statusCode {
	case 401:
		return &APIError{
			Message:   nonEmpty(msg, "Unauthorized"),
			ExitCode:  ExitAuthError,
			ErrorType: ErrorTypeAuth,
		}
	case 402:
		return &APIError{
			Message:   nonEmpty(msg, "Credit limit exceeded"),
			ExitCode:  ExitCreditExhausted,
			ErrorType: ErrorTypeCredit,
		}
	case 422:
		return &APIError{
			Message:   nonEmpty(msg, "Validation error"),
			ExitCode:  ExitClientError,
			ErrorType: ErrorTypeValidation,
		}
	case 429:
		return &APIError{
			Message:   nonEmpty(msg, "Rate limited"),
			ExitCode:  ExitRateLimit,
			ErrorType: ErrorTypeRateLimit,
		}
	default:
		if statusCode >= 500 {
			return &APIError{
				Message:   "Server error",
				ExitCode:  ExitTransientError,
				ErrorType: ErrorTypeServer,
			}
		}
		return &APIError{
			Message:   nonEmpty(msg, fmt.Sprintf("HTTP %d", statusCode)),
			ExitCode:  ExitClientError,
			ErrorType: ErrorTypeValidation,
		}
	}
}

// networkError creates an APIError for transport-level failures.
func networkError(err error) *APIError {
	return &APIError{
		Message:   fmt.Sprintf("Network error: %v", err),
		ExitCode:  ExitTransientError,
		ErrorType: ErrorTypeNetwork,
	}
}

func nonEmpty(a, b string) string {
	if a != "" {
		return a
	}
	return b
}
