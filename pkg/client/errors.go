package client

import (
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"strings"
)

// APIError represents a non-2xx response from the API.
type APIError struct {
	StatusCode int
	Message    string
	Body       string
}

// Error implements the error interface.
func (e *APIError) Error() string {
	if e == nil {
		return ""
	}
	return fmt.Sprintf("api error (%d): %s", e.StatusCode, e.Message)
}

func parseAPIError(resp *http.Response) error {
	data, _ := io.ReadAll(io.LimitReader(resp.Body, 64*1024))
	message := strings.TrimSpace(resp.Status)

	if len(data) > 0 {
		var payload struct {
			Error string `json:"error"`
		}
		if err := json.Unmarshal(data, &payload); err == nil && strings.TrimSpace(payload.Error) != "" {
			message = strings.TrimSpace(payload.Error)
		}
	}

	if message == "" {
		message = http.StatusText(resp.StatusCode)
	}

	return &APIError{
		StatusCode: resp.StatusCode,
		Message:    message,
		Body:       string(data),
	}
}
