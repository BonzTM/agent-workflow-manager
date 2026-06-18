package logging

const (
	OperationContext       = "context"
	OperationFetch         = "fetch"
	OperationExport        = "export"
	OperationReview        = "review"
	OperationWork          = "work"
	OperationHistorySearch = "history"
	OperationDone          = "done"
	OperationSync          = "sync"
	OperationHealth        = "health"
	OperationStatus        = "status"
	OperationVerify        = "verify"
	OperationInit          = "init"
)

const (
	EventServiceOperationStart  = "service.operation.start"
	EventServiceOperationFinish = "service.operation.finish"

	EventCLIIngressRead     = "cli.ingress.read"
	EventCLIIngressValidate = "cli.ingress.validate"
	EventCLIDispatch        = "cli.dispatch"
	EventCLIResult          = "cli.result"
	EventCLIFailure         = "cli.failure"

	EventMCPIngressRead     = "mcp.ingress.read"
	EventMCPIngressValidate = "mcp.ingress.validate"
	EventMCPDispatch        = "mcp.dispatch"
	EventMCPResult          = "mcp.result"
	EventMCPFailure         = "mcp.failure"

	EventAWMRun    = "awm.run"
	EventAWMMCP    = "awm.mcp"
	EventAWMIORead = "awm.io.read"
)
