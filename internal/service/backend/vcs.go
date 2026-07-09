package backend

import (
	"context"
	"strings"
	"time"

	"github.com/bonztm/agent-workflow-manager/internal/core"
)

// resolveVCSMetadata reports the repo's current HEAD sha and branch via the
// service's injectable detector, best-effort: any failure yields the zero
// value so completion reporting never depends on git being available.
func (s *Service) resolveVCSMetadata() core.VCSInfo {
	if s == nil || s.detectVCSMetadata == nil {
		return core.VCSInfo{}
	}
	return s.detectVCSMetadata(s.defaultProjectRoot())
}

// detectGitVCSMetadata is the default detector: it asks git for HEAD's sha
// and abbreviated branch under projectRoot with a short timeout. A detached
// HEAD reports branch "HEAD"; a missing repo or git binary reports nothing.
func detectGitVCSMetadata(projectRoot string) core.VCSInfo {
	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()

	sha, err := runGitCommand(ctx, projectRoot, "rev-parse", "HEAD")
	if err != nil {
		return core.VCSInfo{}
	}
	branch, err := runGitCommand(ctx, projectRoot, "rev-parse", "--abbrev-ref", "HEAD")
	if err != nil {
		return core.VCSInfo{Sha: strings.TrimSpace(sha)}
	}
	return core.VCSInfo{
		Sha:    strings.TrimSpace(sha),
		Branch: strings.TrimSpace(branch),
	}
}
