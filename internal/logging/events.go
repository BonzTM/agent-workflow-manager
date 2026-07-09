package logging

const (
	// OperationContext names the context-assembly service operation in log events.
	OperationContext = "context"
	// OperationFetch names the fetch service operation in log events.
	OperationFetch = "fetch"
	// OperationExport names the export service operation in log events.
	OperationExport = "export"
	// OperationReview names the review service operation in log events.
	OperationReview = "review"
	// OperationWork names the work service operation in log events.
	OperationWork = "work"
	// OperationHistorySearch names the history-search service operation in log events.
	OperationHistorySearch = "history"
	// OperationDone names the done service operation in log events.
	OperationDone = "done"
	// OperationSync names the sync service operation in log events.
	OperationSync = "sync"
	// OperationHealth names the health service operation in log events.
	OperationHealth = "health"
	// OperationStatus names the status service operation in log events.
	OperationStatus = "status"
	// OperationVerify names the verify service operation in log events.
	OperationVerify = "verify"
	// OperationInit names the init service operation in log events.
	OperationInit = "init"
)

const (
	// EventServiceOperationStart marks the start of a service-layer operation.
	EventServiceOperationStart = "service.operation.start"
	// EventServiceOperationFinish marks the completion of a service-layer operation.
	EventServiceOperationFinish = "service.operation.finish"

	// EventCLIIngressRead marks the CLI reading raw input for a command.
	EventCLIIngressRead = "cli.ingress.read"
	// EventCLIIngressValidate marks the CLI validating parsed command input.
	EventCLIIngressValidate = "cli.ingress.validate"
	// EventCLIDispatch marks the CLI dispatching a command to the service layer.
	EventCLIDispatch = "cli.dispatch"
	// EventCLIResult marks a CLI command completing successfully.
	EventCLIResult = "cli.result"
	// EventCLIFailure marks a CLI command failing.
	EventCLIFailure = "cli.failure"

	// EventMCPIngressRead marks the MCP server reading a raw tool request.
	EventMCPIngressRead = "mcp.ingress.read"
	// EventMCPIngressValidate marks the MCP server validating a tool request.
	EventMCPIngressValidate = "mcp.ingress.validate"
	// EventMCPDispatch marks the MCP server dispatching a request to the service layer.
	EventMCPDispatch = "mcp.dispatch"
	// EventMCPResult marks an MCP tool request completing successfully.
	EventMCPResult = "mcp.result"
	// EventMCPFailure marks an MCP tool request failing.
	EventMCPFailure = "mcp.failure"

	// EventAWMRun marks a top-level awm CLI invocation.
	EventAWMRun = "awm.run"
	// EventAWMMCP marks a top-level awm MCP server invocation.
	EventAWMMCP = "awm.mcp"
	// EventAWMIORead marks awm reading from an input/output stream.
	EventAWMIORead = "awm.io.read"
)
