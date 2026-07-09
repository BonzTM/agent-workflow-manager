//go:build integration

package integration

import (
	"context"
	"fmt"
	"os"
	"reflect"
	"strings"
	"testing"
	"time"

	postgresrepo "github.com/bonztm/agent-workflow-manager/internal/adapters/postgres"
	"github.com/bonztm/agent-workflow-manager/internal/core"
)

func TestPostgresReviewAttempts_RoundTrip(t *testing.T) {
	dsn := strings.TrimSpace(os.Getenv(integrationDSNEnvVar))
	if dsn == "" {
		t.Skipf("%s is required", integrationDSNEnvVar)
	}

	ctx, cancel := context.WithTimeout(context.Background(), 90*time.Second)
	defer cancel()

	repo, err := postgresrepo.New(ctx, postgresrepo.Config{DSN: dsn})
	if err != nil {
		t.Fatalf("new postgres repository: %v", err)
	}
	t.Cleanup(repo.Close)

	projectID := fmt.Sprintf("step11.review.%d", time.Now().UTC().UnixNano())
	receiptID := "receipt.review-step11"
	reviewKey := "review:cross-llm"
	planKey := "plan:" + receiptID
	firstCreatedAt := time.Date(2026, 3, 7, 21, 0, 0, 0, time.UTC)
	secondCreatedAt := firstCreatedAt.Add(2 * time.Minute)
	firstExitCode := 1
	if err := repo.UpsertReceiptScope(ctx, core.ReceiptScope{
		ProjectID: projectID,
		ReceiptID: receiptID,
		TaskText:  "postgres review attempt roundtrip",
		Phase:     "review",
	}); err != nil {
		t.Fatalf("seed receipt scope: %v", err)
	}

	firstID, err := repo.SaveReviewAttempt(ctx, core.ReviewAttempt{
		ProjectID:          " " + projectID + " ",
		ReceiptID:          " " + receiptID + " ",
		PlanKey:            " " + planKey + " ",
		ReviewKey:          " " + reviewKey + " ",
		Summary:            " Cross-LLM review ",
		Fingerprint:        " sha256:first ",
		Status:             " failed ",
		Passed:             false,
		Outcome:            " Blocking findings remain ",
		WorkflowSourcePath: " .awm/awm-workflows.yaml ",
		CommandArgv:        []string{" scripts/awm-cross-review.sh ", "", " --strict "},
		CommandCWD:         " . ",
		TimeoutSec:         900,
		ExitCode:           &firstExitCode,
		StdoutExcerpt:      " stdout ",
		StderrExcerpt:      " stderr ",
		CreatedAt:          firstCreatedAt,
	})
	if err != nil {
		t.Fatalf("save first review attempt: %v", err)
	}

	secondID, err := repo.SaveReviewAttempt(ctx, core.ReviewAttempt{
		ProjectID:          projectID,
		ReceiptID:          receiptID,
		PlanKey:            planKey,
		ReviewKey:          reviewKey,
		Summary:            "Cross-LLM review",
		Fingerprint:        "sha256:second",
		Status:             "passed",
		Passed:             true,
		Outcome:            "No blocking findings",
		WorkflowSourcePath: ".awm/awm-workflows.yaml",
		CommandArgv:        []string{"scripts/awm-cross-review.sh"},
		CommandCWD:         ".",
		TimeoutSec:         900,
		CreatedAt:          secondCreatedAt,
	})
	if err != nil {
		t.Fatalf("save second review attempt: %v", err)
	}

	got, err := repo.ListReviewAttempts(ctx, core.ReviewAttemptListQuery{
		ProjectID: " " + projectID + " ",
		ReceiptID: " " + receiptID + " ",
		ReviewKey: " " + reviewKey + " ",
	})
	if err != nil {
		t.Fatalf("list review attempts: %v", err)
	}
	// pgx scans timestamptz into the session zone; normalize to UTC so the
	// comparison checks the instant, not the driver's presentation zone.
	for i := range got {
		got[i].CreatedAt = got[i].CreatedAt.UTC()
	}

	want := []core.ReviewAttempt{
		{
			AttemptID:          firstID,
			ProjectID:          projectID,
			ReceiptID:          receiptID,
			PlanKey:            planKey,
			ReviewKey:          reviewKey,
			Summary:            "Cross-LLM review",
			Fingerprint:        "sha256:first",
			Status:             "failed",
			Passed:             false,
			Outcome:            "Blocking findings remain",
			WorkflowSourcePath: ".awm/awm-workflows.yaml",
			CommandArgv:        []string{"scripts/awm-cross-review.sh", "--strict"},
			CommandCWD:         ".",
			TimeoutSec:         900,
			ExitCode:           &firstExitCode,
			TimedOut:           false,
			StdoutExcerpt:      "stdout",
			StderrExcerpt:      "stderr",
			CreatedAt:          firstCreatedAt,
		},
		{
			AttemptID:          secondID,
			ProjectID:          projectID,
			ReceiptID:          receiptID,
			PlanKey:            planKey,
			ReviewKey:          reviewKey,
			Summary:            "Cross-LLM review",
			Fingerprint:        "sha256:second",
			Status:             "passed",
			Passed:             true,
			Outcome:            "No blocking findings",
			WorkflowSourcePath: ".awm/awm-workflows.yaml",
			CommandArgv:        []string{"scripts/awm-cross-review.sh"},
			CommandCWD:         ".",
			TimeoutSec:         900,
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
