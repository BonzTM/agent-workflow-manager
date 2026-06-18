package v1

import (
	"encoding/json"
	"regexp"
	"testing"
)

func TestErrorCodeConstants(t *testing.T) {
	codes := []string{
		ErrCodeInvalidJSON,
		ErrCodeInvalidVersion,
		ErrCodeInvalidCommand,
		ErrCodeInvalidPayload,
		ErrCodeInvalidRequestID,
		ErrCodeInvalidInput,
		ErrCodeValidationError,
		ErrCodeUnknownTool,
		ErrCodeMissingTool,
		ErrCodeServiceInitFailed,
		ErrCodeDispatchFailed,
		ErrCodeReadFailed,
		ErrCodeWriteFailed,
		ErrCodeInternalError,
		ErrCodeNotFound,
		ErrCodeNotImplemented,
		ErrCodeMissingSubcommand,
		ErrCodeUnknownSubcommand,
		ErrCodeInvalidFlags,
	}

	pattern := regexp.MustCompile(`^[A-Z0-9_]+$`)
	seen := map[string]struct{}{}
	for _, code := range codes {
		if code == "" {
			t.Fatalf("error code constant must not be empty")
		}
		if !pattern.MatchString(code) {
			t.Fatalf("error code %q must be uppercase with underscores", code)
		}
		if _, ok := seen[code]; ok {
			t.Fatalf("error code %q is duplicated", code)
		}
		seen[code] = struct{}{}
	}
}

func TestErrorPayloadSourceField(t *testing.T) {
	p := ErrorPayload{Code: ErrCodeInvalidPayload, Message: "bad payload", Source: ErrSourceValidation}
	raw, err := json.Marshal(p)
	if err != nil {
		t.Fatalf("marshal: %v", err)
	}

	var got map[string]any
	if err := json.Unmarshal(raw, &got); err != nil {
		t.Fatalf("unmarshal: %v", err)
	}
	if got["source"] != ErrSourceValidation {
		t.Fatalf("unexpected source field: got %v want %q", got["source"], ErrSourceValidation)
	}
}

func TestValidationUsesConstants(t *testing.T) {
	tests := []struct {
		name string
		json string
		want string
	}{
		{
			name: "invalid json",
			json: `{"version":"awm.v1"`,
			want: ErrCodeInvalidJSON,
		},
		{
			name: "invalid command",
			json: `{"version":"awm.v1","command":"nope","request_id":"req-12345678","payload":{}}`,
			want: ErrCodeInvalidCommand,
		},
		{
			name: "missing payload",
			json: `{"version":"awm.v1","command":"context","request_id":"req-12345678","payload":null}`,
			want: ErrCodeInvalidPayload,
		},
	}

	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			_, _, errp := DecodeAndValidateCommandWithDefaults([]byte(tc.json), ValidationDefaults{})
			if errp == nil {
				t.Fatalf("expected validation error")
			}
			if errp.Code != tc.want {
				t.Fatalf("unexpected code: got %q want %q", errp.Code, tc.want)
			}
			if errp.Source != ErrSourceValidation {
				t.Fatalf("unexpected source: got %q want %q", errp.Source, ErrSourceValidation)
			}
		})
	}
}
