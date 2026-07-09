package core

import "github.com/bonztm/agent-workflow-manager/internal/contracts/v1"

// APIError is a structured service error carrying a stable code, a
// human-readable message, an optional originating source, and arbitrary
// details for the wire payload.
type APIError struct {
	Code    string
	Message string
	Source  string
	Details any
}

func (e *APIError) Error() string {
	if e == nil {
		return ""
	}
	return e.Code + ": " + e.Message
}

// ToPayload converts the error into its v1 wire representation.
// It returns nil when the receiver is nil.
func (e *APIError) ToPayload() *v1.ErrorPayload {
	if e == nil {
		return nil
	}
	return &v1.ErrorPayload{
		Code:    e.Code,
		Message: e.Message,
		Source:  e.Source,
		Details: e.Details,
	}
}

// NewError constructs an APIError with the given code, message, and
// details, and an empty source.
func NewError(code, message string, details any) *APIError {
	return NewErrorWithSource(code, message, "", details)
}

// NewErrorWithSource constructs an APIError with an explicit originating
// source in addition to code, message, and details.
func NewErrorWithSource(code, message, source string, details any) *APIError {
	return &APIError{Code: code, Message: message, Source: source, Details: details}
}
