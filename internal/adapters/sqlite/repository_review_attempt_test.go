package sqlite

import (
	"context"
	"path/filepath"
	"reflect"
	"testing"
	"time"

	"github.com/bonztm/agent-workflow-manager/internal/core"
)

func TestSaveAndListReviewAttempts_RoundTrip(t *testing.T) {
	ctx := context.Background()
	repo, err := New(ctx, Config{Path: filepath.Join(t.TempDir(), "ctx.sqlite")})
	if err != nil {
		t.Fatalf("new repository: %v", err)
	}
	t.Cleanup(func() { _ = repo.Close() })
	if err := repo.UpsertReceiptScope(ctx, core.ReceiptScope{
		ProjectID: "project.alpha",
		ReceiptID: "receipt.abc123",
		TaskText:  "review receipt seed",
		Phase:     "review",
	}); err != nil {
		t.Fatalf("seed receipt scope: %v", err)
	}

	exitCode := 1
	firstCreatedAt := time.Date(2026, 3, 7, 20, 0, 0, 0, time.UTC)
	secondCreatedAt := firstCreatedAt.Add(90 * time.Second)

	firstID, err := repo.SaveReviewAttempt(ctx, core.ReviewAttempt{
		ProjectID:          " project.alpha ",
		ReceiptID:          " receipt.abc123 ",
		PlanKey:            " plan:receipt.abc123 ",
		ReviewKey:          " review:cross-llm ",
		Summary:            " Cross-LLM review ",
		Fingerprint:        " sha256:first ",
		Status:             " failed ",
		Passed:             false,
		Outcome:            " Found blocking drift ",
		WorkflowSourcePath: " .awm/awm-workflows.yaml ",
		CommandArgv:        []string{" scripts/awm-cross-review.sh ", "", " --strict "},
		CommandCWD:         " . ",
		TimeoutSec:         600,
		ExitCode:           &exitCode,
		TimedOut:           false,
		StdoutExcerpt:      " stdout ",
		StderrExcerpt:      " stderr ",
		CreatedAt:          firstCreatedAt,
	})
	if err != nil {
		t.Fatalf("save first review attempt: %v", err)
	}

	secondID, err := repo.SaveReviewAttempt(ctx, core.ReviewAttempt{
		ProjectID:          "project.alpha",
		ReceiptID:          "receipt.abc123",
		PlanKey:            "plan:receipt.abc123",
		ReviewKey:          "review:cross-llm",
		Summary:            "Cross-LLM review",
		Fingerprint:        "sha256:second",
		Status:             "passed",
		Passed:             true,
		Outcome:            "No blocking findings",
		WorkflowSourcePath: ".awm/awm-workflows.yaml",
		CommandArgv:        []string{"scripts/awm-cross-review.sh"},
		CommandCWD:         ".",
		TimeoutSec:         600,
		CreatedAt:          secondCreatedAt,
	})
	if err != nil {
		t.Fatalf("save second review attempt: %v", err)
	}

	got, err := repo.ListReviewAttempts(ctx, core.ReviewAttemptListQuery{
		ProjectID: " project.alpha ",
		ReceiptID: " receipt.abc123 ",
		ReviewKey: " review:cross-llm ",
	})
	if err != nil {
		t.Fatalf("list review attempts: %v", err)
	}

	want := []core.ReviewAttempt{
		{
			AttemptID:          firstID,
			ProjectID:          "project.alpha",
			ReceiptID:          "receipt.abc123",
			PlanKey:            "plan:receipt.abc123",
			ReviewKey:          "review:cross-llm",
			Summary:            "Cross-LLM review",
			Fingerprint:        "sha256:first",
			Status:             "failed",
			Passed:             false,
			Outcome:            "Found blocking drift",
			WorkflowSourcePath: ".awm/awm-workflows.yaml",
			CommandArgv:        []string{"scripts/awm-cross-review.sh", "--strict"},
			CommandCWD:         ".",
			TimeoutSec:         600,
			ExitCode:           &exitCode,
			TimedOut:           false,
			StdoutExcerpt:      "stdout",
			StderrExcerpt:      "stderr",
			CreatedAt:          firstCreatedAt,
		},
		{
			AttemptID:          secondID,
			ProjectID:          "project.alpha",
			ReceiptID:          "receipt.abc123",
			PlanKey:            "plan:receipt.abc123",
			ReviewKey:          "review:cross-llm",
			Summary:            "Cross-LLM review",
			Fingerprint:        "sha256:second",
			Status:             "passed",
			Passed:             true,
			Outcome:            "No blocking findings",
			WorkflowSourcePath: ".awm/awm-workflows.yaml",
			CommandArgv:        []string{"scripts/awm-cross-review.sh"},
			CommandCWD:         ".",
			TimeoutSec:         600,
			ExitCode:           nil,
			TimedOut:           false,
			StdoutExcerpt:      "",
			StderrExcerpt:      "",
			CreatedAt:          secondCreatedAt,
		},
	}
	if !reflect.DeepEqual(got, want) {
		t.Fatalf("unexpected persisted review attempts:\n got: %#v\nwant: %#v", got, want)
	}
}
