package backend

import (
	"github.com/bonztm/agent-workflow-manager/internal/contracts/v1"
	"github.com/bonztm/agent-workflow-manager/internal/core"
)

func backendError(code, message string, details any) *core.APIError {
	return core.NewErrorWithSource(code, message, v1.ErrSourceBackend, details)
}
