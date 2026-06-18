package core

import "github.com/bonztm/agent-workflow-manager/internal/contracts/v1"

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

func NewError(code, message string, details any) *APIError {
	return NewErrorWithSource(code, message, "", details)
}

func NewErrorWithSource(code, message, source string, details any) *APIError {
	return &APIError{Code: code, Message: message, Source: source, Details: details}
}
