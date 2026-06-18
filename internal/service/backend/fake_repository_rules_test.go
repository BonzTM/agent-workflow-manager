package backend

import (
	"context"

	"github.com/bonztm/agent-workflow-manager/internal/core"
)

func (f *fakeRepository) SyncRulePointers(_ context.Context, _ core.RulePointerSyncInput) (core.RulePointerSyncResult, error) {
	return core.RulePointerSyncResult{}, nil
}
