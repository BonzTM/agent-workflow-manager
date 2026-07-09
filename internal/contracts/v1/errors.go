package v1

// Validation error codes returned when a request envelope or payload fails
// decoding or validation: ErrCodeInvalidJSON for malformed JSON,
// ErrCodeInvalidVersion for an unsupported contract version,
// ErrCodeInvalidCommand for an unrecognized command, ErrCodeInvalidPayload for
// a payload that fails decoding or validation, ErrCodeInvalidRequestID for a
// bad request ID, ErrCodeInvalidInput for invalid caller input, and
// ErrCodeValidationError for other validation failures.
const (
	ErrCodeInvalidJSON      = "INVALID_JSON"
	ErrCodeInvalidVersion   = "INVALID_VERSION"
	ErrCodeInvalidCommand   = "INVALID_COMMAND"
	ErrCodeInvalidPayload   = "INVALID_PAYLOAD"
	ErrCodeInvalidRequestID = "INVALID_REQUEST_ID"
	ErrCodeInvalidInput     = "INVALID_INPUT"
	ErrCodeValidationError  = "VALIDATION_ERROR"
)

// Dispatch and backend error codes: ErrCodeUnknownTool and ErrCodeMissingTool
// for tool resolution failures, ErrCodeServiceInitFailed when the backend
// service cannot start, ErrCodeDispatchFailed when routing a command fails,
// ErrCodeReadFailed and ErrCodeWriteFailed for I/O failures,
// ErrCodeInternalError for unexpected server-side errors, ErrCodeNotFound for
// missing entities, and ErrCodeNotImplemented for unimplemented operations.
const (
	ErrCodeUnknownTool       = "UNKNOWN_TOOL"
	ErrCodeMissingTool       = "MISSING_TOOL"
	ErrCodeServiceInitFailed = "SERVICE_INIT_FAILED"
	ErrCodeDispatchFailed    = "DISPATCH_FAILED"
	ErrCodeReadFailed        = "READ_FAILED"
	ErrCodeWriteFailed       = "WRITE_FAILED"
	ErrCodeInternalError     = "INTERNAL_ERROR"
	ErrCodeNotFound          = "NOT_FOUND"
	ErrCodeNotImplemented    = "NOT_IMPLEMENTED"
)

// CLI error codes: ErrCodeMissingSubcommand when no subcommand is given,
// ErrCodeUnknownSubcommand when the subcommand is not in the catalog, and
// ErrCodeInvalidFlags when flag parsing or flag combinations fail.
const (
	ErrCodeMissingSubcommand = "MISSING_SUBCOMMAND"
	ErrCodeUnknownSubcommand = "UNKNOWN_SUBCOMMAND"
	ErrCodeInvalidFlags      = "INVALID_FLAGS"
)

// Error source labels identifying the layer that produced an error payload:
// ErrSourceValidation for request validation, ErrSourceDispatch for command
// routing, ErrSourceBackend for the backend service, and ErrSourceAdapter for
// integration adapters.
const (
	ErrSourceValidation = "validation"
	ErrSourceDispatch   = "dispatch"
	ErrSourceBackend    = "backend"
	ErrSourceAdapter    = "adapter"
)
