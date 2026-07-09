package bootstrap

import (
	"bytes"
	"embed"
	"encoding/json"
	"errors"
	"fmt"
	"io/fs"
	"os"
	"path"
	"path/filepath"
	"reflect"
	"sort"
	"strconv"
	"strings"

	"gopkg.in/yaml.v3"

	"github.com/bonztm/agent-workflow-manager/internal/contracts/v1"
)

//go:embed all:bootstrap_templates/**
var initTemplateFS embed.FS

const (
	initTemplateManifestVersion             = "awm.init-template.v1"
	initTemplateOpCreateIfMissing           = "create_if_missing"
	initTemplateOpCreateOrReplaceIfPristine = "create_or_replace_if_pristine"
	initTemplateOpReplaceIfPristine         = "replace_if_pristine"
	initTemplateOpMergeJSON                 = "merge_json"
	// BlankRulesContents is the empty canonical ruleset scaffolded when a
	// project has no ruleset yet.
	BlankRulesContents = "version: awm.rules.v1\nrules: []\n"
	// BlankTestsContents is the empty verify-tests document scaffolded when a
	// project has no tests file yet.
	BlankTestsContents = "version: awm.tests.v1\ndefaults:\n  cwd: .\n  timeout_sec: 300\ntests: []\n"
	// BlankWorkflowsContents is the empty workflow-definitions document
	// scaffolded when a project has no workflows file yet.
	BlankWorkflowsContents = "version: awm.workflows.v1\ncompletion:\n  required_tasks: []\n"
)

type initTemplateManifest struct {
	Version    string                          `yaml:"version"`
	ID         string                          `yaml:"id"`
	Summary    string                          `yaml:"summary"`
	Operations []initTemplateOperationManifest `yaml:"operations"`
}

type initTemplateOperationManifest struct {
	Type     string   `yaml:"type"`
	Target   string   `yaml:"target"`
	Source   string   `yaml:"source,omitempty"`
	Mode     string   `yaml:"mode,omitempty"`
	Pristine []string `yaml:"pristine,omitempty"`
}

// Template is a resolved init template: a catalog entry identified by ID with
// the ordered file operations it applies to a project.
type Template struct {
	ID         string
	Summary    string
	Operations []initTemplateOperation
}

type initTemplateOperation struct {
	Type     string
	Target   string
	Source   string
	Mode     os.FileMode
	Pristine []string
}

type initTemplateContext struct {
	ProjectID string
	RepoName  string
}

// ApplyResult reports the outcome of applying init templates: one result per
// template plus the normalized repo-relative paths of newly created files.
type ApplyResult struct {
	TemplateResults []v1.InitTemplateResult
	CandidatePaths  []string
}

// UnknownTemplateError reports a requested init template ID that does not
// exist in the embedded catalog.
type UnknownTemplateError struct {
	TemplateID string
}

func (e UnknownTemplateError) Error() string {
	return fmt.Sprintf("unknown init template %q", e.TemplateID)
}

// ResolveTemplates looks up the given template IDs in the embedded catalog,
// deduplicating aliases, and returns them in request order. It returns an
// UnknownTemplateError for IDs not present in the catalog and nil when no IDs
// are supplied.
func ResolveTemplates(templateIDs []string) ([]Template, error) {
	normalizedIDs := normalizeValues(templateIDs)
	if len(normalizedIDs) == 0 {
		return nil, nil
	}

	catalog, err := loadInitTemplateCatalog()
	if err != nil {
		return nil, err
	}

	templates := make([]Template, 0, len(normalizedIDs))
	seen := make(map[string]struct{}, len(normalizedIDs))
	for _, templateID := range normalizedIDs {
		canonicalID := canonicalInitTemplateID(templateID)
		if _, ok := seen[canonicalID]; ok {
			continue
		}
		seen[canonicalID] = struct{}{}

		template, ok := catalog[canonicalID]
		if !ok {
			return nil, UnknownTemplateError{TemplateID: templateID}
		}
		templates = append(templates, template)
	}

	return templates, nil
}

func canonicalInitTemplateID(templateID string) string {
	return strings.TrimSpace(templateID)
}

func loadInitTemplateCatalog() (map[string]Template, error) {
	manifestPaths, err := fs.Glob(initTemplateFS, "bootstrap_templates/*/template.yaml")
	if err != nil {
		return nil, fmt.Errorf("glob manifests: %w", err)
	}
	sort.Strings(manifestPaths)

	catalog := make(map[string]Template, len(manifestPaths))
	for _, manifestPath := range manifestPaths {
		raw, err := initTemplateFS.ReadFile(manifestPath)
		if err != nil {
			return nil, fmt.Errorf("read manifest %s: %w", manifestPath, err)
		}

		var manifest initTemplateManifest
		if err := yaml.Unmarshal(raw, &manifest); err != nil {
			return nil, fmt.Errorf("parse manifest %s: %w", manifestPath, err)
		}
		if strings.TrimSpace(manifest.Version) != initTemplateManifestVersion {
			return nil, fmt.Errorf("manifest %s has unsupported version %q", manifestPath, manifest.Version)
		}

		templateID := strings.TrimSpace(manifest.ID)
		if templateID == "" {
			return nil, fmt.Errorf("manifest %s is missing id", manifestPath)
		}
		if _, exists := catalog[templateID]; exists {
			return nil, fmt.Errorf("duplicate init template id %q", templateID)
		}

		operations := make([]initTemplateOperation, 0, len(manifest.Operations))
		for i, operation := range manifest.Operations {
			resolved, err := resolveInitTemplateOperation(manifestPath, templateID, i, operation)
			if err != nil {
				return nil, err
			}
			operations = append(operations, resolved)
		}
		if len(operations) == 0 {
			return nil, fmt.Errorf("template %q must define at least one operation", templateID)
		}

		catalog[templateID] = Template{
			ID:         templateID,
			Summary:    strings.TrimSpace(manifest.Summary),
			Operations: operations,
		}
	}

	return catalog, nil
}

func resolveInitTemplateOperation(manifestPath, templateID string, index int, manifest initTemplateOperationManifest) (initTemplateOperation, error) {
	target := normalizeRelativePath(manifest.Target)
	if target == "" {
		return initTemplateOperation{}, fmt.Errorf("template %q operation %d has invalid target %q", templateID, index, manifest.Target)
	}

	switch strings.TrimSpace(manifest.Type) {
	case initTemplateOpCreateIfMissing, initTemplateOpCreateOrReplaceIfPristine, initTemplateOpReplaceIfPristine, initTemplateOpMergeJSON:
	default:
		return initTemplateOperation{}, fmt.Errorf("template %q operation %d has unsupported type %q", templateID, index, manifest.Type)
	}

	mode, err := parseInitTemplateMode(manifest.Mode)
	if err != nil {
		return initTemplateOperation{}, fmt.Errorf("template %q operation %d: %w", templateID, index, err)
	}

	var source string
	if requiresInitTemplateSource(strings.TrimSpace(manifest.Type)) {
		source, err = resolveInitTemplateSource(manifestPath, manifest.Source)
		if err != nil {
			return initTemplateOperation{}, fmt.Errorf("template %q operation %d: %w", templateID, index, err)
		}
	}

	return initTemplateOperation{
		Type:     strings.TrimSpace(manifest.Type),
		Target:   target,
		Source:   source,
		Mode:     mode,
		Pristine: normalizeValues(manifest.Pristine),
	}, nil
}

func requiresInitTemplateSource(opType string) bool {
	switch opType {
	case initTemplateOpCreateIfMissing, initTemplateOpCreateOrReplaceIfPristine, initTemplateOpReplaceIfPristine, initTemplateOpMergeJSON:
		return true
	default:
		return false
	}
}

func resolveInitTemplateSource(manifestPath, source string) (string, error) {
	trimmed := strings.TrimSpace(source)
	if trimmed == "" {
		return "", errors.New("source is required")
	}
	if strings.HasPrefix(trimmed, "/") || strings.Contains(trimmed, "\\") {
		return "", fmt.Errorf("source %q is invalid", source)
	}

	cleaned := path.Clean(trimmed)
	if cleaned == "." || cleaned == ".." || strings.HasPrefix(cleaned, "../") {
		return "", fmt.Errorf("source %q is invalid", source)
	}

	resolved := path.Join(path.Dir(manifestPath), cleaned)
	info, err := fs.Stat(initTemplateFS, resolved)
	if err != nil {
		return "", fmt.Errorf("source %q not found", resolved)
	}
	if info.IsDir() {
		return "", fmt.Errorf("source %q must be a file", resolved)
	}
	return resolved, nil
}

func parseInitTemplateMode(raw string) (os.FileMode, error) {
	trimmed := strings.TrimSpace(raw)
	if trimmed == "" {
		return 0o644, nil
	}
	value, err := strconv.ParseUint(trimmed, 8, 32)
	if err != nil {
		return 0, fmt.Errorf("invalid mode %q", raw)
	}
	return os.FileMode(value), nil
}

// ApplyTemplates applies each template's operations under projectRoot,
// rendering assets with projectID and the repo name. It stops at the first
// operation error and otherwise aggregates per-template results and created
// paths into an ApplyResult.
func ApplyTemplates(projectRoot, projectID string, templates []Template) (ApplyResult, error) {
	if len(templates) == 0 {
		return ApplyResult{}, nil
	}

	ctx := initTemplateContext{
		ProjectID: strings.TrimSpace(projectID),
		RepoName:  filepath.Base(filepath.Clean(projectRoot)),
	}

	results := make([]v1.InitTemplateResult, 0, len(templates))
	candidatePaths := make([]string, 0)
	for _, template := range templates {
		result, createdPaths, err := applyInitTemplate(projectRoot, ctx, template)
		if err != nil {
			return ApplyResult{}, fmt.Errorf("apply template %s: %w", template.ID, err)
		}
		results = append(results, result)
		candidatePaths = append(candidatePaths, createdPaths...)
	}

	return ApplyResult{
		TemplateResults: results,
		CandidatePaths:  normalizeRelativePaths(candidatePaths),
	}, nil
}

func applyInitTemplate(projectRoot string, ctx initTemplateContext, template Template) (v1.InitTemplateResult, []string, error) {
	result := v1.InitTemplateResult{TemplateID: template.ID}
	createdPaths := make([]string, 0)

	for _, operation := range template.Operations {
		created, err := applyInitTemplateOperation(projectRoot, ctx, operation, &result)
		if err != nil {
			return v1.InitTemplateResult{}, nil, err
		}
		if created {
			createdPaths = append(createdPaths, operation.Target)
		}
	}

	result.Created = normalizeRelativePaths(result.Created)
	result.Updated = normalizeRelativePaths(result.Updated)
	result.Unchanged = normalizeRelativePaths(result.Unchanged)
	sortInitTemplateConflicts(result.SkippedConflicts)
	if len(result.Created) == 0 {
		result.Created = nil
	}
	if len(result.Updated) == 0 {
		result.Updated = nil
	}
	if len(result.Unchanged) == 0 {
		result.Unchanged = nil
	}
	if len(result.SkippedConflicts) == 0 {
		result.SkippedConflicts = nil
	}

	return result, normalizeRelativePaths(createdPaths), nil
}

func applyInitTemplateOperation(projectRoot string, ctx initTemplateContext, operation initTemplateOperation, result *v1.InitTemplateResult) (bool, error) {
	switch operation.Type {
	case initTemplateOpCreateIfMissing:
		return applyInitTemplateCreateIfMissing(projectRoot, ctx, operation, result)
	case initTemplateOpCreateOrReplaceIfPristine:
		return applyInitTemplateCreateOrReplaceIfPristine(projectRoot, ctx, operation, result)
	case initTemplateOpReplaceIfPristine:
		return applyInitTemplateReplaceIfPristine(projectRoot, ctx, operation, result)
	case initTemplateOpMergeJSON:
		return applyInitTemplateMergeJSON(projectRoot, ctx, operation, result)
	default:
		return false, fmt.Errorf("unsupported template operation %q", operation.Type)
	}
}

func applyInitTemplateCreateIfMissing(projectRoot string, ctx initTemplateContext, operation initTemplateOperation, result *v1.InitTemplateResult) (bool, error) {
	rendered, err := renderInitTemplateAsset(operation.Source, ctx)
	if err != nil {
		return false, err
	}

	targetPath := filepath.Join(projectRoot, filepath.FromSlash(operation.Target))
	status, err := writeInitTemplateFile(targetPath, rendered, operation.Mode)
	if err != nil {
		return false, err
	}

	switch status {
	case "created":
		result.Created = append(result.Created, operation.Target)
		return true, nil
	case "updated":
		result.Updated = append(result.Updated, operation.Target)
		return false, nil
	case "unchanged":
		result.Unchanged = append(result.Unchanged, operation.Target)
		return false, nil
	case "conflict":
		result.SkippedConflicts = append(result.SkippedConflicts, v1.InitTemplateConflict{
			Path:   operation.Target,
			Reason: "existing file differs",
		})
		return false, nil
	default:
		return false, fmt.Errorf("unexpected file status %q", status)
	}
}

func applyInitTemplateCreateOrReplaceIfPristine(projectRoot string, ctx initTemplateContext, operation initTemplateOperation, result *v1.InitTemplateResult) (bool, error) {
	rendered, err := renderInitTemplateAsset(operation.Source, ctx)
	if err != nil {
		return false, err
	}

	targetPath := filepath.Join(projectRoot, filepath.FromSlash(operation.Target))
	existing, err := os.ReadFile(targetPath)
	switch {
	case errors.Is(err, os.ErrNotExist):
		if writeErr := writeInitTemplateNewFile(targetPath, rendered, operation.Mode); writeErr != nil {
			return false, writeErr
		}
		result.Created = append(result.Created, operation.Target)
		return true, nil
	case err != nil:
		return false, err
	}

	if bytes.Equal(existing, rendered) {
		if modeUpdated, modeErr := ensureInitTemplateMode(targetPath, operation.Mode); modeErr != nil {
			return false, modeErr
		} else if modeUpdated {
			result.Updated = append(result.Updated, operation.Target)
		} else {
			result.Unchanged = append(result.Unchanged, operation.Target)
		}
		return false, nil
	}

	pristineMatch, err := matchesInitPristineContent(existing, operation.Pristine)
	if err != nil {
		return false, err
	}
	if !pristineMatch {
		result.SkippedConflicts = append(result.SkippedConflicts, v1.InitTemplateConflict{
			Path:   operation.Target,
			Reason: "existing file differs from init scaffold",
		})
		return false, nil
	}

	if err := os.WriteFile(targetPath, rendered, operation.Mode); err != nil {
		return false, err
	}
	result.Updated = append(result.Updated, operation.Target)
	return false, nil
}

func applyInitTemplateReplaceIfPristine(projectRoot string, ctx initTemplateContext, operation initTemplateOperation, result *v1.InitTemplateResult) (bool, error) {
	targetPath := filepath.Join(projectRoot, filepath.FromSlash(operation.Target))
	existing, err := os.ReadFile(targetPath)
	switch {
	case errors.Is(err, os.ErrNotExist):
		return false, nil
	case err != nil:
		return false, err
	}

	rendered, err := renderInitTemplateAsset(operation.Source, ctx)
	if err != nil {
		return false, err
	}
	if bytes.Equal(existing, rendered) {
		if modeUpdated, modeErr := ensureInitTemplateMode(targetPath, operation.Mode); modeErr != nil {
			return false, modeErr
		} else if modeUpdated {
			result.Updated = append(result.Updated, operation.Target)
		} else {
			result.Unchanged = append(result.Unchanged, operation.Target)
		}
		return false, nil
	}

	pristineMatch, err := matchesInitPristineContent(existing, operation.Pristine)
	if err != nil {
		return false, err
	}
	if !pristineMatch {
		result.SkippedConflicts = append(result.SkippedConflicts, v1.InitTemplateConflict{
			Path:   operation.Target,
			Reason: "existing file differs from init scaffold",
		})
		return false, nil
	}

	if err := os.WriteFile(targetPath, rendered, operation.Mode); err != nil {
		return false, err
	}
	result.Updated = append(result.Updated, operation.Target)
	return false, nil
}

func applyInitTemplateMergeJSON(projectRoot string, ctx initTemplateContext, operation initTemplateOperation, result *v1.InitTemplateResult) (bool, error) {
	rendered, err := renderInitTemplateAsset(operation.Source, ctx)
	if err != nil {
		return false, err
	}

	var sourceValue any
	if unmarshalErr := json.Unmarshal(rendered, &sourceValue); unmarshalErr != nil {
		return false, fmt.Errorf("parse json asset %s: %w", operation.Source, unmarshalErr)
	}

	targetPath := filepath.Join(projectRoot, filepath.FromSlash(operation.Target))
	existing, err := os.ReadFile(targetPath)
	switch {
	case errors.Is(err, os.ErrNotExist):
		blob, marshalErr := marshalInitTemplateJSON(sourceValue)
		if marshalErr != nil {
			return false, marshalErr
		}
		if writeErr := writeInitTemplateNewFile(targetPath, blob, operation.Mode); writeErr != nil {
			return false, writeErr
		}
		result.Created = append(result.Created, operation.Target)
		return true, nil
	case err != nil:
		return false, err
	}

	var targetValue any
	if unmarshalErr := json.Unmarshal(existing, &targetValue); unmarshalErr != nil {
		result.SkippedConflicts = append(result.SkippedConflicts, v1.InitTemplateConflict{
			Path:   operation.Target,
			Reason: "existing file is not valid JSON",
		})
		return false, nil //nolint:nilerr // invalid existing JSON is recorded as a conflict, not a failure
	}

	mergedValue, changed, err := mergeInitJSONValues(targetValue, sourceValue)
	if err != nil {
		result.SkippedConflicts = append(result.SkippedConflicts, v1.InitTemplateConflict{
			Path:   operation.Target,
			Reason: err.Error(),
		})
		return false, nil //nolint:nilerr // merge incompatibilities are recorded as conflicts, not failures
	}
	if !changed {
		if modeUpdated, modeErr := ensureInitTemplateMode(targetPath, operation.Mode); modeErr != nil {
			return false, modeErr
		} else if modeUpdated {
			result.Updated = append(result.Updated, operation.Target)
		} else {
			result.Unchanged = append(result.Unchanged, operation.Target)
		}
		return false, nil
	}

	blob, err := marshalInitTemplateJSON(mergedValue)
	if err != nil {
		return false, err
	}
	if err := os.WriteFile(targetPath, blob, operation.Mode); err != nil {
		return false, err
	}
	result.Updated = append(result.Updated, operation.Target)
	return false, nil
}

func renderInitTemplateAsset(source string, ctx initTemplateContext) ([]byte, error) {
	raw, err := initTemplateFS.ReadFile(source)
	if err != nil {
		return nil, fmt.Errorf("read asset %s: %w", source, err)
	}

	replacer := strings.NewReplacer(
		"{{project_id}}", ctx.ProjectID,
		"{{repo_name}}", ctx.RepoName,
	)
	return []byte(replacer.Replace(string(raw))), nil
}

func writeInitTemplateFile(targetPath string, content []byte, mode os.FileMode) (string, error) {
	existing, err := os.ReadFile(targetPath)
	switch {
	case errors.Is(err, os.ErrNotExist):
		if writeErr := writeInitTemplateNewFile(targetPath, content, mode); writeErr != nil {
			return "", writeErr
		}
		return "created", nil
	case err != nil:
		return "", err
	}

	if !bytes.Equal(existing, content) {
		return "conflict", nil
	}
	modeUpdated, err := ensureInitTemplateMode(targetPath, mode)
	if err != nil {
		return "", err
	}
	if modeUpdated {
		return "updated", nil
	}
	return "unchanged", nil
}

func writeInitTemplateNewFile(targetPath string, content []byte, mode os.FileMode) error {
	if err := os.MkdirAll(filepath.Dir(targetPath), 0o755); err != nil { //nolint:gosec // G301: scaffolded repo directory, standard world-readable permissions by design
		return err
	}
	file, err := os.OpenFile(targetPath, os.O_WRONLY|os.O_CREATE|os.O_EXCL, mode)
	if err != nil {
		return err
	}
	defer file.Close()

	if _, err := file.Write(content); err != nil {
		return err
	}
	return nil
}

func ensureInitTemplateMode(targetPath string, mode os.FileMode) (bool, error) {
	info, err := os.Stat(targetPath)
	if err != nil {
		return false, err
	}
	current := info.Mode().Perm()
	desired := mode.Perm()
	if current == desired {
		return false, nil
	}
	if err := os.Chmod(targetPath, desired); err != nil {
		return false, err
	}
	return true, nil
}

func matchesInitPristineContent(existing []byte, pristineIDs []string) (bool, error) {
	for _, pristineID := range pristineIDs {
		pristine, ok := initPristineContent(pristineID)
		if !ok {
			return false, fmt.Errorf("unknown pristine content %q", pristineID)
		}
		if bytes.Equal(existing, pristine) {
			return true, nil
		}
	}
	return false, nil
}

func initPristineContent(pristineID string) ([]byte, bool) {
	switch strings.TrimSpace(pristineID) {
	case "blank_rules_v1":
		return []byte(BlankRulesContents), true
	case "blank_tests_v1":
		return []byte(BlankTestsContents), true
	case "blank_workflows_v1":
		return []byte(BlankWorkflowsContents), true
	case "starter_contract_agents_v1":
		return initPristineEmbeddedContent("bootstrap_templates/starter-contract/files/AGENTS.md")
	case "starter_contract_claude_v1":
		return initPristineEmbeddedContent("bootstrap_templates/starter-contract/files/CLAUDE.md")
	case "starter_contract_rules_v1":
		return initPristineEmbeddedContent("bootstrap_templates/starter-contract/files/.awm/awm-rules.yaml")
	case "verify_generic_tests_v1":
		return initPristineEmbeddedContent("bootstrap_templates/verify-generic/files/.awm/awm-tests.yaml")
	default:
		return nil, false
	}
}

func initPristineEmbeddedContent(source string) ([]byte, bool) {
	raw, err := initTemplateFS.ReadFile(source)
	if err != nil {
		return nil, false
	}
	return raw, true
}

func marshalInitTemplateJSON(value any) ([]byte, error) {
	blob, err := json.MarshalIndent(value, "", "  ")
	if err != nil {
		return nil, err
	}
	return append(blob, '\n'), nil
}

func mergeInitJSONValues(target, source any) (any, bool, error) {
	switch typedSource := source.(type) {
	case map[string]any:
		typedTarget, ok := target.(map[string]any)
		if !ok {
			return nil, false, errors.New("existing JSON structure conflicts with template")
		}
		merged := cloneInitJSONMap(typedTarget)
		changed := false
		for key, sourceValue := range typedSource {
			targetValue, exists := merged[key]
			if !exists {
				merged[key] = cloneInitJSONValue(sourceValue)
				changed = true
				continue
			}
			mergedValue, childChanged, err := mergeInitJSONValues(targetValue, sourceValue)
			if err != nil {
				return nil, false, err
			}
			if childChanged {
				merged[key] = mergedValue
				changed = true
			}
		}
		return merged, changed, nil
	case []any:
		typedTarget, ok := target.([]any)
		if !ok {
			return nil, false, errors.New("existing JSON structure conflicts with template")
		}
		merged := cloneInitJSONArray(typedTarget)
		changed := false
		for _, sourceValue := range typedSource {
			if initJSONArrayContains(merged, sourceValue) {
				continue
			}
			merged = append(merged, cloneInitJSONValue(sourceValue))
			changed = true
		}
		return merged, changed, nil
	default:
		if reflect.DeepEqual(target, source) {
			return target, false, nil
		}
		return nil, false, errors.New("existing JSON structure conflicts with template")
	}
}

func initJSONArrayContains(values []any, candidate any) bool {
	for _, value := range values {
		if reflect.DeepEqual(value, candidate) {
			return true
		}
	}
	return false
}

func cloneInitJSONValue(value any) any {
	switch typed := value.(type) {
	case map[string]any:
		return cloneInitJSONMap(typed)
	case []any:
		return cloneInitJSONArray(typed)
	default:
		return typed
	}
}

func cloneInitJSONMap(value map[string]any) map[string]any {
	cloned := make(map[string]any, len(value))
	for key, child := range value {
		cloned[key] = cloneInitJSONValue(child)
	}
	return cloned
}

func cloneInitJSONArray(value []any) []any {
	cloned := make([]any, 0, len(value))
	for _, child := range value {
		cloned = append(cloned, cloneInitJSONValue(child))
	}
	return cloned
}

func sortInitTemplateConflicts(conflicts []v1.InitTemplateConflict) {
	sort.Slice(conflicts, func(i, j int) bool {
		if conflicts[i].Path == conflicts[j].Path {
			return conflicts[i].Reason < conflicts[j].Reason
		}
		return conflicts[i].Path < conflicts[j].Path
	})
}

func normalizeValues(values []string) []string {
	if len(values) == 0 {
		return nil
	}

	seen := make(map[string]struct{}, len(values))
	out := make([]string, 0, len(values))
	for _, raw := range values {
		trimmed := strings.TrimSpace(raw)
		if trimmed == "" {
			continue
		}
		if _, ok := seen[trimmed]; ok {
			continue
		}
		seen[trimmed] = struct{}{}
		out = append(out, trimmed)
	}
	if len(out) == 0 {
		return nil
	}
	sort.Strings(out)
	return out
}

func normalizeRelativePaths(paths []string) []string {
	if len(paths) == 0 {
		return nil
	}

	seen := make(map[string]struct{}, len(paths))
	out := make([]string, 0, len(paths))
	for _, raw := range paths {
		normalized := normalizeRelativePath(raw)
		if normalized == "" {
			continue
		}
		if _, ok := seen[normalized]; ok {
			continue
		}
		seen[normalized] = struct{}{}
		out = append(out, normalized)
	}
	sort.Strings(out)
	return out
}

func normalizeRelativePath(raw string) string {
	trimmed := strings.TrimSpace(raw)
	if trimmed == "" {
		return ""
	}
	normalizedSlashes := strings.ReplaceAll(trimmed, "\\", "/")
	if strings.HasPrefix(normalizedSlashes, "/") || isWindowsAbsolutePath(normalizedSlashes) {
		return ""
	}
	cleaned := path.Clean(normalizedSlashes)
	if cleaned == "." || cleaned == ".." || strings.HasPrefix(cleaned, "../") {
		return ""
	}
	return cleaned
}

func isWindowsAbsolutePath(value string) bool {
	return len(value) >= 3 && ((value[0] >= 'A' && value[0] <= 'Z') || (value[0] >= 'a' && value[0] <= 'z')) && value[1] == ':' && value[2] == '/'
}
