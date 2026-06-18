package backend

import (
	"errors"
	"fmt"
	"os"
	"sort"
	"strings"

	bootstrapkit "github.com/bonztm/agent-workflow-manager/internal/bootstrap"
	"gopkg.in/yaml.v3"

	"github.com/bonztm/agent-workflow-manager/internal/contracts/v1"
)

const (
	workflowDefinitionsVersionV1           = "awm.workflows.v1"
	workflowDefinitionsPrimarySourcePath   = ".awm/awm-workflows.yaml"
	workflowDefinitionsSecondarySourcePath = "awm-workflows.yaml"
	maxWorkflowRequiredTasks               = 128
)

type workflowDefinitionsSource struct {
	SourcePath   string
	AbsolutePath string
	Exists       bool
}

type workflowDefinitionsDocumentV1 struct {
	Version    string                       `yaml:"version"`
	Completion workflowCompletionDocumentV1 `yaml:"completion"`
}

type workflowCompletionDocumentV1 struct {
	RequiredTasks []workflowRequiredTaskDocumentV1 `yaml:"required_tasks"`
}

type workflowRequiredTaskDocumentV1 struct {
	Key                         string                 `yaml:"key"`
	Summary                     string                 `yaml:"summary"`
	MaxAttempts                 *int                   `yaml:"max_attempts"`
	RerunRequiresNewFingerprint *bool                  `yaml:"rerun_requires_new_fingerprint"`
	Select                      verifyTestSelectionV1  `yaml:"select"`
	Run                         *workflowRunDocumentV1 `yaml:"run"`
}

type workflowRunDocumentV1 struct {
	Argv       []string          `yaml:"argv"`
	CWD        string            `yaml:"cwd"`
	TimeoutSec int               `yaml:"timeout_sec"`
	Env        map[string]string `yaml:"env"`
}

type workflowRequiredTaskDefinition struct {
	Key                         string
	Summary                     string
	MaxAttempts                 int
	RerunRequiresNewFingerprint bool
	Phases                      []v1.Phase
	TagsAny                     []string
	ChangedPathsAny             []string
	AlwaysRun                   bool
	Run                         *workflowRunDefinition
}

type workflowRunDefinition struct {
	Argv       []string
	CWD        string
	TimeoutSec int
	Env        map[string]string
}

func (s *Service) loadWorkflowCompletionRequirements(projectRoot, workflowsFile, tagsFile string) ([]workflowRequiredTaskDefinition, workflowDefinitionsSource, error) {
	tagNormalizer, err := s.loadCanonicalTagNormalizer(projectRoot, tagsFile)
	if err != nil {
		return nil, workflowDefinitionsSource{}, fmt.Errorf("load canonical tags: %w", err)
	}

	source, err := discoverWorkflowDefinitionsSource(projectRoot, workflowsFile)
	if err != nil {
		return nil, workflowDefinitionsSource{}, err
	}
	if !source.Exists {
		return nil, source, nil
	}

	blob, err := os.ReadFile(source.AbsolutePath)
	if err != nil {
		return nil, source, fmt.Errorf("read workflow definitions %s: %w", source.SourcePath, err)
	}

	decoder := yaml.NewDecoder(strings.NewReader(string(blob)))
	decoder.KnownFields(true)

	doc := workflowDefinitionsDocumentV1{}
	if err := decoder.Decode(&doc); err != nil {
		return nil, source, fmt.Errorf("parse workflow definitions %s: %w", source.SourcePath, err)
	}
	if strings.TrimSpace(doc.Version) != workflowDefinitionsVersionV1 {
		return nil, source, fmt.Errorf("workflow definitions %s have unsupported version %q", source.SourcePath, strings.TrimSpace(doc.Version))
	}
	if len(doc.Completion.RequiredTasks) > maxWorkflowRequiredTasks {
		return nil, source, fmt.Errorf("workflow definitions %s may include at most %d completion.required_tasks entries", source.SourcePath, maxWorkflowRequiredTasks)
	}

	definitions := make([]workflowRequiredTaskDefinition, 0, len(doc.Completion.RequiredTasks))
	for i, raw := range doc.Completion.RequiredTasks {
		definition, err := normalizeWorkflowRequiredTask(source.SourcePath, i, raw, tagNormalizer)
		if err != nil {
			return nil, source, err
		}
		definitions = append(definitions, definition)
	}

	return definitions, source, nil
}

func discoverWorkflowDefinitionsSource(projectRoot, workflowsFile string) (workflowDefinitionsSource, error) {
	root := bootstrapkit.NormalizeProjectRoot(projectRoot)

	if trimmed := strings.TrimSpace(workflowsFile); trimmed != "" {
		return statWorkflowDefinitionsSource(root, trimmed)
	}

	primary, err := statWorkflowDefinitionsSource(root, workflowDefinitionsPrimarySourcePath)
	if err != nil {
		return workflowDefinitionsSource{}, err
	}
	if primary.Exists {
		return primary, nil
	}

	secondary, err := statWorkflowDefinitionsSource(root, workflowDefinitionsSecondarySourcePath)
	if err != nil {
		return workflowDefinitionsSource{}, err
	}
	if secondary.Exists {
		return secondary, nil
	}
	return primary, nil
}

func statWorkflowDefinitionsSource(projectRoot, sourcePath string) (workflowDefinitionsSource, error) {
	normalized, absolutePath, err := resolveProjectSourcePath(projectRoot, sourcePath)
	if err != nil {
		return workflowDefinitionsSource{}, fmt.Errorf("workflow definitions source path is required: %w", err)
	}
	stat, err := os.Stat(absolutePath)
	exists := false
	switch {
	case err == nil:
		exists = !stat.IsDir()
	case errors.Is(err, os.ErrNotExist):
		exists = false
	default:
		return workflowDefinitionsSource{}, fmt.Errorf("stat workflow definitions %s: %w", normalized, err)
	}
	return workflowDefinitionsSource{
		SourcePath:   normalized,
		AbsolutePath: absolutePath,
		Exists:       exists,
	}, nil
}

func normalizeWorkflowRequiredTask(sourcePath string, index int, raw workflowRequiredTaskDocumentV1, tagNormalizer canonicalTagNormalizer) (workflowRequiredTaskDefinition, error) {
	prefix := fmt.Sprintf("workflow definitions %s completion.required_tasks[%d]", sourcePath, index)

	key := strings.TrimSpace(raw.Key)
	if key == "" || len(key) > maxFetchKeyLength {
		return workflowRequiredTaskDefinition{}, fmt.Errorf("%s.key must be 1..%d chars", prefix, maxFetchKeyLength)
	}
	summary := strings.TrimSpace(raw.Summary)
	if summary != "" && len(summary) > 600 {
		return workflowRequiredTaskDefinition{}, fmt.Errorf("%s.summary must be 1..600 chars when provided", prefix)
	}
	if raw.MaxAttempts != nil && (*raw.MaxAttempts < 1 || *raw.MaxAttempts > 16) {
		return workflowRequiredTaskDefinition{}, fmt.Errorf("%s.max_attempts must be 1..16 when provided", prefix)
	}

	phases, err := normalizeVerifyPhases(raw.Select.Phases)
	if err != nil {
		return workflowRequiredTaskDefinition{}, fmt.Errorf("%s.select.phases %w", prefix, err)
	}
	tagsAny := tagNormalizer.normalizeTags(raw.Select.TagsAny)
	changedPathsAny, err := normalizeVerifyGlobs(raw.Select.ChangedPathsAny)
	if err != nil {
		return workflowRequiredTaskDefinition{}, fmt.Errorf("%s.select.changed_paths_any %w", prefix, err)
	}
	if raw.Select.AlwaysRun && (len(phases) > 0 || len(tagsAny) > 0 || len(changedPathsAny) > 0) {
		return workflowRequiredTaskDefinition{}, fmt.Errorf("%s.select.always_run must not be combined with other selector fields", prefix)
	}

	alwaysRun := raw.Select.AlwaysRun
	if !alwaysRun && len(phases) == 0 && len(tagsAny) == 0 && len(changedPathsAny) == 0 {
		alwaysRun = true
	}

	var run *workflowRunDefinition
	maxAttempts := 0
	rerunRequiresNewFingerprint := false
	if raw.Run != nil {
		argv, err := normalizeVerifyArgv(raw.Run.Argv)
		if err != nil {
			return workflowRequiredTaskDefinition{}, fmt.Errorf("%s.run.argv %w", prefix, err)
		}
		env, err := normalizeVerifyEnv(raw.Run.Env)
		if err != nil {
			return workflowRequiredTaskDefinition{}, fmt.Errorf("%s.run.env %w", prefix, err)
		}
		cwd, err := normalizeVerifyWorkingDir(raw.Run.CWD)
		if err != nil {
			return workflowRequiredTaskDefinition{}, fmt.Errorf("%s.run.cwd %w", prefix, err)
		}
		timeout, err := normalizeVerifyTimeout(raw.Run.TimeoutSec, true)
		if err != nil {
			return workflowRequiredTaskDefinition{}, fmt.Errorf("%s.run.timeout_sec %w", prefix, err)
		}
		run = &workflowRunDefinition{
			Argv:       argv,
			CWD:        cwd,
			TimeoutSec: timeout,
			Env:        env,
		}
		if raw.MaxAttempts != nil {
			maxAttempts = *raw.MaxAttempts
		}
		rerunRequiresNewFingerprint = true
		if raw.RerunRequiresNewFingerprint != nil {
			rerunRequiresNewFingerprint = *raw.RerunRequiresNewFingerprint
		}
	} else if raw.MaxAttempts != nil || raw.RerunRequiresNewFingerprint != nil {
		return workflowRequiredTaskDefinition{}, fmt.Errorf("%s.max_attempts and %s.rerun_requires_new_fingerprint require run", prefix, prefix)
	}

	return workflowRequiredTaskDefinition{
		Key:                         key,
		Summary:                     summary,
		MaxAttempts:                 maxAttempts,
		RerunRequiresNewFingerprint: rerunRequiresNewFingerprint,
		Phases:                      phases,
		TagsAny:                     tagsAny,
		ChangedPathsAny:             changedPathsAny,
		AlwaysRun:                   alwaysRun,
		Run:                         run,
	}, nil
}

func matchWorkflowRequiredTasks(definitions []workflowRequiredTaskDefinition, selection verifySelectionContext) []string {
	if len(definitions) == 0 {
		return nil
	}

	seen := make(map[string]struct{}, len(definitions))
	matches := make([]string, 0, len(definitions))
	for _, definition := range definitions {
		if !matchWorkflowRequiredTask(definition, selection) {
			continue
		}
		if _, ok := seen[definition.Key]; ok {
			continue
		}
		seen[definition.Key] = struct{}{}
		matches = append(matches, definition.Key)
	}
	return matches
}

func matchWorkflowRequiredTaskDefinitions(definitions []workflowRequiredTaskDefinition, selection verifySelectionContext) []workflowRequiredTaskDefinition {
	if len(definitions) == 0 {
		return nil
	}

	seen := make(map[string]struct{}, len(definitions))
	matches := make([]workflowRequiredTaskDefinition, 0, len(definitions))
	for _, definition := range definitions {
		if !matchWorkflowRequiredTask(definition, selection) {
			continue
		}
		if _, ok := seen[definition.Key]; ok {
			continue
		}
		seen[definition.Key] = struct{}{}
		matches = append(matches, definition)
	}
	return matches
}

func matchWorkflowRequiredTask(definition workflowRequiredTaskDefinition, selection verifySelectionContext) bool {
	if definition.AlwaysRun {
		return true
	}

	if len(definition.Phases) > 0 {
		if selection.Phase == "" || !containsVerifyPhase(definition.Phases, selection.Phase) {
			return false
		}
	}
	if len(definition.TagsAny) > 0 {
		if _, ok := firstVerifyStringIntersection(definition.TagsAny, selection.Tags); !ok {
			return false
		}
	}
	if len(definition.ChangedPathsAny) > 0 {
		if _, ok := matchVerifyChangedPaths(definition.ChangedPathsAny, selection.FilesChanged); !ok {
			return false
		}
	}
	return true
}

func defaultCompletionRequiredTaskKeys(hasFilesChanged bool) []string {
	if !hasFilesChanged {
		return nil
	}
	return []string{requiredVerifyTestsKey}
}

func normalizeCompletionRequiredTaskKeys(keys []string) []string {
	return normalizeValues(keys)
}

func hasConfiguredWorkflowRequiredTasks(source workflowDefinitionsSource, definitions []workflowRequiredTaskDefinition) bool {
	return source.Exists && len(definitions) > 0
}

func sortWorkflowRequiredTaskKeys(keys []string) []string {
	out := append([]string(nil), keys...)
	sort.Strings(out)
	return out
}

func findWorkflowRequiredTaskDefinition(definitions []workflowRequiredTaskDefinition, key string) (workflowRequiredTaskDefinition, bool) {
	needle := strings.TrimSpace(key)
	if needle == "" {
		return workflowRequiredTaskDefinition{}, false
	}
	for _, definition := range definitions {
		if definition.Key == needle {
			return definition, true
		}
	}
	return workflowRequiredTaskDefinition{}, false
}
