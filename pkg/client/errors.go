package client

import (
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"strings"
)

// APIError represents a non-2xx response from the Pali API.
// Callers should use errors.As to inspect the full error, or errors.Is against
// the sentinel values (ErrNotFound, ErrUnauthorized, etc.) for common cases.
type APIError struct {
	// StatusCode is the HTTP status code returned by the server.
	StatusCode int
	// Code is the machine-readable error code from the API response body (e.g. "not_found").
	Code string
	// Message is the human-readable error message.
	Message string
	// RequestID is the value of the X-Request-ID response header, if present.
	// Include this in bug reports and support tickets.
	RequestID string
	// Body is the raw response body, preserved for debugging.
	Body string
}

// Error implements the error interface.
func (e *APIError) Error() string {
	if e == nil {
		return ""
	}
	if e.RequestID != "" {
		return fmt.Sprintf("pali: %d %s: %s (request_id=%s)", e.StatusCode, e.Code, e.Message, e.RequestID)
	}
	return fmt.Sprintf("pali: %d %s: %s", e.StatusCode, e.Code, e.Message)
}

// Is reports whether this error matches target.
// Two APIErrors are equal when they have the same StatusCode and Code.
// This enables errors.Is(err, client.ErrNotFound) checks.
func (e *APIError) Is(target error) bool {
	t, ok := target.(*APIError)
	if !ok {
		return false
	}
	if t.StatusCode != 0 && t.StatusCode != e.StatusCode {
		return false
	}
	if t.Code != "" && t.Code != e.Code {
		return false
	}
	return true
}

// Sentinel errors for the most common API error conditions.
// Use with errors.Is:
//
//	if errors.Is(err, client.ErrNotFound) { ... }
var (
	ErrNotFound     = &APIError{StatusCode: 404, Code: "not_found"}
	ErrUnauthorized = &APIError{StatusCode: 401, Code: "unauthorized"}
	ErrForbidden    = &APIError{StatusCode: 403, Code: "forbidden"}
	ErrConflict     = &APIError{StatusCode: 409, Code: "conflict"}
	ErrRateLimit    = &APIError{StatusCode: 429, Code: "rate_limited"}
)

func parseAPIError(resp *http.Response) error {
	requestID := strings.TrimSpace(resp.Header.Get("X-Request-ID"))
	data, _ := io.ReadAll(io.LimitReader(resp.Body, 64*1024))

	code := ""
	message := strings.TrimSpace(resp.Status)

	if len(data) > 0 {
		var payload struct {
			Error   string `json:"error"`
			Code    string `json:"code"`
			Message string `json:"message"`
		}
		if err := json.Unmarshal(data, &payload); err == nil {
			if strings.TrimSpace(payload.Error) != "" {
				message = strings.TrimSpace(payload.Error)
			} else if strings.TrimSpace(payload.Message) != "" {
				message = strings.TrimSpace(payload.Message)
			}
			if strings.TrimSpace(payload.Code) != "" {
				code = strings.TrimSpace(payload.Code)
			}
		}
	}

	if message == "" {
		message = http.StatusText(resp.StatusCode)
	}
	if code == "" {
		code = deriveCode(resp.StatusCode)
	}

	return &APIError{
		StatusCode: resp.StatusCode,
		Code:       code,
		Message:    message,
		RequestID:  requestID,
		Body:       string(data),
	}
}

// deriveCode maps common HTTP status codes to a machine-readable code string
// when the server does not supply one explicitly.
func deriveCode(status int) string {
	switch status {
	case 400:
		return "bad_request"
	case 401:
		return "unauthorized"
	case 403:
		return "forbidden"
	case 404:
		return "not_found"
	case 409:
		return "conflict"
	case 422:
		return "unprocessable"
	case 429:
		return "rate_limited"
	case 500:
		return "internal_error"
	case 503:
		return "unavailable"
	default:
		return "error"
	}
}
