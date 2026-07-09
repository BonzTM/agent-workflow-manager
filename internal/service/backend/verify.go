package backend

import (
	"bytes"
	"context"
	"crypto/rand"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"maps"
	"os"
	"os/exec"
	"path"
	"path/filepath"
	"regexp"
	"slices"
	"sort"
	"strings"
	"time"

	"gopkg.in/yaml.v3"

	bootstrapkit "github.com/bonztm/agent-workflow-manager/internal/bootstrap"
	"github.com/bonztm/agent-workflow-manager/internal/contracts/v1"
	"github.com/bonztm/agent-workflow-manager/internal/core"
	"github.com/bonztm/agent-workflow-manager/internal/workspace"
)

const (
	verifyTestsVersionV1           = "awm.tests.v1"
	verifyTestsPrimarySourcePath   = ".awm/awm-tests.yaml"
	verifyTestsSecondarySourcePath = "awm-tests.yaml"
	maxVerifyOutputExcerptChars    = 1600
	maxVerifyWorkEvidenceEntries   = 128
	maxVerifyDefinitions           = 512
	maxVerifyArgs                  = 256
	maxVerifyTimeoutSec            = 86400
)

var (
	verifyTestIDPattern   = regexp.MustCompile(`^[a-z0-9][a-z0-9._-]{0,127}$`)
	verifyEnvKeyPattern   = regexp.MustCompile(`^[A-Za-z_][A-Za-z0-9_]{0,127}$`)
	runtimeCommandEnvKeys = []string{
		"AWM_PG_DSN",
		"AWM_PROJECT_ID",
		"AWM_PROJECT_ROOT",
		"AWM_SQLITE_PATH",
		unboundedEnvVar,
		"AWM_LOG_LEVEL",
		"AWM_LOG_SINK",
	}
)

type verifyRunnerFunc func(ctx context.Context, projectRoot string, def verifyTestDefinition, extraEnv map[string]string) verifyCommandRun

type verifyTestsSource struct {
	SourcePath   string
	AbsolutePath string
	Exists       bool
}

type verifyTestsDocumentV1 struct {
	Version  string                 `yaml:"version"`
	Defaults verifyTestsDefaultsV1  `yaml:"defaults"`
	Tests    []verifyTestDocumentV1 `yaml:"tests"`
}

type verifyTestsDefaultsV1 struct {
	CWD        string `yaml:"cwd"`
	TimeoutSec int    `yaml:"timeout_sec"`
}

type verifyTestDocumentV1 struct {
	ID       string                   `yaml:"id"`
	Summary  string                   `yaml:"summary"`
	Command  verifyTestCommandV1      `yaml:"command"`
	Select   verifyTestSelectionV1    `yaml:"select"`
	Expected verifyTestExpectedExitV1 `yaml:"expected"`
}

type verifyTestCommandV1 struct {
	Argv       []string          `yaml:"argv"`
	CWD        string            `yaml:"cwd"`
	TimeoutSec int               `yaml:"timeout_sec"`
	Env        map[string]string `yaml:"env"`
}

type verifyTestSelectionV1 struct {
	Phases          []string `yaml:"phases"`
	TagsAny         []string `yaml:"tags_any"`
	ChangedPathsAny []string `yaml:"changed_paths_any"`
	AlwaysRun       bool     `yaml:"always_run"`
}

type verifyTestExpectedExitV1 struct {
	ExitCode *int `yaml:"exit_code"`
}

type verifyTestDefinition struct {
	ID               string
	Summary          string
	Argv             []string
	CWD              string
	TimeoutSec       int
	Env              map[string]string
	Phases           []v1.Phase
	TagsAny          []string
	ChangedPathsAny  []string
	AlwaysRun        bool
	ExpectedExitCode int
	DefinitionHash   string
}

type verifySelectionContext struct {
	ReceiptID    string
	PlanKey      string
	Phase        v1.Phase
	Tags         []string
	FilesChanged []string
}

type verifySelectedTest struct {
	Definition       verifyTestDefinition
	SelectionReasons []string
}

type verifyCommandRun struct {
	ExitCode   *int
	Stdout     string
	Stderr     string
	StartedAt  time.Time
	FinishedAt time.Time
	TimedOut   bool
	Err        error
}

// Verify selects the verification tests that match the payload's phase, tags,
// and changed files, executes them (unless dry-run or explain-only), persists
// the batch results, and updates the linked work items with the outcome.
func (s *Service) Verify(ctx context.Context, payload v1.VerifyPayload) (v1.VerifyResult, *core.APIError) {
	if s == nil || s.repo == nil {
		return v1.VerifyResult{}, backendError(v1.ErrCodeInternalError, "service repository is not configured", nil)
	}

	if s.verifyRepo == nil {
		return v1.VerifyResult{}, backendError(v1.ErrCodeInternalError, "verification storage is not configured", nil)
	}

	projectID := strings.TrimSpace(payload.ProjectID)
	projectRoot := s.defaultProjectRoot()
	definitions, source, err := s.loadVerifyDefinitions(projectRoot, payload.TestsFile, payload.TagsFile)
	if err != nil {
		return v1.VerifyResult{}, verifyDefinitionsAPIError(source.SourcePath, err)
	}

	selectionContext, apiErr := s.resolveVerifySelectionContext(ctx, payload)
	if apiErr != nil {
		return v1.VerifyResult{}, apiErr
	}

	selected, apiErr := selectVerifyDefinitions(definitions, payload, selectionContext)
	if apiErr != nil {
		return v1.VerifyResult{}, apiErr
	}

	result := v1.VerifyResult{
		SelectedTestIDs: selectedVerifyTestIDs(selected),
		Selected:        selectedVerifyResults(selected),
		Passed:          false,
	}
	if payload.DryRun {
		result.Status = v1.VerifyStatusDryRun
		return result, nil
	}
	if len(selected) == 0 {
		result.Status = v1.VerifyStatusNoTestsSelected
		return result, nil
	}

	batchRunID, err := newVerifyBatchRunID()
	if err != nil {
		return v1.VerifyResult{}, verifyInternalError("new_batch_run_id", err, "")
	}

	runner := s.runVerifyCommand
	if runner == nil {
		runner = runVerifyCommand
	}
	commandEnv := verifyCommandEnvironment(selectionContext)

	executedAt := time.Now().UTC()
	results := make([]v1.VerifyTestResult, 0, len(selected))
	records := make([]core.VerificationTestRun, 0, len(selected))
	allPassed := true

	for _, selectedTest := range selected {
		run := runner(ctx, projectRoot, selectedTest.Definition, commandEnv)
		status := classifyVerifyCommandRun(run, selectedTest.Definition.ExpectedExitCode)
		if status != v1.VerifyTestStatusPassed {
			allPassed = false
		}

		publicResult := v1.VerifyTestResult{
			TestID:         selectedTest.Definition.ID,
			Status:         status,
			DefinitionHash: selectedTest.Definition.DefinitionHash,
			DurationMS:     verifyRunDurationMS(run),
			StdoutExcerpt:  excerptVerifyOutput(run.Stdout),
			StderrExcerpt:  excerptVerifyOutput(run.Stderr),
		}
		if run.ExitCode != nil {
			exitCode := *run.ExitCode
			publicResult.ExitCode = &exitCode
		}
		results = append(results, publicResult)

		record := core.VerificationTestRun{
			BatchRunID:       batchRunID,
			ProjectID:        projectID,
			TestID:           selectedTest.Definition.ID,
			DefinitionHash:   selectedTest.Definition.DefinitionHash,
			Summary:          selectedTest.Definition.Summary,
			CommandArgv:      append([]string(nil), selectedTest.Definition.Argv...),
			CommandCWD:       selectedTest.Definition.CWD,
			TimeoutSec:       selectedTest.Definition.TimeoutSec,
			ExpectedExitCode: selectedTest.Definition.ExpectedExitCode,
			SelectionReasons: append([]string(nil), selectedTest.SelectionReasons...),
			Status:           string(status),
			DurationMS:       publicResult.DurationMS,
			StdoutExcerpt:    publicResult.StdoutExcerpt,
			StderrExcerpt:    publicResult.StderrExcerpt,
			StartedAt:        run.StartedAt.UTC(),
			FinishedAt:       run.FinishedAt.UTC(),
		}
		if run.ExitCode != nil {
			exitCode := *run.ExitCode
			record.ExitCode = &exitCode
		}
		records = append(records, record)
	}

	result.BatchRunID = batchRunID
	result.Results = results
	result.Status = v1.VerifyStatusPassed
	if !allPassed {
		result.Status = v1.VerifyStatusFailed
	}
	result.Passed = allPassed

	batch := core.VerificationBatch{
		BatchRunID:      batchRunID,
		ProjectID:       projectID,
		ReceiptID:       selectionContext.ReceiptID,
		PlanKey:         selectionContext.PlanKey,
		Phase:           string(selectionContext.Phase),
		TestsSourcePath: source.SourcePath,
		Status:          string(result.Status),
		Passed:          allPassed,
		SelectedTestIDs: append([]string(nil), result.SelectedTestIDs...),
		Results:         records,
		CreatedAt:       executedAt,
	}
	if err := s.verifyRepo.SaveVerificationBatch(ctx, batch); err != nil {
		return v1.VerifyResult{}, verifyInternalError("save_verification_batch", err, batchRunID)
	}

	if selectionContext.ReceiptID != "" || selectionContext.PlanKey != "" {
		if apiErr := s.updateVerifyWork(ctx, projectID, selectionContext, batchRunID, results, allPassed); apiErr != nil {
			return v1.VerifyResult{}, apiErr
		}
	}

	return result, nil
}

func (s *Service) loadVerifyDefinitions(projectRoot, testsFile, tagsFile string) ([]verifyTestDefinition, verifyTestsSource, error) {
	tagNormalizer, err := s.loadCanonicalTagNormalizer(projectRoot, tagsFile)
	if err != nil {
		return nil, verifyTestsSource{}, fmt.Errorf("load canonical tags: %w", err)
	}

	source, err := discoverVerifyTestsSource(projectRoot, testsFile)
	if err != nil {
		return nil, verifyTestsSource{}, err
	}
	if !source.Exists {
		return nil, source, os.ErrNotExist
	}

	blob, err := os.ReadFile(source.AbsolutePath)
	if err != nil {
		return nil, source, fmt.Errorf("read verification definitions %s: %w", source.SourcePath, err)
	}

	decoder := yaml.NewDecoder(strings.NewReader(string(blob)))
	decoder.KnownFields(true)

	doc := verifyTestsDocumentV1{}
	if dErr := decoder.Decode(&doc); dErr != nil {
		return nil, source, fmt.Errorf("parse verification definitions %s: %w", source.SourcePath, dErr)
	}
	if strings.TrimSpace(doc.Version) != verifyTestsVersionV1 {
		return nil, source, fmt.Errorf("verification definitions %s have unsupported version %q", source.SourcePath, strings.TrimSpace(doc.Version))
	}
	if len(doc.Tests) > maxVerifyDefinitions {
		return nil, source, fmt.Errorf("verification definitions %s may include at most %d tests", source.SourcePath, maxVerifyDefinitions)
	}

	defaultCWD, err := normalizeVerifyWorkingDir(doc.Defaults.CWD)
	if err != nil {
		return nil, source, fmt.Errorf("verification definitions %s defaults.cwd %w", source.SourcePath, err)
	}
	defaultTimeout, err := normalizeVerifyTimeout(doc.Defaults.TimeoutSec, true)
	if err != nil {
		return nil, source, fmt.Errorf("verification definitions %s defaults.timeout_sec %w", source.SourcePath, err)
	}

	seenIDs := make(map[string]struct{}, len(doc.Tests))
	definitions := make([]verifyTestDefinition, 0, len(doc.Tests))
	for i, raw := range doc.Tests {
		definition, err := normalizeVerifyDefinition(source.SourcePath, i, raw, defaultCWD, defaultTimeout, tagNormalizer)
		if err != nil {
			return nil, source, err
		}
		if _, exists := seenIDs[definition.ID]; exists {
			return nil, source, fmt.Errorf("verification definitions %s tests[%d].id duplicates %q", source.SourcePath, i, definition.ID)
		}
		seenIDs[definition.ID] = struct{}{}
		definitions = append(definitions, definition)
	}

	sort.Slice(definitions, func(i, j int) bool {
		return definitions[i].ID < definitions[j].ID
	})
	return definitions, source, nil
}

func discoverVerifyTestsSource(projectRoot, testsFile string) (verifyTestsSource, error) {
	root := bootstrapkit.NormalizeProjectRoot(projectRoot)
	if trimmedTestsFile := strings.TrimSpace(testsFile); trimmedTestsFile != "" {
		return statVerifyTestsSource(root, trimmedTestsFile)
	}

	primary, err := statVerifyTestsSource(root, verifyTestsPrimarySourcePath)
	if err != nil {
		return verifyTestsSource{}, err
	}
	if primary.Exists {
		return primary, nil
	}

	secondary, err := statVerifyTestsSource(root, verifyTestsSecondarySourcePath)
	if err != nil {
		return verifyTestsSource{}, err
	}
	if secondary.Exists {
		return secondary, nil
	}
	return primary, nil
}

func statVerifyTestsSource(projectRoot, sourcePath string) (verifyTestsSource, error) {
	normalized, absolutePath, err := resolveProjectSourcePath(projectRoot, sourcePath)
	if err != nil {
		return verifyTestsSource{}, fmt.Errorf("verification definitions source path is required: %w", err)
	}
	stat, err := os.Stat(absolutePath)
	var exists bool
	switch {
	case err == nil:
		exists = !stat.IsDir()
	case errors.Is(err, os.ErrNotExist):
		exists = false
	default:
		return verifyTestsSource{}, fmt.Errorf("stat verification definitions %s: %w", normalized, err)
	}
	return verifyTestsSource{
		SourcePath:   normalized,
		AbsolutePath: absolutePath,
		Exists:       exists,
	}, nil
}

func normalizeVerifyDefinition(sourcePath string, index int, raw verifyTestDocumentV1, defaultCWD string, defaultTimeout int, tagNormalizer canonicalTagNormalizer) (verifyTestDefinition, error) {
	prefix := fmt.Sprintf("verification definitions %s tests[%d]", sourcePath, index)

	id := strings.ToLower(strings.TrimSpace(raw.ID))
	if !verifyTestIDPattern.MatchString(id) {
		return verifyTestDefinition{}, fmt.Errorf("%s.id format is invalid", prefix)
	}

	summary := strings.TrimSpace(raw.Summary)
	if summary == "" || len(summary) > 600 {
		return verifyTestDefinition{}, fmt.Errorf("%s.summary must be 1..600 chars", prefix)
	}

	argv, err := normalizeVerifyArgv(raw.Command.Argv)
	if err != nil {
		return verifyTestDefinition{}, fmt.Errorf("%s.command.argv %w", prefix, err)
	}
	env, err := normalizeVerifyEnv(raw.Command.Env)
	if err != nil {
		return verifyTestDefinition{}, fmt.Errorf("%s.command.env %w", prefix, err)
	}

	cwdValue := firstNonEmpty(raw.Command.CWD, defaultCWD)
	cwd, err := normalizeVerifyWorkingDir(cwdValue)
	if err != nil {
		return verifyTestDefinition{}, fmt.Errorf("%s.command.cwd %w", prefix, err)
	}

	timeout := defaultTimeout
	if raw.Command.TimeoutSec != 0 {
		timeout, err = normalizeVerifyTimeout(raw.Command.TimeoutSec, false)
		if err != nil {
			return verifyTestDefinition{}, fmt.Errorf("%s.command.timeout_sec %w", prefix, err)
		}
	}

	phases, err := normalizeVerifyPhases(raw.Select.Phases)
	if err != nil {
		return verifyTestDefinition{}, fmt.Errorf("%s.select.phases %w", prefix, err)
	}

	tagsAny := tagNormalizer.normalizeTags(raw.Select.TagsAny)
	changedPathsAny, err := normalizeVerifyGlobs(raw.Select.ChangedPathsAny)
	if err != nil {
		return verifyTestDefinition{}, fmt.Errorf("%s.select.changed_paths_any %w", prefix, err)
	}
	if raw.Select.AlwaysRun && (len(phases) > 0 || len(tagsAny) > 0 || len(changedPathsAny) > 0) {
		return verifyTestDefinition{}, fmt.Errorf("%s.select.always_run must not be combined with other selector fields", prefix)
	}

	expectedExitCode := 0
	if raw.Expected.ExitCode != nil {
		expectedExitCode = *raw.Expected.ExitCode
	}
	if expectedExitCode < 0 || expectedExitCode > 255 {
		return verifyTestDefinition{}, fmt.Errorf("%s.expected.exit_code must be between 0 and 255", prefix)
	}

	definition := verifyTestDefinition{
		ID:               id,
		Summary:          summary,
		Argv:             argv,
		CWD:              cwd,
		TimeoutSec:       timeout,
		Env:              env,
		Phases:           phases,
		TagsAny:          tagsAny,
		ChangedPathsAny:  changedPathsAny,
		AlwaysRun:        raw.Select.AlwaysRun,
		ExpectedExitCode: expectedExitCode,
	}
	definition.DefinitionHash = verifyDefinitionHash(definition)
	return definition, nil
}

func (s *Service) resolveVerifySelectionContext(ctx context.Context, payload v1.VerifyPayload) (verifySelectionContext, *core.APIError) {
	selection := verifySelectionContext{
		ReceiptID:    strings.TrimSpace(payload.ReceiptID),
		PlanKey:      strings.TrimSpace(payload.PlanKey),
		Phase:        payload.Phase,
		FilesChanged: normalizeCompletionPaths(payload.FilesChanged),
	}

	if selection.ReceiptID == "" && selection.PlanKey != "" {
		derivedReceiptID, ok := parsePlanFetchKey(selection.PlanKey)
		if !ok {
			return verifySelectionContext{}, backendError(v1.ErrCodeInvalidInput, "plan_key must use format plan:<receipt_id>", map[string]any{"plan_key": selection.PlanKey})
		}
		selection.ReceiptID = derivedReceiptID
	}

	if selection.ReceiptID == "" {
		return selection, nil
	}

	scope, err := s.repo.FetchReceiptScope(ctx, core.ReceiptScopeQuery{
		ProjectID: strings.TrimSpace(payload.ProjectID),
		ReceiptID: selection.ReceiptID,
	})
	if err != nil {
		if errors.Is(err, core.ErrReceiptScopeNotFound) {
			return verifySelectionContext{}, backendError(v1.ErrCodeNotFound, "receipt scope was not found", map[string]any{
				"project_id": strings.TrimSpace(payload.ProjectID),
				"receipt_id": selection.ReceiptID,
			})
		}
		return verifySelectionContext{}, verifyInternalError("fetch_receipt_scope", err, "")
	}

	if selection.Phase == "" {
		selection.Phase = v1.Phase(strings.TrimSpace(scope.Phase))
	}
	detectedFiles, reliableDetection, apiErr := s.detectReceiptChangedPaths(ctx, s.defaultProjectRoot(), scope)
	if apiErr != nil {
		return verifySelectionContext{}, apiErr
	}
	selection.FilesChanged = resolveDetectedFilesChanged(reliableDetection, detectedFiles, payload.FilesChanged)
	selection.Tags = normalizeValues(scope.ResolvedTags)
	return selection, nil
}

func selectVerifyDefinitions(definitions []verifyTestDefinition, payload v1.VerifyPayload, selection verifySelectionContext) ([]verifySelectedTest, *core.APIError) {
	if len(payload.TestIDs) > 0 {
		byID := make(map[string]verifyTestDefinition, len(definitions))
		for _, definition := range definitions {
			byID[definition.ID] = definition
		}
		selected := make([]verifySelectedTest, 0, len(payload.TestIDs))
		for _, rawID := range payload.TestIDs {
			testID := strings.ToLower(strings.TrimSpace(rawID))
			definition, ok := byID[testID]
			if !ok {
				return nil, backendError(v1.ErrCodeInvalidInput, "unknown test_id", map[string]any{"test_id": testID})
			}
			selected = append(selected, verifySelectedTest{
				Definition:       definition,
				SelectionReasons: []string{"explicit test_id=" + testID},
			})
		}
		return selected, nil
	}

	selected := make([]verifySelectedTest, 0, len(definitions))
	for _, definition := range definitions {
		reasons, matched := matchVerifyDefinition(definition, selection)
		if !matched {
			continue
		}
		selected = append(selected, verifySelectedTest{
			Definition:       definition,
			SelectionReasons: reasons,
		})
	}
	return selected, nil
}

func matchVerifyDefinition(definition verifyTestDefinition, selection verifySelectionContext) ([]string, bool) {
	if definition.AlwaysRun {
		return []string{"always_run=true"}, true
	}

	selectorCount := len(definition.Phases) + len(definition.TagsAny) + len(definition.ChangedPathsAny)
	if selectorCount == 0 {
		return nil, false
	}

	reasons := make([]string, 0, 4)
	if len(definition.Phases) > 0 {
		if selection.Phase == "" || !containsVerifyPhase(definition.Phases, selection.Phase) {
			return nil, false
		}
		reasons = append(reasons, "phase="+string(selection.Phase))
	}
	if len(definition.TagsAny) > 0 {
		matchedTag, ok := firstVerifyStringIntersection(definition.TagsAny, selection.Tags)
		if !ok {
			return nil, false
		}
		reasons = append(reasons, "tags_any matched "+matchedTag)
	}
	if len(definition.ChangedPathsAny) > 0 {
		matchedPath, ok := matchVerifyChangedPaths(definition.ChangedPathsAny, selection.FilesChanged)
		if !ok {
			return nil, false
		}
		reasons = append(reasons, "changed_paths_any matched "+matchedPath)
	}
	return reasons, true
}

func containsVerifyPhase(values []v1.Phase, candidate v1.Phase) bool {
	return slices.Contains(values, candidate)
}

func firstVerifyStringIntersection(values, candidates []string) (string, bool) {
	if len(values) == 0 || len(candidates) == 0 {
		return "", false
	}
	candidateSet := make(map[string]struct{}, len(candidates))
	for _, candidate := range candidates {
		candidateSet[strings.TrimSpace(candidate)] = struct{}{}
	}
	for _, value := range values {
		if _, ok := candidateSet[strings.TrimSpace(value)]; ok {
			return strings.TrimSpace(value), true
		}
	}
	return "", false
}

func matchVerifyChangedPaths(patterns, paths []string) (string, bool) {
	for _, pattern := range patterns {
		for _, candidatePath := range paths {
			matched, err := matchVerifyGlob(pattern, candidatePath)
			if err != nil {
				continue
			}
			if matched {
				return candidatePath, true
			}
		}
	}
	return "", false
}

func matchVerifyGlob(pattern, candidate string) (bool, error) {
	regex, err := verifyGlobToRegexp(pattern)
	if err != nil {
		return false, err
	}
	return regex.MatchString(candidate), nil
}

func verifyGlobToRegexp(pattern string) (*regexp.Regexp, error) {
	var b strings.Builder
	b.WriteString("^")
	for i := 0; i < len(pattern); i++ {
		ch := pattern[i]
		switch ch {
		case '*':
			if i+1 < len(pattern) && pattern[i+1] == '*' {
				b.WriteString(".*")
				i++
				continue
			}
			b.WriteString(`[^/]*`)
		case '?':
			b.WriteString(`[^/]`)
		case '.', '+', '(', ')', '|', '^', '$', '{', '}', '[', ']', '\\':
			b.WriteByte('\\')
			b.WriteByte(ch)
		default:
			b.WriteByte(ch)
		}
	}
	b.WriteString("$")
	return regexp.Compile(b.String())
}

func verifyCommandEnvironment(selection verifySelectionContext) map[string]string {
	receiptID := strings.TrimSpace(selection.ReceiptID)
	planKey := strings.TrimSpace(selection.PlanKey)
	if receiptID == "" && planKey != "" {
		if derivedReceiptID, ok := parsePlanFetchKey(planKey); ok {
			receiptID = derivedReceiptID
		}
	}
	if planKey == "" && receiptID != "" {
		planKey = "plan:" + receiptID
	}

	extraEnv := map[string]string{
		"AWM_VERIFY_TAGS_JSON":          verifyJSONListEnv(selection.Tags),
		"AWM_VERIFY_FILES_CHANGED_JSON": verifyJSONListEnv(selection.FilesChanged),
	}
	if receiptID != "" {
		extraEnv["AWM_RECEIPT_ID"] = receiptID
	}
	if planKey != "" {
		extraEnv["AWM_PLAN_KEY"] = planKey
	}
	if selection.Phase != "" {
		extraEnv["AWM_VERIFY_PHASE"] = string(selection.Phase)
	}
	return extraEnv
}

func verifyJSONListEnv(values []string) string {
	normalized := normalizeValues(values)
	if normalized == nil {
		normalized = []string{}
	}
	encoded, err := json.Marshal(normalized)
	if err != nil {
		return "[]"
	}
	return string(encoded)
}

func runVerifyCommand(ctx context.Context, projectRoot string, def verifyTestDefinition, extraEnv map[string]string) verifyCommandRun {
	return runConfiguredCommand(ctx, projectRoot, def.Argv, def.CWD, def.TimeoutSec, nil, def.Env, extraEnv)
}

func classifyVerifyCommandRun(run verifyCommandRun, expectedExitCode int) v1.VerifyTestStatus {
	switch {
	case run.TimedOut:
		return v1.VerifyTestStatusTimedOut
	case run.Err == nil:
		if run.ExitCode != nil && *run.ExitCode != expectedExitCode {
			return v1.VerifyTestStatusFailed
		}
		return v1.VerifyTestStatusPassed
	case run.ExitCode != nil:
		if *run.ExitCode == expectedExitCode {
			return v1.VerifyTestStatusPassed
		}
		return v1.VerifyTestStatusFailed
	default:
		return v1.VerifyTestStatusErrored
	}
}

func verifyRunDurationMS(run verifyCommandRun) int {
	if run.FinishedAt.Before(run.StartedAt) {
		return 0
	}
	return int(run.FinishedAt.Sub(run.StartedAt).Milliseconds())
}

func selectedVerifyTestIDs(selected []verifySelectedTest) []string {
	out := make([]string, 0, len(selected))
	for _, item := range selected {
		out = append(out, item.Definition.ID)
	}
	return out
}

func selectedVerifyResults(selected []verifySelectedTest) []v1.VerifySelection {
	out := make([]v1.VerifySelection, 0, len(selected))
	for _, item := range selected {
		out = append(out, v1.VerifySelection{
			TestID:           item.Definition.ID,
			Summary:          item.Definition.Summary,
			SelectionReasons: append([]string(nil), item.SelectionReasons...),
		})
	}
	return out
}

func excerptVerifyOutput(raw string) string {
	trimmed := strings.TrimSpace(raw)
	if trimmed == "" {
		return ""
	}
	if len(trimmed) <= maxVerifyOutputExcerptChars {
		return trimmed
	}
	if maxVerifyOutputExcerptChars <= len("...(truncated)") {
		return trimmed[:maxVerifyOutputExcerptChars]
	}
	return trimmed[:maxVerifyOutputExcerptChars-len("...(truncated)")] + "...(truncated)"
}

func (s *Service) updateVerifyWork(ctx context.Context, projectID string, selection verifySelectionContext, batchRunID string, results []v1.VerifyTestResult, passed bool) *core.APIError {
	status := v1.WorkItemStatusBlocked
	if passed {
		status = v1.WorkItemStatusComplete
	}

	evidence := []string{"verifyrun:" + batchRunID}
	for _, result := range results {
		if len(evidence) >= maxVerifyWorkEvidenceEntries {
			break
		}
		evidence = append(evidence, fmt.Sprintf("verifyrun:%s#%s", batchRunID, result.TestID))
	}

	_, apiErr := s.Work(ctx, v1.WorkPayload{
		ProjectID: projectID,
		PlanKey:   selection.PlanKey,
		ReceiptID: selection.ReceiptID,
		Tasks: []v1.WorkTaskPayload{{
			Key:      requiredVerifyTestsKey,
			Summary:  "Run project verification checks",
			Status:   status,
			Outcome:  summarizeVerifyResults(results, passed),
			Evidence: evidence,
		}},
	})
	if apiErr != nil {
		return backendError(apiErr.Code, "verification ran but failed to update verify:tests", map[string]any{
			"batch_run_id":    batchRunID,
			"wrapped_code":    apiErr.Code,
			"wrapped_message": apiErr.Message,
		})
	}
	return nil
}

func summarizeVerifyResults(results []v1.VerifyTestResult, passed bool) string {
	if len(results) == 0 {
		return ""
	}
	if passed {
		return fmt.Sprintf("%d verification tests passed", len(results))
	}

	failures := make([]string, 0, len(results))
	passedCount := 0
	for _, result := range results {
		if result.Status == v1.VerifyTestStatusPassed {
			passedCount++
			continue
		}
		failures = append(failures, fmt.Sprintf("%s (%s)", result.TestID, result.Status))
	}
	summary := fmt.Sprintf("%d/%d verification tests passed", passedCount, len(results))
	if len(failures) == 0 {
		return summary
	}
	return excerptVerifyOutput(summary + "; failed: " + strings.Join(failures, ", "))
}

func normalizeVerifyArgv(raw []string) ([]string, error) {
	if len(raw) == 0 {
		return nil, errors.New("must not be empty")
	}
	if len(raw) > maxVerifyArgs {
		return nil, fmt.Errorf("may include at most %d entries", maxVerifyArgs)
	}
	argv := make([]string, 0, len(raw))
	for i, arg := range raw {
		trimmed := strings.TrimSpace(arg)
		if trimmed == "" || len(trimmed) > 1024 {
			return nil, fmt.Errorf("[%d] must be 1..1024 chars", i)
		}
		argv = append(argv, trimmed)
	}
	return argv, nil
}

func normalizeVerifyEnv(raw map[string]string) (map[string]string, error) {
	if len(raw) == 0 {
		return nil, nil
	}
	if len(raw) > 64 {
		return nil, errors.New("may include at most 64 entries")
	}
	out := make(map[string]string, len(raw))
	for rawKey, rawValue := range raw {
		key := strings.TrimSpace(rawKey)
		if !verifyEnvKeyPattern.MatchString(key) {
			return nil, fmt.Errorf("key %q is invalid", rawKey)
		}
		if len(rawValue) > 4096 {
			return nil, fmt.Errorf("value for %q may not exceed 4096 chars", key)
		}
		out[key] = rawValue
	}
	return out, nil
}

func normalizeVerifyWorkingDir(raw string) (string, error) {
	trimmed := strings.TrimSpace(raw)
	if trimmed == "" {
		return ".", nil
	}
	if filepath.IsAbs(trimmed) {
		return "", errors.New("must be repository-relative")
	}
	normalized := path.Clean(strings.ReplaceAll(trimmed, "\\", "/"))
	if normalized == ".." || strings.HasPrefix(normalized, "../") {
		return "", errors.New("must be repository-relative")
	}
	if normalized == "." {
		return ".", nil
	}
	return normalized, nil
}

func normalizeVerifyTimeout(raw int, allowZero bool) (int, error) {
	if raw == 0 && allowZero {
		return defaultVerifyTimeoutSec, nil
	}
	if raw <= 0 || raw > maxVerifyTimeoutSec {
		return 0, fmt.Errorf("must be between 1 and %d", maxVerifyTimeoutSec)
	}
	return raw, nil
}

func normalizeVerifyPhases(raw []string) ([]v1.Phase, error) {
	if len(raw) == 0 {
		return nil, nil
	}
	seen := map[v1.Phase]struct{}{}
	out := make([]v1.Phase, 0, len(raw))
	for i, value := range raw {
		phase := v1.Phase(strings.ToLower(strings.TrimSpace(value)))
		switch phase {
		case v1.PhasePlan, v1.PhaseExecute, v1.PhaseReview:
		default:
			return nil, fmt.Errorf("[%d] must be plan|execute|review", i)
		}
		if _, ok := seen[phase]; ok {
			continue
		}
		seen[phase] = struct{}{}
		out = append(out, phase)
	}
	slices.Sort(out)
	return out, nil
}

func normalizeVerifyGlobs(raw []string) ([]string, error) {
	if len(raw) == 0 {
		return nil, nil
	}
	seen := map[string]struct{}{}
	out := make([]string, 0, len(raw))
	for i, value := range raw {
		trimmed := strings.TrimSpace(value)
		if trimmed == "" {
			return nil, fmt.Errorf("[%d] must not be empty", i)
		}
		if filepath.IsAbs(trimmed) {
			return nil, fmt.Errorf("[%d] must be repository-relative", i)
		}
		normalized := path.Clean(strings.ReplaceAll(trimmed, "\\", "/"))
		if normalized == ".." || strings.HasPrefix(normalized, "../") {
			return nil, fmt.Errorf("[%d] must be repository-relative", i)
		}
		if _, ok := seen[normalized]; ok {
			continue
		}
		seen[normalized] = struct{}{}
		out = append(out, normalized)
	}
	sort.Strings(out)
	return out, nil
}

func verifyDefinitionHash(definition verifyTestDefinition) string {
	type envEntry struct {
		Key   string `json:"key"`
		Value string `json:"value"`
	}
	env := make([]envEntry, 0, len(definition.Env))
	for _, pair := range verifyEnvPairs(definition.Env) {
		name, value, _ := strings.Cut(pair, "=")
		env = append(env, envEntry{Key: name, Value: value})
	}
	payload, mErr := json.Marshal(struct {
		ID               string     `json:"id"`
		Summary          string     `json:"summary"`
		Argv             []string   `json:"argv"`
		CWD              string     `json:"cwd"`
		TimeoutSec       int        `json:"timeout_sec"`
		Env              []envEntry `json:"env,omitempty"`
		Phases           []v1.Phase `json:"phases,omitempty"`
		TagsAny          []string   `json:"tags_any,omitempty"`
		ChangedPathsAny  []string   `json:"changed_paths_any,omitempty"`
		AlwaysRun        bool       `json:"always_run,omitempty"`
		ExpectedExitCode int        `json:"expected_exit_code"`
	}{
		ID:               definition.ID,
		Summary:          definition.Summary,
		Argv:             definition.Argv,
		CWD:              definition.CWD,
		TimeoutSec:       definition.TimeoutSec,
		Env:              env,
		Phases:           definition.Phases,
		TagsAny:          definition.TagsAny,
		ChangedPathsAny:  definition.ChangedPathsAny,
		AlwaysRun:        definition.AlwaysRun,
		ExpectedExitCode: definition.ExpectedExitCode,
	})
	if mErr != nil {
		// Marshaling this fixed shape of strings, ints, and bools cannot fail;
		// hash a distinct sentinel if it somehow does.
		payload = []byte("verify-definition-marshal-error: " + mErr.Error())
	}
	digest := sha256.Sum256(payload)
	return "sha256:" + hex.EncodeToString(digest[:])
}

func verifyEnvPairs(values map[string]string) []string {
	if len(values) == 0 {
		return nil
	}
	keys := make([]string, 0, len(values))
	for key := range values {
		keys = append(keys, key)
	}
	sort.Strings(keys)
	pairs := make([]string, 0, len(keys))
	for _, key := range keys {
		pairs = append(pairs, key+"="+values[key])
	}
	return pairs
}

func mergeCommandEnv(base, extra map[string]string) map[string]string {
	if len(base) == 0 && len(extra) == 0 {
		return nil
	}
	merged := make(map[string]string, len(base)+len(extra))
	maps.Copy(merged, base)
	maps.Copy(merged, extra)
	return merged
}

func runtimeCommandEnv(projectRoot string) map[string]string {
	startDir := strings.TrimSpace(projectRoot)
	if startDir != "" {
		startDir = filepath.Clean(startDir)
	}

	values := make(map[string]string, len(runtimeCommandEnvKeys))
	for _, key := range runtimeCommandEnvKeys {
		if value := workspace.LookupEnvValue(startDir, key, nil); value != "" {
			values[key] = value
		}
	}
	if len(values) == 0 {
		return nil
	}
	return values
}

func resolveConfiguredCommandArgv(projectRoot, cwd string, argv []string) []string {
	out := append([]string(nil), argv...)
	if len(out) == 0 {
		return out
	}
	commandPath := strings.TrimSpace(out[0])
	if commandPath == "" {
		return out
	}
	if filepath.IsAbs(commandPath) || strings.Contains(commandPath, "/") || strings.Contains(commandPath, string(filepath.Separator)) {
		baseDir := filepath.Clean(filepath.Join(projectRoot, filepath.FromSlash(cwd)))
		if filepath.IsAbs(commandPath) {
			out[0] = filepath.Clean(commandPath)
		} else {
			out[0] = filepath.Clean(filepath.Join(baseDir, filepath.FromSlash(commandPath)))
		}
	}
	return out
}

func runConfiguredCommand(ctx context.Context, projectRoot string, argv []string, cwd string, timeoutSec int, runtimeEnv, env, extraEnv map[string]string) verifyCommandRun {
	timeoutCtx, cancel := context.WithTimeout(ctx, time.Duration(timeoutSec)*time.Second)
	defer cancel()

	startedAt := time.Now().UTC()
	resolvedArgv := resolveConfiguredCommandArgv(projectRoot, cwd, argv)
	command := exec.CommandContext(timeoutCtx, resolvedArgv[0], resolvedArgv[1:]...) //nolint:gosec // G204: argv comes from the project's own verify/workflow definitions; executing it is this function's purpose
	command.Dir = filepath.Clean(filepath.Join(projectRoot, filepath.FromSlash(cwd)))
	commandEnv := mergeCommandEnv(runtimeEnv, env)
	commandEnv = mergeCommandEnv(commandEnv, extraEnv)
	command.Env = append(os.Environ(), verifyEnvPairs(commandEnv)...)

	var stdout bytes.Buffer
	var stderr bytes.Buffer
	command.Stdout = &stdout
	command.Stderr = &stderr

	err := command.Run()
	finishedAt := time.Now().UTC()

	var exitCode *int
	if command.ProcessState != nil {
		code := command.ProcessState.ExitCode()
		if code >= 0 {
			exitCode = &code
		}
	}
	if exitCode == nil {
		var exitErr *exec.ExitError
		if errors.As(err, &exitErr) && exitErr.ProcessState != nil {
			code := exitErr.ExitCode()
			if code >= 0 {
				exitCode = &code
			}
		}
	}

	return verifyCommandRun{
		ExitCode:   exitCode,
		Stdout:     stdout.String(),
		Stderr:     stderr.String(),
		StartedAt:  startedAt,
		FinishedAt: finishedAt,
		TimedOut:   errors.Is(timeoutCtx.Err(), context.DeadlineExceeded),
		Err:        err,
	}
}

func newVerifyBatchRunID() (string, error) {
	var bytes [8]byte
	if _, err := rand.Read(bytes[:]); err != nil {
		return "", fmt.Errorf("read random bytes: %w", err)
	}
	return fmt.Sprintf("verify-%d-%s", time.Now().UTC().UnixNano(), hex.EncodeToString(bytes[:])), nil
}

func verifyDefinitionsAPIError(sourcePath string, err error) *core.APIError {
	if errors.Is(err, os.ErrNotExist) {
		return backendError(v1.ErrCodeNotFound, "verification definitions file was not found", map[string]any{"tests_source_path": sourcePath})
	}
	return backendError(v1.ErrCodeInvalidInput, "verification definitions are invalid", map[string]any{
		"tests_source_path": sourcePath,
		"error":             err.Error(),
	})
}

func verifyInternalError(operation string, err error, batchRunID string) *core.APIError {
	details := map[string]any{
		"operation": operation,
		"error":     err.Error(),
	}
	if strings.TrimSpace(batchRunID) != "" {
		details["batch_run_id"] = batchRunID
	}
	return backendError(v1.ErrCodeInternalError, "failed to run verify", details)
}
