package backend

import (
	"context"
	"reflect"
	"testing"
	"time"

	"github.com/bonztm/agent-workflow-manager/internal/contracts/v1"
	"github.com/bonztm/agent-workflow-manager/internal/core"
)

func TestHistorySearch_RunItemsSurfaceFilesChanged(t *testing.T) {
	repo := &fakeRepository{
		runHistoryResults: [][]core.RunHistorySummary{{
			{
				RunID:        41,
				ReceiptID:    "receipt.files123",
				Status:       "accepted",
				Outcome:      "files changed surfaced",
				FilesChanged: []string{"internal/service/backend/history.go", "docs/cli-reference.md"},
				UpdatedAt:    time.Date(2026, 7, 9, 12, 0, 0, 0, time.UTC),
			},
		}},
	}
	svc, err := New(repo)
	if err != nil {
		t.Fatalf("new service: %v", err)
	}

	result, apiErr := svc.HistorySearch(context.Background(), v1.HistorySearchPayload{
		ProjectID: "project.alpha",
		Entity:    v1.HistoryEntityRun,
	})
	if apiErr != nil {
		t.Fatalf("unexpected API error: %+v", apiErr)
	}
	if len(result.Items) != 1 {
		t.Fatalf("expected one run item, got %+v", result.Items)
	}
	want := []string{"internal/service/backend/history.go", "docs/cli-reference.md"}
	if !reflect.DeepEqual(result.Items[0].FilesChanged, want) {
		t.Fatalf("expected files_changed on run history item: got %v want %v", result.Items[0].FilesChanged, want)
	}
}

func TestDone_RecordsVCSMetadataBestEffort(t *testing.T) {
	repo := &fakeRepository{
		scopeResults: []core.ReceiptScope{{
			ProjectID: "project.alpha",
			ReceiptID: "receipt.vcs123",
			TaskText:  "record vcs metadata",
			Phase:     "execute",
		}},
	}
	svc, err := New(repo)
	if err != nil {
		t.Fatalf("new service: %v", err)
	}
	svc.detectVCSMetadata = func(projectRoot string) core.VCSInfo {
		return core.VCSInfo{Sha: "abc123def456", Branch: "feat/vcs-metadata"}
	}

	_, apiErr := svc.Done(context.Background(), v1.DonePayload{
		ProjectID:     "project.alpha",
		ReceiptID:     "receipt.vcs123",
		Outcome:       "vcs metadata recorded",
		NoFileChanges: true,
	})
	if apiErr != nil {
		t.Fatalf("unexpected API error: %+v", apiErr)
	}
	if len(repo.saveCalls) != 1 {
		t.Fatalf("expected one run summary save, got %d", len(repo.saveCalls))
	}
	saved := repo.saveCalls[0]
	if saved.VCS.Sha != "abc123def456" || saved.VCS.Branch != "feat/vcs-metadata" {
		t.Fatalf("expected vcs metadata on run summary, got %+v", saved.VCS)
	}
}

func TestDone_AbsentVCSMetadataStaysZero(t *testing.T) {
	repo := &fakeRepository{
		scopeResults: []core.ReceiptScope{{
			ProjectID: "project.alpha",
			ReceiptID: "receipt.novcs123",
			TaskText:  "no vcs available",
			Phase:     "execute",
		}},
	}
	svc, err := New(repo)
	if err != nil {
		t.Fatalf("new service: %v", err)
	}
	svc.detectVCSMetadata = func(projectRoot string) core.VCSInfo {
		return core.VCSInfo{}
	}

	_, apiErr := svc.Done(context.Background(), v1.DonePayload{
		ProjectID:     "project.alpha",
		ReceiptID:     "receipt.novcs123",
		Outcome:       "no vcs recorded",
		NoFileChanges: true,
	})
	if apiErr != nil {
		t.Fatalf("unexpected API error: %+v", apiErr)
	}
	if len(repo.saveCalls) != 1 {
		t.Fatalf("expected one run summary save, got %d", len(repo.saveCalls))
	}
	if repo.saveCalls[0].VCS != (core.VCSInfo{}) {
		t.Fatalf("expected zero vcs metadata, got %+v", repo.saveCalls[0].VCS)
	}
}
