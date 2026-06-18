package cli

import (
	"bytes"
	"context"
	"encoding/json"
	"io"
	"os"
	"path/filepath"
	"reflect"
	"strings"
	"testing"
	"time"

	"github.com/bonztm/agent-workflow-manager/internal/contracts/v1"
	"github.com/bonztm/agent-workflow-manager/internal/core"
	"github.com/bonztm/agent-workflow-manager/internal/logging"
	"github.com/bonztm/agent-workflow-manager/internal/runtime"
)

func TestParseExpectedVersion(t *testing.T) {
	key, version, err := parseExpectedVersion("plan:req-12345678=v4")
	if err != nil {
		t.Fatalf("expected no error, got %v", err)
	}
	if key != "plan:req-12345678" {
		t.Fatalf("unexpected key: %q", key)
	}
	if version != "v4" {
		t.Fatalf("unexpected version: %q", version)
	}

	if _, _, err := parseExpectedVersion("missing-separator"); err == nil {
		t.Fatal("expected error for invalid value")
	}
}

func TestBuildFetchEnvelope_ParsesRepeatedFlags(t *testing.T) {
	env, err := buildConvenienceEnvelope("fetch", []string{
		"--project", "myproject",
		"--request-id", "req-12345678",
		"--key", "plan:req-12345678",
		"--key", "rule:myproject/rule-1",
		"--expect", "plan:req-12345678=v4",
		"--expect", "rule:myproject/rule-1=v2",
	}, fixedNow)
	if err != nil {
		t.Fatalf("buildConvenienceEnvelope returned error: %v", err)
	}
	if env.Command != v1.CommandFetch {
		t.Fatalf("unexpected command: %s", env.Command)
	}
	if env.RequestID != "req-12345678" {
		t.Fatalf("unexpected request id: %s", env.RequestID)
	}

	var payload v1.FetchPayload
	if err := json.Unmarshal(env.Payload, &payload); err != nil {
		t.Fatalf("failed to decode payload: %v", err)
	}
	if payload.ProjectID != "myproject" {
		t.Fatalf("unexpected project id: %s", payload.ProjectID)
	}
	if len(payload.Keys) != 2 {
		t.Fatalf("expected 2 keys, got %d", len(payload.Keys))
	}
	if payload.ExpectedVersions["plan:req-12345678"] != "v4" {
		t.Fatalf("unexpected expected version for plan key: %q", payload.ExpectedVersions["plan:req-12345678"])
	}
	if payload.ExpectedVersions["rule:myproject/rule-1"] != "v2" {
		t.Fatalf("unexpected expected version for rule key: %q", payload.ExpectedVersions["rule:myproject/rule-1"])
	}
}

func TestBuildFetchEnvelope_LoadsKeysFile(t *testing.T) {
	keysPath := filepath.Join(t.TempDir(), "keys.json")
	if err := os.WriteFile(keysPath, []byte(`["plan:req-12345678","rule:myproject/rule-2"]`), 0o644); err != nil {
		t.Fatalf("write keys fixture: %v", err)
	}

	env, err := buildConvenienceEnvelope("fetch", []string{
		"--project", "myproject",
		"--keys-file", keysPath,
		"--receipt-id", "req-87654321",
	}, fixedNow)
	if err != nil {
		t.Fatalf("buildConvenienceEnvelope returned error: %v", err)
	}

	var payload v1.FetchPayload
	if err := json.Unmarshal(env.Payload, &payload); err != nil {
		t.Fatalf("failed to decode payload: %v", err)
	}
	if payload.ReceiptID != "req-87654321" {
		t.Fatalf("unexpected receipt id: %s", payload.ReceiptID)
	}
	if len(payload.Keys) != 2 {
		t.Fatalf("expected 2 keys from file, got %d", len(payload.Keys))
	}
}

func TestBuildFetchEnvelope_LoadsInlineJSON(t *testing.T) {
	env, err := buildConvenienceEnvelope("fetch", []string{
		"--project", "myproject",
		"--keys-json", `["plan:req-12345678","rule:myproject/rule-2"]`,
		"--expected-versions-json", `{"plan:req-12345678":"v3","rule:myproject/rule-2":"v7"}`,
	}, fixedNow)
	if err != nil {
		t.Fatalf("buildConvenienceEnvelope returned error: %v", err)
	}

	var payload v1.FetchPayload
	if err := json.Unmarshal(env.Payload, &payload); err != nil {
		t.Fatalf("failed to decode payload: %v", err)
	}
	if len(payload.Keys) != 2 {
		t.Fatalf("expected 2 keys from inline json, got %d", len(payload.Keys))
	}
	if payload.ExpectedVersions["plan:req-12345678"] != "v3" {
		t.Fatalf("unexpected version for plan key: %q", payload.ExpectedVersions["plan:req-12345678"])
	}
	if payload.ExpectedVersions["rule:myproject/rule-2"] != "v7" {
		t.Fatalf("unexpected version for rule key: %q", payload.ExpectedVersions["rule:myproject/rule-2"])
	}
}

func TestBuildFetchEnvelope_ReceiptShorthandOmitsEmptyKeys(t *testing.T) {
	env, err := buildConvenienceEnvelope("fetch", []string{
		"--project", "myproject",
		"--receipt-id", "req-87654321",
	}, fixedNow)
	if err != nil {
		t.Fatalf("buildConvenienceEnvelope returned error: %v", err)
	}

	var raw map[string]any
	if err := json.Unmarshal(env.Payload, &raw); err != nil {
		t.Fatalf("failed to decode raw payload: %v", err)
	}
	if _, ok := raw["keys"]; ok {
		t.Fatalf("expected keys to be omitted for receipt shorthand payload, got %v", raw["keys"])
	}

	var payload v1.FetchPayload
	if err := json.Unmarshal(env.Payload, &payload); err != nil {
		t.Fatalf("failed to decode payload: %v", err)
	}
	if payload.ReceiptID != "req-87654321" {
		t.Fatalf("unexpected receipt id: %s", payload.ReceiptID)
	}
	if len(payload.Keys) != 0 {
		t.Fatalf("expected no keys, got %d", len(payload.Keys))
	}
}

func TestBuildContextEnvelope_LowersToExportWhenFormatRequested(t *testing.T) {
	env, err := buildConvenienceEnvelope("context", []string{
		"--project", "myproject",
		"--task-text", "Add sync checks",
		"--phase", "execute",
		"--format", "markdown",
	}, fixedNow)
	if err != nil {
		t.Fatalf("buildConvenienceEnvelope returned error: %v", err)
	}
	if env.Command != v1.CommandExport {
		t.Fatalf("unexpected command: %s", env.Command)
	}

	var payload v1.ExportPayload
	if err := json.Unmarshal(env.Payload, &payload); err != nil {
		t.Fatalf("failed to decode payload: %v", err)
	}
	if payload.Format != v1.ExportFormatMarkdown {
		t.Fatalf("unexpected format: %q", payload.Format)
	}
	if payload.Context == nil || payload.Context.TaskText != "Add sync checks" {
		t.Fatalf("unexpected export context payload: %+v", payload.Context)
	}
}

func TestBuildFetchEnvelope_LowersToExportWhenFormatRequested(t *testing.T) {
	env, err := buildConvenienceEnvelope("fetch", []string{
		"--project", "myproject",
		"--key", "plan:req-12345678",
		"--expect", "plan:req-12345678=v3",
		"--format", "json",
	}, fixedNow)
	if err != nil {
		t.Fatalf("buildConvenienceEnvelope returned error: %v", err)
	}
	if env.Command != v1.CommandExport {
		t.Fatalf("unexpected command: %s", env.Command)
	}

	var payload v1.ExportPayload
	if err := json.Unmarshal(env.Payload, &payload); err != nil {
		t.Fatalf("failed to decode payload: %v", err)
	}
	if payload.Fetch == nil || len(payload.Fetch.Keys) != 1 {
		t.Fatalf("unexpected export fetch payload: %+v", payload.Fetch)
	}
	if payload.Fetch.ExpectedVersions["plan:req-12345678"] != "v3" {
		t.Fatalf("unexpected expected version map: %+v", payload.Fetch.ExpectedVersions)
	}
}

func TestBuildHistoryEnvelope_LowersToExportWhenFormatRequested(t *testing.T) {
	env, err := buildConvenienceEnvelope("history", []string{
		"--project", "myproject",
		"--entity", "work",
		"--scope", "current",
		"--format", "markdown",
	}, fixedNow)
	if err != nil {
		t.Fatalf("buildConvenienceEnvelope returned error: %v", err)
	}
	if env.Command != v1.CommandExport {
		t.Fatalf("unexpected command: %s", env.Command)
	}

	var payload v1.ExportPayload
	if err := json.Unmarshal(env.Payload, &payload); err != nil {
		t.Fatalf("failed to decode payload: %v", err)
	}
	if payload.History == nil || payload.History.Entity != v1.HistoryEntityWork || payload.History.Scope != v1.HistoryScopeCurrent {
		t.Fatalf("unexpected export history payload: %+v", payload.History)
	}
}

func TestBuildStatusEnvelope_LowersToExportWhenFormatRequested(t *testing.T) {
	env, err := buildConvenienceEnvelope("status", []string{
		"--project", "myproject",
		"--task-text", "inspect export readiness",
		"--phase", "execute",
		"--format", "json",
	}, fixedNow)
	if err != nil {
		t.Fatalf("buildConvenienceEnvelope returned error: %v", err)
	}
	if env.Command != v1.CommandExport {
		t.Fatalf("unexpected command: %s", env.Command)
	}

	var payload v1.ExportPayload
	if err := json.Unmarshal(env.Payload, &payload); err != nil {
		t.Fatalf("failed to decode payload: %v", err)
	}
	if payload.Status == nil || payload.Status.TaskText != "inspect export readiness" {
		t.Fatalf("unexpected export status payload: %+v", payload.Status)
	}
}

func TestBuildReadSurfaceExportFlags_RejectForceWithoutOutFile(t *testing.T) {
	_, err := buildConvenienceEnvelope("context", []string{
		"--task-text", "Add sync checks",
		"--phase", "execute",
		"--format", "markdown",
		"--force",
	}, fixedNow)
	if err == nil {
		t.Fatal("expected validation error")
	}
	if !strings.Contains(err.Error(), "--force requires --out-file") {
		t.Fatalf("unexpected error: %v", err)
	}
}

func TestBuildWorkEnvelope_LoadsTasksFile(t *testing.T) {
	tasksPath := filepath.Join(t.TempDir(), "tasks.json")
	if err := os.WriteFile(tasksPath, []byte(`[
		{"key":"verify:tests","summary":"Run tests","status":"pending"},
		{"key":"verify:diff-review","summary":"Review diff","status":"complete","outcome":"No regressions"}
	]`), 0o644); err != nil {
		t.Fatalf("failed to write test fixture: %v", err)
	}

	env, err := buildConvenienceEnvelope("work", []string{
		"--project", "myproject",
		"--request-id", "req-12345678",
		"--receipt-id", "req-87654321",
		"--tasks-file", tasksPath,
	}, fixedNow)
	if err != nil {
		t.Fatalf("buildConvenienceEnvelope returned error: %v", err)
	}
	if env.Command != v1.CommandWork {
		t.Fatalf("unexpected command: %s", env.Command)
	}

	var payload v1.WorkPayload
	if err := json.Unmarshal(env.Payload, &payload); err != nil {
		t.Fatalf("failed to decode payload: %v", err)
	}
	if payload.ReceiptID != "req-87654321" {
		t.Fatalf("unexpected receipt id: %s", payload.ReceiptID)
	}
	if len(payload.Tasks) != 2 {
		t.Fatalf("expected 2 work tasks, got %d", len(payload.Tasks))
	}
	if payload.Tasks[0].Key != "verify:tests" {
		t.Fatalf("unexpected first task key: %q", payload.Tasks[0].Key)
	}
}

func TestBuildWorkEnvelope_LoadsTasksJSON(t *testing.T) {
	env, err := buildConvenienceEnvelope("work", []string{
		"--project", "myproject",
		"--request-id", "req-12345678",
		"--receipt-id", "req-87654321",
		"--tasks-json", `[{"key":"verify:tests","summary":"Run tests","status":"pending"}]`,
	}, fixedNow)
	if err != nil {
		t.Fatalf("buildConvenienceEnvelope returned error: %v", err)
	}

	var payload v1.WorkPayload
	if err := json.Unmarshal(env.Payload, &payload); err != nil {
		t.Fatalf("failed to decode payload: %v", err)
	}
	if len(payload.Tasks) != 1 {
		t.Fatalf("expected 1 work task, got %d", len(payload.Tasks))
	}
	if payload.Tasks[0].Key != "verify:tests" {
		t.Fatalf("unexpected task key: %q", payload.Tasks[0].Key)
	}
}

func TestBuildWorkEnvelope_TasksJSONConflict(t *testing.T) {
	tasksPath := filepath.Join(t.TempDir(), "tasks.json")
	if err := os.WriteFile(tasksPath, []byte(`[]`), 0o644); err != nil {
		t.Fatalf("failed to write test fixture: %v", err)
	}

	_, err := buildConvenienceEnvelope("work", []string{
		"--project", "myproject",
		"--tasks-file", tasksPath,
		"--tasks-json", `[]`,
	}, fixedNow)
	if err == nil {
		t.Fatal("expected conflict error, got nil")
	}
	if !strings.Contains(err.Error(), "use only one of --tasks-file or --tasks-json") {
		t.Fatalf("unexpected error: %v", err)
	}
}

func TestBuildWorkEnvelope_LoadsPlanAndTasksJSON(t *testing.T) {
	env, err := buildConvenienceEnvelope("work", []string{
		"--project", "myproject",
		"--request-id", "req-12345678",
		"--plan-key", "plan:req-87654321",
		"--receipt-id", "req-87654321",
		"--mode", "replace",
		"--plan-json", `{
			"title":"Init this repo",
			"objective":"Capture spec, tasks, and outcomes in awm",
			"status":"in_progress",
			"stages":{
				"spec_outline":"complete",
				"refined_spec":"in_progress",
				"implementation_plan":"pending"
			},
			"in_scope":["internal/service/backend","cmd/awm"],
			"out_of_scope":["release automation"],
			"constraints":["no breaking APIs"],
			"references":["docs/getting-started.md"]
		}`,
		"--tasks-json", `[{
			"key":"task.bootstrap",
			"summary":"Implement bootstrap flow",
			"status":"in_progress",
			"depends_on":["task.spec"],
			"acceptance_criteria":["bootstrap persists canonical rules"],
			"references":["doc:bootstrap-spec"],
			"blocked_reason":"waiting for review",
			"outcome":"command executes end to end",
			"evidence":["go test ./..."]
		}]`,
	}, fixedNow)
	if err != nil {
		t.Fatalf("buildConvenienceEnvelope returned error: %v", err)
	}

	var payload v1.WorkPayload
	if err := json.Unmarshal(env.Payload, &payload); err != nil {
		t.Fatalf("failed to decode payload: %v", err)
	}
	if payload.Mode != v1.WorkPlanModeReplace {
		t.Fatalf("unexpected mode: %q", payload.Mode)
	}
	if payload.Plan == nil {
		t.Fatal("expected plan payload")
	}
	if payload.Plan.Title != "Init this repo" || payload.Plan.Objective != "Capture spec, tasks, and outcomes in awm" {
		t.Fatalf("unexpected plan metadata: %+v", payload.Plan)
	}
	if payload.Plan.Stages == nil || payload.Plan.Stages.SpecOutline != v1.WorkItemStatusComplete {
		t.Fatalf("unexpected plan stages: %+v", payload.Plan.Stages)
	}
	if len(payload.Tasks) != 1 {
		t.Fatalf("expected 1 task, got %d", len(payload.Tasks))
	}
	if payload.Tasks[0].Key != "task.bootstrap" || payload.Tasks[0].Status != v1.WorkItemStatusInProgress {
		t.Fatalf("unexpected task payload: %+v", payload.Tasks[0])
	}
}

func TestBuildWorkEnvelope_MergesDiscoveredPathsIntoPlan(t *testing.T) {
	env, err := buildConvenienceEnvelope("work", []string{
		"--project", "myproject",
		"--receipt-id", "receipt-87654321",
		"--discovered-path", "cmd/awm/routes.go",
		"--discovered-path", "internal/contracts/v1/command_catalog.go",
	}, fixedNow)
	if err != nil {
		t.Fatalf("buildConvenienceEnvelope returned error: %v", err)
	}

	var payload v1.WorkPayload
	if err := json.Unmarshal(env.Payload, &payload); err != nil {
		t.Fatalf("failed to decode payload: %v", err)
	}
	if payload.Plan == nil {
		t.Fatal("expected plan payload for discovered paths")
	}
	if got, want := payload.Plan.DiscoveredPaths, []string{
		"cmd/awm/routes.go",
		"internal/contracts/v1/command_catalog.go",
	}; !reflect.DeepEqual(got, want) {
		t.Fatalf("unexpected discovered_paths: got %v want %v", got, want)
	}
}

func TestBuildReviewEnvelope_LoadsOutcomeFileAndEvidenceJSON(t *testing.T) {
	outcomePath := filepath.Join(t.TempDir(), "review-outcome.txt")
	if err := os.WriteFile(outcomePath, []byte("Cross-LLM review passed with no blocking issues."), 0o644); err != nil {
		t.Fatalf("write outcome fixture: %v", err)
	}

	env, err := buildConvenienceEnvelope("review", []string{
		"--project", "myproject",
		"--request-id", "req-12345678",
		"--receipt-id", "receipt-87654321",
		"--outcome-file", outcomePath,
		"--evidence-json", `["review://cross-llm/run-1","comment://issue-42"]`,
	}, fixedNow)
	if err != nil {
		t.Fatalf("buildConvenienceEnvelope returned error: %v", err)
	}
	if env.Command != v1.CommandReview {
		t.Fatalf("unexpected command: %s", env.Command)
	}

	var payload v1.ReviewPayload
	if err := json.Unmarshal(env.Payload, &payload); err != nil {
		t.Fatalf("failed to decode payload: %v", err)
	}
	if payload.ReceiptID != "receipt-87654321" {
		t.Fatalf("unexpected receipt id: %q", payload.ReceiptID)
	}
	if payload.Key != "" {
		t.Fatalf("unexpected review key: %q", payload.Key)
	}
	if payload.Status != "" {
		t.Fatalf("unexpected review status: %q", payload.Status)
	}
	if payload.Outcome != "Cross-LLM review passed with no blocking issues." {
		t.Fatalf("unexpected review outcome: %q", payload.Outcome)
	}
	if len(payload.Evidence) != 2 {
		t.Fatalf("expected 2 evidence values, got %d", len(payload.Evidence))
	}
}

func TestBuildReviewEnvelope_RequiresSelectionContext(t *testing.T) {
	_, err := buildConvenienceEnvelope("review", []string{
		"--project", "myproject",
	}, fixedNow)
	if err == nil {
		t.Fatal("expected error for missing review selection context")
	}
	if !strings.Contains(err.Error(), "review requires --receipt-id or --plan-key") {
		t.Fatalf("unexpected error: %v", err)
	}
}

func TestBuildReviewEnvelope_RunModeIncludesTagsFile(t *testing.T) {
	env, err := buildConvenienceEnvelope("review", []string{
		"--project", "myproject",
		"--receipt-id", "receipt-87654321",
		"--run",
		"--tags-file", ".awm/awm-tags.yaml",
	}, fixedNow)
	if err != nil {
		t.Fatalf("buildConvenienceEnvelope returned error: %v", err)
	}

	var payload v1.ReviewPayload
	if err := json.Unmarshal(env.Payload, &payload); err != nil {
		t.Fatalf("failed to decode payload: %v", err)
	}
	if !payload.Run {
		t.Fatalf("expected run=true, got %+v", payload)
	}
	if payload.TagsFile != ".awm/awm-tags.yaml" {
		t.Fatalf("unexpected tags file: %q", payload.TagsFile)
	}
}

func TestBuildReviewEnvelope_RunModeRejectsManualOutcomeFields(t *testing.T) {
	_, err := buildConvenienceEnvelope("review", []string{
		"--project", "myproject",
		"--receipt-id", "receipt-87654321",
		"--run",
		"--outcome", "No blocking review findings.",
	}, fixedNow)
	if err == nil {
		t.Fatal("expected error for run mode with manual outcome")
	}
	if !strings.Contains(err.Error(), "--outcome and --outcome-file are only supported when --run is omitted") {
		t.Fatalf("unexpected error: %v", err)
	}
}

func TestBuildHistorySearchEnvelope_ForWorkHistoryLeavesScopeUnsetByDefault(t *testing.T) {
	env, err := buildConvenienceEnvelope("history", []string{
		"--project", "myproject",
		"--entity", "work",
		"--limit", "7",
		"--unbounded",
	}, fixedNow)
	if err != nil {
		t.Fatalf("buildConvenienceEnvelope returned error: %v", err)
	}
	if env.Command != v1.CommandHistorySearch {
		t.Fatalf("unexpected command: %s", env.Command)
	}

	var payload v1.HistorySearchPayload
	if err := json.Unmarshal(env.Payload, &payload); err != nil {
		t.Fatalf("failed to decode payload: %v", err)
	}
	if payload.ProjectID != "myproject" || payload.Entity != v1.HistoryEntityWork || payload.Scope != "" || payload.Limit != 7 || payload.Query != "" {
		t.Fatalf("unexpected payload: %+v", payload)
	}
	if payload.Unbounded == nil || !*payload.Unbounded {
		t.Fatalf("expected unbounded payload flag, got %+v", payload.Unbounded)
	}
}

func TestBuildHistorySearchEnvelope_ForWorkHistoryLoadsQueryFile(t *testing.T) {
	queryPath := filepath.Join(t.TempDir(), "query.txt")
	if err := os.WriteFile(queryPath, []byte("  bootstrap history  \n"), 0o644); err != nil {
		t.Fatalf("write query fixture: %v", err)
	}

	env, err := buildConvenienceEnvelope("history", []string{
		"--project", "myproject",
		"--entity", "work",
		"--query-file", queryPath,
		"--scope", "completed",
		"--kind", "story",
	}, fixedNow)
	if err != nil {
		t.Fatalf("buildConvenienceEnvelope returned error: %v", err)
	}

	var payload v1.HistorySearchPayload
	if err := json.Unmarshal(env.Payload, &payload); err != nil {
		t.Fatalf("failed to decode payload: %v", err)
	}
	if payload.ProjectID != "myproject" || payload.Entity != v1.HistoryEntityWork || payload.Query != "bootstrap history" || payload.Scope != v1.HistoryScopeCompleted || payload.Kind != "story" || payload.Limit != 20 {
		t.Fatalf("unexpected payload: %+v", payload)
	}
}

func TestBuildHistorySearchEnvelope_ForGenericHistoryDefaultsToAllEntities(t *testing.T) {
	env, err := buildConvenienceEnvelope("history", []string{
		"--project", "myproject",
		"--limit", "9",
	}, fixedNow)
	if err != nil {
		t.Fatalf("buildConvenienceEnvelope returned error: %v", err)
	}

	var payload v1.HistorySearchPayload
	if err := json.Unmarshal(env.Payload, &payload); err != nil {
		t.Fatalf("failed to decode payload: %v", err)
	}
	if payload.ProjectID != "myproject" || payload.Entity != v1.HistoryEntityAll || payload.Scope != "" || payload.Kind != "" || payload.Limit != 9 || payload.Query != "" {
		t.Fatalf("unexpected payload: %+v", payload)
	}
}

func TestBuildHistorySearchEnvelope_GenericHistoryRejectsWorkOnlyFlags(t *testing.T) {
	_, err := buildConvenienceEnvelope("history", []string{
		"--project", "myproject",
		"--scope", "all",
	}, fixedNow)
	if err == nil {
		t.Fatal("expected error for generic history scope flag")
	}
	if !strings.Contains(err.Error(), "scope is only supported when entity=work") {
		t.Fatalf("unexpected error: %v", err)
	}
}

func TestBuildVerifyEnvelope_RequiresSelectionContext(t *testing.T) {
	_, err := buildConvenienceEnvelope("verify", []string{
		"--project", "myproject",
	}, fixedNow)
	if err == nil {
		t.Fatal("expected error for missing verify selection context")
	}
	if !strings.Contains(err.Error(), "verify requires --test-id or selection context") {
		t.Fatalf("unexpected error: %v", err)
	}
}

func TestBuildContextEnvelope_TagsFileFlag(t *testing.T) {
	env, err := buildConvenienceEnvelope("context", []string{
		"--project", "myproject",
		"--task-text", "Check sync tags",
		"--phase", "execute",
		"--tags-file", ".awm/awm-tags.yaml",
	}, fixedNow)
	if err != nil {
		t.Fatalf("buildConvenienceEnvelope returned error: %v", err)
	}

	var payload v1.ContextPayload
	if err := json.Unmarshal(env.Payload, &payload); err != nil {
		t.Fatalf("failed to decode payload: %v", err)
	}
	if payload.TagsFile != ".awm/awm-tags.yaml" {
		t.Fatalf("unexpected tags_file: %q", payload.TagsFile)
	}
}

func TestBuildDoneEnvelope_FilesAndOutcomeFromFiles(t *testing.T) {
	filesPath := filepath.Join(t.TempDir(), "files.json")
	if err := os.WriteFile(filesPath, []byte(`["cmd/awm/main.go","cmd/awm/convenience.go"]`), 0o644); err != nil {
		t.Fatalf("write files fixture: %v", err)
	}
	outcomePath := filepath.Join(t.TempDir(), "outcome.txt")
	if err := os.WriteFile(outcomePath, []byte("Implemented script-friendly flags"), 0o644); err != nil {
		t.Fatalf("write outcome fixture: %v", err)
	}

	env, err := buildConvenienceEnvelope("done", []string{
		"--project", "myproject",
		"--receipt-id", "req-87654321",
		"--files-changed-file", filesPath,
		"--outcome-file", outcomePath,
	}, fixedNow)
	if err != nil {
		t.Fatalf("buildConvenienceEnvelope returned error: %v", err)
	}

	var payload v1.DonePayload
	if err := json.Unmarshal(env.Payload, &payload); err != nil {
		t.Fatalf("failed to decode payload: %v", err)
	}
	if len(payload.FilesChanged) != 2 {
		t.Fatalf("expected 2 changed files, got %d", len(payload.FilesChanged))
	}
	if payload.Outcome != "Implemented script-friendly flags" {
		t.Fatalf("unexpected outcome: %q", payload.Outcome)
	}
}

func TestBuildDoneEnvelope_TagsFileFlag(t *testing.T) {
	env, err := buildConvenienceEnvelope("done", []string{
		"--project", "myproject",
		"--receipt-id", "req-12345678",
		"--file-changed", "cmd/awm/main.go",
		"--outcome", "Done",
		"--tags-file", ".awm/awm-tags.yaml",
	}, fixedNow)
	if err != nil {
		t.Fatalf("buildConvenienceEnvelope returned error: %v", err)
	}

	var payload v1.DonePayload
	if err := json.Unmarshal(env.Payload, &payload); err != nil {
		t.Fatalf("failed to decode payload: %v", err)
	}
	if payload.TagsFile != ".awm/awm-tags.yaml" {
		t.Fatalf("unexpected tags_file: %q", payload.TagsFile)
	}
}

func TestBuildDoneEnvelope_AcceptsPlanKeyWithoutReceiptID(t *testing.T) {
	env, err := buildConvenienceEnvelope("done", []string{
		"--project", "myproject",
		"--plan-key", "plan:req-87654321",
		"--outcome", "Done",
	}, fixedNow)
	if err != nil {
		t.Fatalf("buildConvenienceEnvelope returned error: %v", err)
	}

	var payload v1.DonePayload
	if err := json.Unmarshal(env.Payload, &payload); err != nil {
		t.Fatalf("failed to decode payload: %v", err)
	}
	if payload.PlanKey != "plan:req-87654321" {
		t.Fatalf("unexpected plan_key: %q", payload.PlanKey)
	}
	if payload.ReceiptID != "" {
		t.Fatalf("expected empty receipt_id, got %q", payload.ReceiptID)
	}
}

func TestBuildDoneEnvelope_RequiresReceiptIDOrPlanKey(t *testing.T) {
	_, err := buildConvenienceEnvelope("done", []string{
		"--project", "myproject",
		"--outcome", "Done",
	}, fixedNow)
	if err == nil {
		t.Fatal("expected error for missing selection context")
	}
	if !strings.Contains(err.Error(), "done requires --receipt-id or --plan-key") {
		t.Fatalf("unexpected error: %v", err)
	}
}

func TestBuildDoneEnvelope_LoadsFilesChangedJSON(t *testing.T) {
	env, err := buildConvenienceEnvelope("done", []string{
		"--project", "myproject",
		"--receipt-id", "req-87654321",
		"--outcome", "Done",
		"--files-changed-json", `["cmd/awm/main.go","cmd/awm/convenience.go"]`,
	}, fixedNow)
	if err != nil {
		t.Fatalf("buildConvenienceEnvelope returned error: %v", err)
	}

	var payload v1.DonePayload
	if err := json.Unmarshal(env.Payload, &payload); err != nil {
		t.Fatalf("failed to decode payload: %v", err)
	}
	if len(payload.FilesChanged) != 2 {
		t.Fatalf("expected 2 changed files, got %d", len(payload.FilesChanged))
	}
}

func TestBuildDoneEnvelope_OmitsFilesChangedWhenNoFilesProvided(t *testing.T) {
	env, err := buildConvenienceEnvelope("done", []string{
		"--project", "myproject",
		"--receipt-id", "req-87654321",
		"--outcome", "Captured planning result",
	}, fixedNow)
	if err != nil {
		t.Fatalf("buildConvenienceEnvelope returned error: %v", err)
	}
	if strings.Contains(string(env.Payload), "\"files_changed\"") {
		t.Fatalf("expected files_changed to be omitted, got %s", env.Payload)
	}

	var payload v1.DonePayload
	if err := json.Unmarshal(env.Payload, &payload); err != nil {
		t.Fatalf("failed to decode payload: %v", err)
	}
	if payload.FilesChanged != nil {
		t.Fatalf("expected omitted files_changed to decode as nil, got %v", payload.FilesChanged)
	}
	if payload.NoFileChanges {
		t.Fatal("expected no_file_changes to default false")
	}
}

func TestBuildDoneEnvelope_ExplicitNoFileChangesFlag(t *testing.T) {
	env, err := buildConvenienceEnvelope("done", []string{
		"--project", "myproject",
		"--receipt-id", "req-87654321",
		"--outcome", "Captured planning result",
		"--no-file-changes",
	}, fixedNow)
	if err != nil {
		t.Fatalf("buildConvenienceEnvelope returned error: %v", err)
	}

	var payload v1.DonePayload
	if err := json.Unmarshal(env.Payload, &payload); err != nil {
		t.Fatalf("failed to decode payload: %v", err)
	}
	if !payload.NoFileChanges {
		t.Fatal("expected no_file_changes to be true")
	}
	if payload.FilesChanged != nil {
		t.Fatalf("expected files_changed to remain omitted, got %v", payload.FilesChanged)
	}
}

func TestBuildDoneEnvelope_RejectsFilesChangedWithExplicitNoFileChanges(t *testing.T) {
	_, err := buildConvenienceEnvelope("done", []string{
		"--project", "myproject",
		"--receipt-id", "req-87654321",
		"--outcome", "Captured planning result",
		"--file-changed", "cmd/awm/main.go",
		"--no-file-changes",
	}, fixedNow)
	if err == nil {
		t.Fatal("expected error")
	}
	if !strings.Contains(err.Error(), "--no-file-changes") {
		t.Fatalf("unexpected error: %v", err)
	}
}

func TestBuildSyncEnvelope_RulesAndTagsFileFlags(t *testing.T) {
	env, err := buildConvenienceEnvelope("sync", []string{
		"--project", "myproject",
		"--rules-file", ".awm/awm-rules.yaml",
		"--tags-file", ".awm/awm-tags.yaml",
	}, fixedNow)
	if err != nil {
		t.Fatalf("buildConvenienceEnvelope returned error: %v", err)
	}

	var payload v1.SyncPayload
	if err := json.Unmarshal(env.Payload, &payload); err != nil {
		t.Fatalf("failed to decode payload: %v", err)
	}
	if payload.RulesFile != ".awm/awm-rules.yaml" {
		t.Fatalf("unexpected rules_file: %q", payload.RulesFile)
	}
	if payload.TagsFile != ".awm/awm-tags.yaml" {
		t.Fatalf("unexpected tags_file: %q", payload.TagsFile)
	}
}

func TestBuildHealthEnvelope_FixModeSupportsRulesAndTagsFileFlags(t *testing.T) {
	env, err := buildConvenienceEnvelope("health", []string{
		"--project", "myproject",
		"--fix", "sync_ruleset",
		"--rules-file", "custom-rules.yaml",
		"--tags-file", "custom-tags.json",
	}, fixedNow)
	if err != nil {
		t.Fatalf("buildConvenienceEnvelope returned error: %v", err)
	}
	if env.Command != v1.CommandHealth {
		t.Fatalf("unexpected command: %s", env.Command)
	}

	var payload v1.HealthPayload
	if err := json.Unmarshal(env.Payload, &payload); err != nil {
		t.Fatalf("failed to decode payload: %v", err)
	}
	if payload.RulesFile != "custom-rules.yaml" {
		t.Fatalf("unexpected rules_file: %q", payload.RulesFile)
	}
	if payload.TagsFile != "custom-tags.json" {
		t.Fatalf("unexpected tags_file: %q", payload.TagsFile)
	}
}

func TestBuildHealthEnvelope_DefaultsToCheckMode(t *testing.T) {
	env, err := buildConvenienceEnvelope("health", []string{
		"--project", "myproject",
		"--include-details",
		"--max-findings-per-check", "25",
	}, fixedNow)
	if err != nil {
		t.Fatalf("buildConvenienceEnvelope returned error: %v", err)
	}
	if env.Command != v1.CommandHealth {
		t.Fatalf("unexpected command: %s", env.Command)
	}

	var payload v1.HealthPayload
	if err := json.Unmarshal(env.Payload, &payload); err != nil {
		t.Fatalf("failed to decode payload: %v", err)
	}
	if payload.IncludeDetails == nil || !*payload.IncludeDetails {
		t.Fatalf("expected include_details=true, got %+v", payload.IncludeDetails)
	}
	if payload.MaxFindingsPerCheck == nil || *payload.MaxFindingsPerCheck != 25 {
		t.Fatalf("unexpected max_findings_per_check: %+v", payload.MaxFindingsPerCheck)
	}
}

func TestBuildHealthEnvelope_FixModeWithDryRun(t *testing.T) {
	env, err := buildConvenienceEnvelope("health", []string{
		"--project", "myproject",
		"--fix", "sync_ruleset",
		"--dry-run",
	}, fixedNow)
	if err != nil {
		t.Fatalf("buildConvenienceEnvelope returned error: %v", err)
	}
	if env.Command != v1.CommandHealth {
		t.Fatalf("unexpected command: %s", env.Command)
	}

	var payload v1.HealthPayload
	if err := json.Unmarshal(env.Payload, &payload); err != nil {
		t.Fatalf("failed to decode payload: %v", err)
	}
	if payload.Apply == nil || *payload.Apply {
		t.Fatalf("expected apply=false, got %+v", payload.Apply)
	}
	if want := []v1.HealthFixer{v1.HealthFixerSyncRuleset}; !reflect.DeepEqual(payload.Fixers, want) {
		t.Fatalf("unexpected fixers: got %v want %v", payload.Fixers, want)
	}
}

func TestBuildHealthEnvelope_FixModeDefaultsToApply(t *testing.T) {
	env, err := buildConvenienceEnvelope("health", []string{
		"--project", "myproject",
		"--fix", "sync_ruleset",
	}, fixedNow)
	if err != nil {
		t.Fatalf("buildConvenienceEnvelope returned error: %v", err)
	}
	if env.Command != v1.CommandHealth {
		t.Fatalf("unexpected command: %s", env.Command)
	}

	var payload v1.HealthPayload
	if err := json.Unmarshal(env.Payload, &payload); err != nil {
		t.Fatalf("failed to decode payload: %v", err)
	}
	if payload.Apply == nil || !*payload.Apply {
		t.Fatalf("expected apply=true, got %+v", payload.Apply)
	}
	if want := []v1.HealthFixer{v1.HealthFixerSyncRuleset}; !reflect.DeepEqual(payload.Fixers, want) {
		t.Fatalf("unexpected fixers: got %v want %v", payload.Fixers, want)
	}
}

func TestBuildHealthEnvelope_AllFixer(t *testing.T) {
	env, err := buildConvenienceEnvelope("health", []string{
		"--project", "myproject",
		"--fix", "all",
		"--dry-run",
	}, fixedNow)
	if err != nil {
		t.Fatalf("buildConvenienceEnvelope returned error: %v", err)
	}

	var payload v1.HealthPayload
	if err := json.Unmarshal(env.Payload, &payload); err != nil {
		t.Fatalf("failed to decode payload: %v", err)
	}
	if want := []v1.HealthFixer{v1.HealthFixerAll}; !reflect.DeepEqual(payload.Fixers, want) {
		t.Fatalf("unexpected fixers: got %v want %v", payload.Fixers, want)
	}
}

func TestBuildHealthEnvelope_RejectsMixedCheckAndFixFlags(t *testing.T) {
	_, err := buildConvenienceEnvelope("health", []string{
		"--include-details",
		"--fix", "sync_ruleset",
	}, fixedNow)
	if err == nil {
		t.Fatal("expected mixed check/fix flags to fail")
	}
	if !strings.Contains(err.Error(), "cannot be combined") {
		t.Fatalf("unexpected error: %v", err)
	}
}

func TestBuildHealthEnvelope_RejectsFixScopedFlagsWithoutFixMode(t *testing.T) {
	_, err := buildConvenienceEnvelope("health", []string{
		"--rules-file", "custom-rules.yaml",
	}, fixedNow)
	if err == nil {
		t.Fatal("expected fix-scoped flags without fix mode to fail")
	}
	if !strings.Contains(err.Error(), "use --fix, --apply, or --dry-run") {
		t.Fatalf("unexpected error: %v", err)
	}
}

func TestBuildConvenienceEnvelope_RejectsRemovedLegacySubcommands(t *testing.T) {
	for _, subcommand := range []string{"get-context", "report-completion", "bootstrap"} {
		t.Run(subcommand, func(t *testing.T) {
			_, err := buildConvenienceEnvelope(subcommand, nil, fixedNow)
			if err == nil {
				t.Fatal("expected error")
			}
			if !strings.Contains(err.Error(), "unknown subcommand") {
				t.Fatalf("unexpected error: %v", err)
			}
		})
	}
}

func TestBuildConvenienceEnvelope_RejectsRemovedHealthSubcommands(t *testing.T) {
	for _, subcommand := range []string{"health-fix", "health-check"} {
		t.Run(subcommand, func(t *testing.T) {
			_, err := buildConvenienceEnvelope(subcommand, nil, fixedNow)
			if err == nil {
				t.Fatal("expected error")
			}
			if !strings.Contains(err.Error(), "unknown subcommand") {
				t.Fatalf("unexpected error: %v", err)
			}
		})
	}
}

func TestBuildStatusEnvelope_LoadsTaskFileAndOptionalSources(t *testing.T) {
	taskFile := filepath.Join(t.TempDir(), "task.txt")
	if err := os.WriteFile(taskFile, []byte(" diagnose context drift \n"), 0o644); err != nil {
		t.Fatalf("write task file: %v", err)
	}

	env, err := buildConvenienceEnvelope("status", []string{
		"--project", "myproject",
		"--project-root", ".",
		"--rules-file", ".awm/awm-rules.yaml",
		"--tags-file", ".awm/awm-tags.yaml",
		"--tests-file", ".awm/awm-tests.yaml",
		"--workflows-file", ".awm/awm-workflows.yaml",
		"--task-file", taskFile,
		"--phase", "review",
	}, fixedNow)
	if err != nil {
		t.Fatalf("buildConvenienceEnvelope returned error: %v", err)
	}

	var payload v1.StatusPayload
	if err := json.Unmarshal(env.Payload, &payload); err != nil {
		t.Fatalf("failed to decode payload: %v", err)
	}
	if payload.ProjectID != "myproject" {
		t.Fatalf("unexpected project_id: %q", payload.ProjectID)
	}
	if payload.ProjectRoot != "." {
		t.Fatalf("unexpected project_root: %q", payload.ProjectRoot)
	}
	if payload.RulesFile != ".awm/awm-rules.yaml" || payload.TagsFile != ".awm/awm-tags.yaml" {
		t.Fatalf("unexpected rules/tags files: %+v", payload)
	}
	if payload.TestsFile != ".awm/awm-tests.yaml" || payload.WorkflowsFile != ".awm/awm-workflows.yaml" {
		t.Fatalf("unexpected tests/workflows files: %+v", payload)
	}
	if payload.TaskText != "diagnose context drift" {
		t.Fatalf("unexpected task_text: %q", payload.TaskText)
	}
	if payload.Phase != v1.PhaseReview {
		t.Fatalf("unexpected phase: %q", payload.Phase)
	}
}

func TestBuildInitEnvelope_RulesAndTagsFileFlags(t *testing.T) {
	env, err := buildConvenienceEnvelope("init", []string{
		"--project", "myproject",
		"--project-root", ".",
		"--apply-template", "starter-contract",
		"--apply-template", "verify-go",
		"--rules-file", "legacy-rules.yaml",
		"--tags-file", "legacy-tags.json",
		"--persist-candidates",
	}, fixedNow)
	if err != nil {
		t.Fatalf("buildConvenienceEnvelope returned error: %v", err)
	}

	var payload v1.InitPayload
	if err := json.Unmarshal(env.Payload, &payload); err != nil {
		t.Fatalf("failed to decode payload: %v", err)
	}
	if payload.RulesFile != "legacy-rules.yaml" {
		t.Fatalf("unexpected rules_file: %q", payload.RulesFile)
	}
	if payload.TagsFile != "legacy-tags.json" {
		t.Fatalf("unexpected tags_file: %q", payload.TagsFile)
	}
	if want := []string{"starter-contract", "verify-go"}; !reflect.DeepEqual(payload.ApplyTemplates, want) {
		t.Fatalf("unexpected apply_templates: got %v want %v", payload.ApplyTemplates, want)
	}
	if payload.PersistCandidates == nil || !*payload.PersistCandidates {
		t.Fatalf("expected persist_candidates=true, got %+v", payload.PersistCandidates)
	}
}

func TestBuildInitEnvelope_AllowsOmittedProjectAndRoot(t *testing.T) {
	env, err := buildConvenienceEnvelope("init", []string{
		"--apply-template", "starter-contract",
	}, fixedNow)
	if err != nil {
		t.Fatalf("buildConvenienceEnvelope returned error: %v", err)
	}

	var payload v1.InitPayload
	if err := json.Unmarshal(env.Payload, &payload); err != nil {
		t.Fatalf("failed to decode payload: %v", err)
	}
	if payload.ProjectID != "" {
		t.Fatalf("expected empty project_id for runtime inference, got %q", payload.ProjectID)
	}
	if payload.ProjectRoot != "" {
		t.Fatalf("expected empty project_root for runtime inference, got %q", payload.ProjectRoot)
	}
	if want := []string{"starter-contract"}; !reflect.DeepEqual(payload.ApplyTemplates, want) {
		t.Fatalf("unexpected apply_templates: got %v want %v", payload.ApplyTemplates, want)
	}
}

func TestRunConvenienceWithDeps_EndToEndContext(t *testing.T) {
	svc := &convenienceFakeService{}
	out := &bytes.Buffer{}
	recorder := logging.NewRecorder()

	code := runConvenienceWithDeps(
		context.Background(),
		recorder,
		"context",
		[]string{"--project", "myproject", "--request-id", "req-12345678", "--task-text", "Add health checks", "--phase", "execute"},
		out,
		fixedNow,
		func(_ context.Context, _ logging.Logger) (core.Service, runtime.CleanupFunc, error) {
			return svc, func() {}, nil
		},
		RunWithLogger,
	)
	if code != 0 {
		t.Fatalf("expected exit code 0, got %d", code)
	}
	if len(svc.getContextCalls) != 1 {
		t.Fatalf("expected one context call, got %d", len(svc.getContextCalls))
	}

	var env v1.ResultEnvelope
	if err := json.Unmarshal(out.Bytes(), &env); err != nil {
		t.Fatalf("failed to parse output envelope: %v", err)
	}
	if !env.OK {
		t.Fatalf("expected ok=true, got error %+v", env.Error)
	}
	if env.Command != v1.CommandContext {
		t.Fatalf("unexpected command: %s", env.Command)
	}
	if env.RequestID != "req-12345678" {
		t.Fatalf("unexpected request id: %s", env.RequestID)
	}
}

func TestRunConvenienceWithDeps_DefaultsProjectIDFromEnv(t *testing.T) {
	t.Setenv(runtime.ProjectIDEnvVar, "env-project")

	svc := &convenienceFakeService{}
	out := &bytes.Buffer{}
	recorder := logging.NewRecorder()

	code := runConvenienceWithDeps(
		context.Background(),
		recorder,
		"context",
		[]string{"--request-id", "req-12345678", "--task-text", "Add health checks", "--phase", "execute"},
		out,
		fixedNow,
		func(_ context.Context, _ logging.Logger) (core.Service, runtime.CleanupFunc, error) {
			return svc, func() {}, nil
		},
		RunWithLogger,
	)
	if code != 0 {
		t.Fatalf("expected exit code 0, got %d", code)
	}
	if len(svc.getContextCalls) != 1 {
		t.Fatalf("expected one context call, got %d", len(svc.getContextCalls))
	}
	if got := svc.getContextCalls[0].ProjectID; got != "env-project" {
		t.Fatalf("unexpected inferred project id: %q", got)
	}
}

func TestRunConvenienceWithDeps_ExportFlagsWriteRawStdout(t *testing.T) {
	svc := &convenienceFakeService{
		exportResult: v1.ExportResult{
			Format:  v1.ExportFormatMarkdown,
			Content: "# export artifact",
			Document: &v1.ExportDocument{
				Kind: v1.ExportDocumentKindContext,
			},
		},
	}
	out := &bytes.Buffer{}

	code := runConvenienceWithDeps(
		context.Background(),
		logging.NewRecorder(),
		"context",
		[]string{"--project", "myproject", "--request-id", "req-12345678", "--task-text", "Add health checks", "--phase", "execute", "--format", "markdown"},
		out,
		fixedNow,
		func(_ context.Context, _ logging.Logger) (core.Service, runtime.CleanupFunc, error) {
			return svc, func() {}, nil
		},
		RunWithLogger,
	)
	if code != 0 {
		t.Fatalf("expected exit code 0, got %d", code)
	}
	if got := out.String(); got != "# export artifact" {
		t.Fatalf("unexpected stdout: %q", got)
	}
	if len(svc.exportCalls) != 1 {
		t.Fatalf("expected one export call, got %d", len(svc.exportCalls))
	}
	if len(svc.getContextCalls) != 0 {
		t.Fatalf("expected no direct context calls, got %d", len(svc.getContextCalls))
	}
}

func TestRunConvenienceWithDeps_ExportFlagsWriteOutFileAndRequireForce(t *testing.T) {
	target := filepath.Join(t.TempDir(), "artifact.md")
	if err := os.WriteFile(target, []byte("old artifact"), 0o644); err != nil {
		t.Fatalf("seed artifact: %v", err)
	}

	svc := &convenienceFakeService{
		exportResult: v1.ExportResult{
			Format:  v1.ExportFormatMarkdown,
			Content: "# fresh artifact",
			Document: &v1.ExportDocument{
				Kind: v1.ExportDocumentKindStatus,
			},
		},
	}
	out := &bytes.Buffer{}

	stderr := captureStderr(t, func() {
		code := runConvenienceWithDeps(
			context.Background(),
			logging.NewRecorder(),
			"status",
			[]string{"--project", "myproject", "--format", "markdown", "--out-file", target},
			out,
			fixedNow,
			func(_ context.Context, _ logging.Logger) (core.Service, runtime.CleanupFunc, error) {
				return svc, func() {}, nil
			},
			RunWithLogger,
		)
		if code != 1 {
			t.Fatalf("expected exit code 1 without force, got %d", code)
		}
	})
	if !strings.Contains(stderr, "--force") {
		t.Fatalf("expected overwrite warning, got %q", stderr)
	}
	if got := strings.TrimSpace(readFile(t, target)); got != "old artifact" {
		t.Fatalf("expected artifact to remain unchanged, got %q", got)
	}

	out.Reset()
	code := runConvenienceWithDeps(
		context.Background(),
		logging.NewRecorder(),
		"status",
		[]string{"--project", "myproject", "--format", "markdown", "--out-file", target, "--force"},
		out,
		fixedNow,
		func(_ context.Context, _ logging.Logger) (core.Service, runtime.CleanupFunc, error) {
			return svc, func() {}, nil
		},
		RunWithLogger,
	)
	if code != 0 {
		t.Fatalf("expected exit code 0 with force, got %d", code)
	}
	if out.Len() != 0 {
		t.Fatalf("expected no stdout when writing file, got %q", out.String())
	}
	if got := strings.TrimSpace(readFile(t, target)); got != "# fresh artifact" {
		t.Fatalf("unexpected written artifact: %q", got)
	}
}

func TestRunConvenienceWithDeps_HistoryHelpShowsWorkFilters(t *testing.T) {
	output := captureStderr(t, func() {
		code := runConvenienceWithDeps(
			context.Background(),
			logging.NewRecorder(),
			"history",
			[]string{"--help"},
			&bytes.Buffer{},
			fixedNow,
			nil,
			nil,
		)
		if code != 0 {
			t.Fatalf("expected exit code 0, got %d", code)
		}
	})

	if !strings.Contains(output, "awm history [--project <id>] [--entity <all|work|receipt|run>] [--query <text>|--query-file <path>] [--scope <current|deferred|completed|all>] [--kind <kind>] [--limit <n>] [--unbounded[=true|false]] [--format <json|markdown>] [--out-file <path>] [--force[=true|false]]") {
		t.Fatalf("unexpected help output: %q", output)
	}
	for _, required := range []string{"-entity string", "-scope string", "-kind string"} {
		if !strings.Contains(output, required) {
			t.Fatalf("history help should include %q: %q", required, output)
		}
	}
}

func TestRunConvenienceWithDeps_StatusHelpUsesCanonicalUsage(t *testing.T) {
	output := captureStderr(t, func() {
		code := runConvenienceWithDeps(
			context.Background(),
			logging.NewRecorder(),
			"status",
			[]string{"--help"},
			&bytes.Buffer{},
			fixedNow,
			nil,
			nil,
		)
		if code != 0 {
			t.Fatalf("expected exit code 0, got %d", code)
		}
	})

	if !strings.Contains(output, "Usage:\n  awm status [--project <id>] [--project-root <path>] [--rules-file <path>] [--tags-file <path>] [--tests-file <path>] [--workflows-file <path>] [--task-text <text>|--task-file <path>] [--phase <plan|execute|review>] [--format <json|markdown>] [--out-file <path>] [--force[=true|false]]") {
		t.Fatalf("unexpected help output: %q", output)
	}
	if strings.Contains(output, "Usage:\n  awm doctor") {
		t.Fatalf("status help should not advertise doctor usage: %q", output)
	}
	if !strings.Contains(output, "awm status --task-text \"add review gate\" --phase execute") {
		t.Fatalf("status help should include a canonical example: %q", output)
	}
}

func fixedNow() time.Time {
	return time.Date(2026, 3, 5, 12, 0, 0, 0, time.UTC)
}

func captureStderr(t *testing.T, fn func()) string {
	t.Helper()

	original := os.Stderr
	r, w, err := os.Pipe()
	if err != nil {
		t.Fatalf("pipe stderr: %v", err)
	}
	os.Stderr = w
	defer func() {
		os.Stderr = original
	}()

	fn()

	if err := w.Close(); err != nil {
		t.Fatalf("close stderr writer: %v", err)
	}
	output, err := io.ReadAll(r)
	if err != nil {
		t.Fatalf("read captured stderr: %v", err)
	}
	if err := r.Close(); err != nil {
		t.Fatalf("close stderr reader: %v", err)
	}
	return string(output)
}

func readFile(t *testing.T, path string) string {
	t.Helper()
	blob, err := os.ReadFile(path)
	if err != nil {
		t.Fatalf("read %s: %v", path, err)
	}
	return string(blob)
}

type convenienceFakeService struct {
	getContextCalls []v1.ContextPayload
	exportCalls     []v1.ExportPayload
	exportResult    v1.ExportResult
	exportErr       *core.APIError
}

func (f *convenienceFakeService) Context(_ context.Context, payload v1.ContextPayload) (v1.ContextResult, *core.APIError) {
	f.getContextCalls = append(f.getContextCalls, payload)
	return v1.ContextResult{Status: "ok"}, nil
}

func (f *convenienceFakeService) Fetch(_ context.Context, _ v1.FetchPayload) (v1.FetchResult, *core.APIError) {
	return v1.FetchResult{}, nil
}

func (f *convenienceFakeService) Export(_ context.Context, payload v1.ExportPayload) (v1.ExportResult, *core.APIError) {
	f.exportCalls = append(f.exportCalls, payload)
	if f.exportErr != nil {
		return v1.ExportResult{}, f.exportErr
	}
	if f.exportResult == (v1.ExportResult{}) {
		return v1.ExportResult{
			Format:  v1.ExportFormatMarkdown,
			Content: "# export",
			Document: &v1.ExportDocument{
				Kind: v1.ExportDocumentKindFetchBundle,
			},
		}, nil
	}
	return f.exportResult, nil
}

func (f *convenienceFakeService) Review(_ context.Context, _ v1.ReviewPayload) (v1.ReviewResult, *core.APIError) {
	return v1.ReviewResult{}, nil
}

func (f *convenienceFakeService) Work(_ context.Context, _ v1.WorkPayload) (v1.WorkResult, *core.APIError) {
	return v1.WorkResult{}, nil
}

func (f *convenienceFakeService) HistorySearch(_ context.Context, _ v1.HistorySearchPayload) (v1.HistorySearchResult, *core.APIError) {
	return v1.HistorySearchResult{}, nil
}

func (f *convenienceFakeService) Done(_ context.Context, _ v1.DonePayload) (v1.DoneResult, *core.APIError) {
	return v1.DoneResult{}, nil
}

func (f *convenienceFakeService) Sync(_ context.Context, _ v1.SyncPayload) (v1.SyncResult, *core.APIError) {
	return v1.SyncResult{}, nil
}

func (f *convenienceFakeService) Health(_ context.Context, _ v1.HealthPayload) (v1.HealthResult, *core.APIError) {
	return v1.HealthResult{}, nil
}

func (f *convenienceFakeService) Status(_ context.Context, _ v1.StatusPayload) (v1.StatusResult, *core.APIError) {
	return v1.StatusResult{}, nil
}

func (f *convenienceFakeService) Verify(_ context.Context, _ v1.VerifyPayload) (v1.VerifyResult, *core.APIError) {
	return v1.VerifyResult{}, nil
}

func (f *convenienceFakeService) Init(_ context.Context, _ v1.InitPayload) (v1.InitResult, *core.APIError) {
	return v1.InitResult{}, nil
}
