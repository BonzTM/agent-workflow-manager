package backend

import (
	"context"
	"encoding/json"
	"fmt"
	"maps"
	"strconv"
	"strings"

	"github.com/bonztm/agent-workflow-manager/internal/contracts/v1"
	"github.com/bonztm/agent-workflow-manager/internal/core"
)

// Export resolves exactly one selector (context, fetch, history, or status)
// into an export document and renders it in the requested format.
func (s *Service) Export(ctx context.Context, payload v1.ExportPayload) (v1.ExportResult, *core.APIError) {
	if s == nil || s.repo == nil {
		return v1.ExportResult{}, backendError(v1.ErrCodeInternalError, "service repository is not configured", nil)
	}

	document, apiErr := s.resolveExportDocument(ctx, payload)
	if apiErr != nil {
		return v1.ExportResult{}, apiErr
	}

	content, apiErr := renderExportContent(payload.Format, document)
	if apiErr != nil {
		return v1.ExportResult{}, apiErr
	}

	return v1.ExportResult{
		Format:   payload.Format,
		Document: document,
		Content:  content,
	}, nil
}

func (s *Service) resolveExportDocument(ctx context.Context, payload v1.ExportPayload) (*v1.ExportDocument, *core.APIError) {
	switch {
	case payload.Context != nil:
		return s.resolveContextExportDocument(ctx, payload.ProjectID, payload.Context)
	case payload.Fetch != nil:
		return s.resolveFetchExportDocument(ctx, payload.ProjectID, payload.Fetch)
	case payload.History != nil:
		return s.resolveHistoryExportDocument(ctx, payload.ProjectID, payload.History)
	case payload.Status != nil:
		return s.resolveStatusExportDocument(ctx, payload.ProjectID, payload.Status)
	default:
		return nil, backendError(v1.ErrCodeInvalidInput, "export requires exactly one selector", map[string]any{
			"operation": "resolve_export_document",
		})
	}
}

func (s *Service) resolveContextExportDocument(ctx context.Context, projectID string, selector *v1.ExportContextSelector) (*v1.ExportDocument, *core.APIError) {
	result, apiErr := s.Context(ctx, v1.ContextPayload{
		ProjectID:         strings.TrimSpace(projectID),
		TaskText:          selector.TaskText,
		Phase:             selector.Phase,
		TagsFile:          selector.TagsFile,
		InitialScopePaths: append([]string(nil), selector.InitialScopePaths...),
	})
	if apiErr != nil {
		return nil, apiErr
	}
	if result.Receipt == nil {
		return nil, backendError(v1.ErrCodeInternalError, "context export receipt is missing", map[string]any{
			"operation": "resolve_export_context",
		})
	}

	title := "Context " + strings.TrimSpace(result.Receipt.Meta.ReceiptID)
	summary := strings.TrimSpace(result.Receipt.Meta.TaskText)
	if summary == "" {
		summary = "Context for " + strings.TrimSpace(projectID)
	}

	return &v1.ExportDocument{
		Kind:    v1.ExportDocumentKindContext,
		Title:   title,
		Summary: summary,
		Context: result.Receipt,
	}, nil
}

func (s *Service) resolveFetchExportDocument(ctx context.Context, projectID string, selector *v1.ExportFetchSelector) (*v1.ExportDocument, *core.APIError) {
	fetchPayload := v1.FetchPayload{
		ProjectID:        strings.TrimSpace(projectID),
		Keys:             append([]string(nil), selector.Keys...),
		ReceiptID:        selector.ReceiptID,
		ExpectedVersions: cloneStringMap(selector.ExpectedVersions),
	}
	result, apiErr := s.Fetch(ctx, fetchPayload)
	if apiErr != nil {
		return nil, apiErr
	}

	requestedKeys := fetchPayloadKeys(fetchPayload)
	if len(result.NotFound) > 0 || len(result.VersionMismatches) > 0 || len(result.Items) != 1 {
		return s.resolveFetchBundleDocument(requestedKeys, result)
	}

	document, ok, apiErr := s.resolveTypedFetchExportDocument(result.Items[0])
	if apiErr != nil {
		return nil, apiErr
	}
	if ok {
		return document, nil
	}

	return s.resolveFetchBundleDocument(requestedKeys, result)
}

func (s *Service) resolveHistoryExportDocument(ctx context.Context, projectID string, selector *v1.ExportHistorySelector) (*v1.ExportDocument, *core.APIError) {
	result, apiErr := s.HistorySearch(ctx, v1.HistorySearchPayload{
		ProjectID: strings.TrimSpace(projectID),
		Entity:    selector.Entity,
		Query:     selector.Query,
		Scope:     selector.Scope,
		Kind:      selector.Kind,
		Limit:     selector.Limit,
		Unbounded: selector.Unbounded,
	})
	if apiErr != nil {
		return nil, apiErr
	}

	title := "History " + strings.TrimSpace(string(result.Entity))
	if result.Entity == "" {
		title = "History"
	}
	summary := fmt.Sprintf("%d item(s)", result.Count)
	if strings.TrimSpace(result.Query) != "" {
		summary = fmt.Sprintf("%s matching %q", summary, strings.TrimSpace(result.Query))
	}

	return &v1.ExportDocument{
		Kind:    v1.ExportDocumentKindHistory,
		Title:   title,
		Summary: summary,
		History: &result,
	}, nil
}

func (s *Service) resolveStatusExportDocument(ctx context.Context, projectID string, selector *v1.ExportStatusSelector) (*v1.ExportDocument, *core.APIError) {
	result, apiErr := s.Status(ctx, v1.StatusPayload{
		ProjectID:     strings.TrimSpace(projectID),
		ProjectRoot:   selector.ProjectRoot,
		RulesFile:     selector.RulesFile,
		TagsFile:      selector.TagsFile,
		TestsFile:     selector.TestsFile,
		WorkflowsFile: selector.WorkflowsFile,
		TaskText:      selector.TaskText,
		Phase:         selector.Phase,
	})
	if apiErr != nil {
		return nil, apiErr
	}

	title := "Status " + strings.TrimSpace(result.Project.ProjectID)
	summary := fmt.Sprintf("ready=%t missing=%d warnings=%d", result.Summary.Ready, result.Summary.MissingCount, result.Summary.WarningCount)

	return &v1.ExportDocument{
		Kind:    v1.ExportDocumentKindStatus,
		Title:   title,
		Summary: summary,
		Status:  &result,
	}, nil
}

func (s *Service) resolveTypedFetchExportDocument(item v1.FetchItem) (*v1.ExportDocument, bool, *core.APIError) {
	switch strings.TrimSpace(item.Type) {
	case "plan":
		document, apiErr := exportPlanDocumentFromFetchItem(item)
		if apiErr != nil {
			return nil, false, apiErr
		}
		return &v1.ExportDocument{
			Kind:    v1.ExportDocumentKindPlan,
			Title:   firstNonEmpty(document.Title, document.PlanKey),
			Summary: firstNonEmpty(item.Summary, document.Objective, document.PlanKey),
			Plan:    document,
		}, true, nil
	case "receipt":
		document, apiErr := exportReceiptDocumentFromFetchItem(item)
		if apiErr != nil {
			return nil, false, apiErr
		}
		return &v1.ExportDocument{
			Kind:    v1.ExportDocumentKindReceipt,
			Title:   "Receipt " + document.ReceiptID,
			Summary: firstNonEmpty(item.Summary, document.TaskText, document.ReceiptID),
			Receipt: document,
		}, true, nil
	case "task":
		document, apiErr := exportTaskDocumentFromFetchItem(item)
		if apiErr != nil {
			return nil, false, apiErr
		}
		return &v1.ExportDocument{
			Kind:    v1.ExportDocumentKindTask,
			Title:   firstNonEmpty(document.Summary, document.Key),
			Summary: firstNonEmpty(item.Summary, document.Summary, document.Key),
			Task:    document,
		}, true, nil
	case "run":
		document, apiErr := exportRunDocumentFromFetchItem(item)
		if apiErr != nil {
			return nil, false, apiErr
		}
		return &v1.ExportDocument{
			Kind:    v1.ExportDocumentKindRun,
			Title:   fmt.Sprintf("Run %d", document.RunID),
			Summary: firstNonEmpty(item.Summary, document.Outcome, document.TaskText),
			Run:     document,
		}, true, nil
	default:
		return nil, false, nil
	}
}

func (s *Service) resolveFetchBundleDocument(requestedKeys []string, result v1.FetchResult) (*v1.ExportDocument, *core.APIError) {
	items := make([]v1.ExportBundleItem, 0, len(result.Items))
	for _, item := range result.Items {
		exportItem, apiErr := s.resolveExportBundleItem(item)
		if apiErr != nil {
			return nil, apiErr
		}
		items = append(items, exportItem)
	}

	summary := fmt.Sprintf("%d item(s)", len(items))
	if len(result.NotFound) > 0 {
		summary = fmt.Sprintf("%s, %d missing", summary, len(result.NotFound))
	}
	if len(result.VersionMismatches) > 0 {
		summary = fmt.Sprintf("%s, %d version mismatch(es)", summary, len(result.VersionMismatches))
	}

	return &v1.ExportDocument{
		Kind:    v1.ExportDocumentKindFetchBundle,
		Title:   "Fetch bundle",
		Summary: summary,
		Bundle: &v1.ExportBundleDocument{
			RequestedKeys:     append([]string(nil), requestedKeys...),
			Items:             items,
			NotFound:          append([]string(nil), result.NotFound...),
			VersionMismatches: append([]v1.FetchVersionMismatch(nil), result.VersionMismatches...),
		},
	}, nil
}

func (s *Service) resolveExportBundleItem(item v1.FetchItem) (v1.ExportBundleItem, *core.APIError) {
	exportItem := v1.ExportBundleItem{
		Key:     strings.TrimSpace(item.Key),
		Type:    strings.TrimSpace(item.Type),
		Summary: strings.TrimSpace(item.Summary),
		Status:  strings.TrimSpace(item.Status),
		Version: strings.TrimSpace(item.Version),
	}

	switch strings.TrimSpace(item.Type) {
	case "plan":
		document, apiErr := exportPlanDocumentFromFetchItem(item)
		if apiErr != nil {
			return v1.ExportBundleItem{}, apiErr
		}
		exportItem.Kind = v1.ExportBundleItemKindPlan
		exportItem.Plan = document
	case "receipt":
		document, apiErr := exportReceiptDocumentFromFetchItem(item)
		if apiErr != nil {
			return v1.ExportBundleItem{}, apiErr
		}
		exportItem.Kind = v1.ExportBundleItemKindReceipt
		exportItem.Receipt = document
	case "task":
		document, apiErr := exportTaskDocumentFromFetchItem(item)
		if apiErr != nil {
			return v1.ExportBundleItem{}, apiErr
		}
		exportItem.Kind = v1.ExportBundleItemKindTask
		exportItem.Task = document
	case "run":
		document, apiErr := exportRunDocumentFromFetchItem(item)
		if apiErr != nil {
			return v1.ExportBundleItem{}, apiErr
		}
		exportItem.Kind = v1.ExportBundleItemKindRun
		exportItem.Run = document
	case "rule":
		exportItem.Kind = v1.ExportBundleItemKindRule
		exportItem.Content = item.Content
	default:
		exportItem.Kind = v1.ExportBundleItemKindPointer
		exportItem.Content = item.Content
	}

	return exportItem, nil
}

func exportPlanDocumentFromFetchItem(item v1.FetchItem) (*v1.ExportPlanDocument, *core.APIError) {
	document, err := decodeFetchContent[v1.ExportPlanDocument](item)
	if err != nil {
		return nil, exportContentDecodeError(item, err)
	}
	return document, nil
}

func exportReceiptDocumentFromFetchItem(item v1.FetchItem) (*v1.ExportReceiptDocument, *core.APIError) {
	document, err := decodeFetchContent[v1.ExportReceiptDocument](item)
	if err != nil {
		return nil, exportContentDecodeError(item, err)
	}
	return document, nil
}

func exportTaskDocumentFromFetchItem(item v1.FetchItem) (*v1.ExportTaskDocument, *core.APIError) {
	document, err := decodeFetchContent[v1.ExportTaskDocument](item)
	if err != nil {
		return nil, exportContentDecodeError(item, err)
	}
	return document, nil
}

func exportRunDocumentFromFetchItem(item v1.FetchItem) (*v1.ExportRunDocument, *core.APIError) {
	document, err := decodeFetchContent[v1.ExportRunDocument](item)
	if err != nil {
		return nil, exportContentDecodeError(item, err)
	}
	return document, nil
}

func decodeFetchContent[T any](item v1.FetchItem) (*T, error) {
	var document T
	if err := json.Unmarshal([]byte(item.Content), &document); err != nil {
		return nil, err
	}
	return &document, nil
}

func exportContentDecodeError(item v1.FetchItem, err error) *core.APIError {
	return backendError(v1.ErrCodeInternalError, "failed to decode fetch export content", map[string]any{
		"operation": "decode_fetch_export_content",
		"key":       strings.TrimSpace(item.Key),
		"type":      strings.TrimSpace(item.Type),
		"error":     err.Error(),
	})
}

func renderExportContent(format v1.ExportFormat, document *v1.ExportDocument) (string, *core.APIError) {
	switch format {
	case v1.ExportFormatMarkdown:
		return renderExportMarkdown(document), nil
	case v1.ExportFormatJSON:
		fallthrough
	default:
		content, err := json.MarshalIndent(document, "", "  ")
		if err != nil {
			return "", backendError(v1.ErrCodeInternalError, "failed to render export content", map[string]any{
				"operation": "render_export_json",
				"error":     err.Error(),
			})
		}
		return string(content), nil
	}
}

func renderExportMarkdown(document *v1.ExportDocument) string {
	if document == nil {
		return ""
	}
	switch document.Kind {
	case v1.ExportDocumentKindContext:
		return renderContextMarkdown(document)
	case v1.ExportDocumentKindPlan:
		return renderPlanMarkdown(document)
	case v1.ExportDocumentKindReceipt:
		return renderReceiptMarkdown(document)
	case v1.ExportDocumentKindTask:
		return renderTaskMarkdown(document)
	case v1.ExportDocumentKindRun:
		return renderRunMarkdown(document)
	case v1.ExportDocumentKindHistory:
		return renderHistoryMarkdown(document)
	case v1.ExportDocumentKindStatus:
		return renderStatusMarkdown(document)
	case v1.ExportDocumentKindFetchBundle:
		return renderBundleMarkdown(document)
	default:
		return firstNonEmpty(document.Summary, document.Title)
	}
}

func renderContextMarkdown(document *v1.ExportDocument) string {
	var b strings.Builder
	appendMarkdownHeading(&b, 1, firstNonEmpty(document.Title, "Context"))
	appendMarkdownSummary(&b, document.Summary)
	if document.Context == nil {
		return strings.TrimSpace(b.String())
	}

	appendMarkdownKeyValue(&b, "Receipt ID", strings.TrimSpace(document.Context.Meta.ReceiptID))
	appendMarkdownKeyValue(&b, "Project ID", strings.TrimSpace(document.Context.Meta.ProjectID))
	appendMarkdownKeyValue(&b, "Task", strings.TrimSpace(document.Context.Meta.TaskText))
	appendMarkdownKeyValue(&b, "Phase", strings.TrimSpace(string(document.Context.Meta.Phase)))
	appendMarkdownStringListInline(&b, "Resolved Tags", document.Context.Meta.ResolvedTags)
	appendMarkdownKeyValue(&b, "Baseline Captured", markdownBool(document.Context.Meta.BaselineCaptured))

	appendMarkdownRuleSection(&b, document.Context.Rules)
	appendMarkdownContextPlanSection(&b, document.Context.Plans)
	appendMarkdownStringListSection(&b, 2, "Initial Scope", document.Context.InitialScopePaths, true)

	return strings.TrimSpace(b.String())
}

func renderPlanMarkdown(document *v1.ExportDocument) string {
	var b strings.Builder
	appendMarkdownHeading(&b, 1, firstNonEmpty(document.Title, "Plan"))
	appendMarkdownSummary(&b, document.Summary)
	if document.Plan == nil {
		return strings.TrimSpace(b.String())
	}

	appendPlanMetadata(&b, document.Plan)
	appendPlanStages(&b, document.Plan.Stages)
	appendMarkdownStringListSection(&b, 2, "In Scope", document.Plan.InScope, true)
	appendMarkdownStringListSection(&b, 2, "Out Of Scope", document.Plan.OutOfScope, true)
	appendMarkdownStringListSection(&b, 2, "Discovered Paths", document.Plan.DiscoveredPaths, true)
	appendMarkdownStringListSection(&b, 2, "Constraints", document.Plan.Constraints, false)
	appendMarkdownStringListSection(&b, 2, "References", document.Plan.References, true)
	appendMarkdownStringListSection(&b, 2, "External References", document.Plan.ExternalRefs, false)
	appendPlanTasks(&b, document.Plan.Tasks)

	return strings.TrimSpace(b.String())
}

func renderReceiptMarkdown(document *v1.ExportDocument) string {
	var b strings.Builder
	appendMarkdownHeading(&b, 1, firstNonEmpty(document.Title, "Receipt"))
	appendMarkdownSummary(&b, document.Summary)
	if document.Receipt == nil {
		return strings.TrimSpace(b.String())
	}

	appendMarkdownKeyValue(&b, "Receipt ID", document.Receipt.ReceiptID)
	appendMarkdownKeyValue(&b, "Task", strings.TrimSpace(document.Receipt.TaskText))
	appendMarkdownKeyValue(&b, "Phase", strings.TrimSpace(string(document.Receipt.Phase)))
	appendMarkdownStringListInline(&b, "Resolved Tags", document.Receipt.ResolvedTags)
	appendMarkdownStringListInline(&b, "Pointer Keys", document.Receipt.PointerKeys)
	appendMarkdownStringListInline(&b, "Initial Scope", document.Receipt.InitialScopePaths)
	appendMarkdownKeyValue(&b, "Baseline Captured", markdownBool(document.Receipt.BaselineCaptured))

	if len(document.Receipt.BaselinePaths) > 0 {
		appendMarkdownHeading(&b, 2, "Baseline Paths")
		for _, path := range document.Receipt.BaselinePaths {
			line := fmt.Sprintf("`%s` deleted=%t", strings.TrimSpace(path.Path), path.Deleted)
			if strings.TrimSpace(path.ContentHash) != "" {
				line = fmt.Sprintf("%s hash=%s", line, strings.TrimSpace(path.ContentHash))
			}
			appendMarkdownListItem(&b, line)
		}
		b.WriteString("\n")
	}

	if document.Receipt.LatestRun != nil {
		appendMarkdownHeading(&b, 2, "Latest Run")
		appendMarkdownKeyValue(&b, "Run ID", strconv.FormatInt(document.Receipt.LatestRun.RunID, 10))
		appendMarkdownKeyValue(&b, "Status", strings.TrimSpace(document.Receipt.LatestRun.Status))
		appendMarkdownKeyValue(&b, "Plan Status", strings.TrimSpace(string(document.Receipt.LatestRun.PlanStatus)))
		appendTaskSection(&b, 3, "Tasks", document.Receipt.LatestRun.Tasks)
	}

	return strings.TrimSpace(b.String())
}

func renderTaskMarkdown(document *v1.ExportDocument) string {
	var b strings.Builder
	appendMarkdownHeading(&b, 1, firstNonEmpty(document.Title, "Task"))
	appendMarkdownSummary(&b, document.Summary)
	if document.Task == nil {
		return strings.TrimSpace(b.String())
	}
	appendTaskDetails(&b, 2, document.Task, true)
	return strings.TrimSpace(b.String())
}

func renderRunMarkdown(document *v1.ExportDocument) string {
	var b strings.Builder
	appendMarkdownHeading(&b, 1, firstNonEmpty(document.Title, "Run"))
	appendMarkdownSummary(&b, document.Summary)
	if document.Run == nil {
		return strings.TrimSpace(b.String())
	}

	appendMarkdownKeyValue(&b, "Run ID", strconv.FormatInt(document.Run.RunID, 10))
	appendMarkdownKeyValue(&b, "Receipt ID", strings.TrimSpace(document.Run.ReceiptID))
	appendMarkdownKeyValue(&b, "Request ID", strings.TrimSpace(document.Run.RequestID))
	appendMarkdownKeyValue(&b, "Task", strings.TrimSpace(document.Run.TaskText))
	appendMarkdownKeyValue(&b, "Phase", strings.TrimSpace(string(document.Run.Phase)))
	appendMarkdownKeyValue(&b, "Status", strings.TrimSpace(document.Run.Status))
	appendMarkdownKeyValue(&b, "Outcome", strings.TrimSpace(document.Run.Outcome))
	appendMarkdownKeyValue(&b, "Updated At", strings.TrimSpace(document.Run.UpdatedAt))
	appendMarkdownStringListSection(&b, 2, "Files Changed", document.Run.FilesChanged, true)

	return strings.TrimSpace(b.String())
}

func renderHistoryMarkdown(document *v1.ExportDocument) string {
	var b strings.Builder
	appendMarkdownHeading(&b, 1, firstNonEmpty(document.Title, "History"))
	appendMarkdownSummary(&b, document.Summary)
	if document.History == nil {
		return strings.TrimSpace(b.String())
	}

	appendMarkdownKeyValue(&b, "Entity", strings.TrimSpace(string(document.History.Entity)))
	appendMarkdownKeyValue(&b, "Scope", strings.TrimSpace(string(document.History.Scope)))
	appendMarkdownKeyValue(&b, "Query", strings.TrimSpace(document.History.Query))
	appendMarkdownKeyValue(&b, "Limit", markdownInt(document.History.Limit))
	appendMarkdownKeyValue(&b, "Count", markdownInt(document.History.Count))

	if len(document.History.Items) > 0 {
		appendMarkdownHeading(&b, 2, "Items")
		for _, item := range document.History.Items {
			appendMarkdownHeading(&b, 3, firstNonEmpty(strings.TrimSpace(item.Summary), strings.TrimSpace(item.Key)))
			appendMarkdownKeyValue(&b, "Key", strings.TrimSpace(item.Key))
			appendMarkdownKeyValue(&b, "Entity", strings.TrimSpace(string(item.Entity)))
			appendMarkdownKeyValue(&b, "Status", strings.TrimSpace(item.Status))
			appendMarkdownKeyValue(&b, "Scope", strings.TrimSpace(string(item.Scope)))
			appendMarkdownKeyValue(&b, "Plan Key", strings.TrimSpace(item.PlanKey))
			appendMarkdownKeyValue(&b, "Receipt ID", strings.TrimSpace(item.ReceiptID))
			appendMarkdownKeyValue(&b, "Run ID", markdownInt64(item.RunID))
			appendMarkdownKeyValue(&b, "Request ID", strings.TrimSpace(item.RequestID))
			appendMarkdownKeyValue(&b, "Phase", strings.TrimSpace(string(item.Phase)))
			appendMarkdownKeyValue(&b, "Kind", strings.TrimSpace(item.Kind))
			appendMarkdownKeyValue(&b, "Parent Plan", strings.TrimSpace(item.ParentPlanKey))
			appendMarkdownKeyValue(&b, "Updated At", strings.TrimSpace(item.UpdatedAt))
			if item.TaskCounts != nil {
				appendMarkdownListItem(&b, fmt.Sprintf("Task Counts: total=%d pending=%d in_progress=%d blocked=%d complete=%d",
					item.TaskCounts.Total,
					item.TaskCounts.Pending,
					item.TaskCounts.InProgress,
					item.TaskCounts.Blocked,
					item.TaskCounts.Complete,
				))
			}
			appendMarkdownStringListInline(&b, "Fetch Keys", item.FetchKeys)
			b.WriteString("\n")
		}
	}

	return strings.TrimSpace(b.String())
}

func renderStatusMarkdown(document *v1.ExportDocument) string {
	var b strings.Builder
	appendMarkdownHeading(&b, 1, firstNonEmpty(document.Title, "Status"))
	appendMarkdownSummary(&b, document.Summary)
	if document.Status == nil {
		return strings.TrimSpace(b.String())
	}

	appendMarkdownKeyValue(&b, "Ready", markdownBool(document.Status.Summary.Ready))
	appendMarkdownKeyValue(&b, "Missing Count", markdownInt(document.Status.Summary.MissingCount))
	appendMarkdownKeyValue(&b, "Warning Count", markdownInt(document.Status.Summary.WarningCount))

	appendMarkdownHeading(&b, 2, "Project")
	appendMarkdownKeyValue(&b, "Project ID", strings.TrimSpace(document.Status.Project.ProjectID))
	appendMarkdownKeyValue(&b, "Project Root", strings.TrimSpace(document.Status.Project.ProjectRoot))
	appendMarkdownKeyValue(&b, "Detected Repo Root", strings.TrimSpace(document.Status.Project.DetectedRepoRoot))
	appendMarkdownKeyValue(&b, "Backend", strings.TrimSpace(document.Status.Project.Backend))
	appendMarkdownKeyValue(&b, "Postgres Configured", markdownBool(document.Status.Project.PostgresConfigured))
	appendMarkdownKeyValue(&b, "SQLite Path", strings.TrimSpace(document.Status.Project.SQLitePath))
	appendMarkdownKeyValue(&b, "Uses Implicit SQLite Path", markdownBool(document.Status.Project.UsesImplicitSQLitePath))
	appendMarkdownKeyValue(&b, "Unbounded", markdownBool(document.Status.Project.Unbounded))

	if len(document.Status.Sources) > 0 {
		appendMarkdownHeading(&b, 2, "Sources")
		for _, source := range document.Status.Sources {
			appendMarkdownHeading(&b, 3, firstNonEmpty(strings.TrimSpace(source.Kind), strings.TrimSpace(source.SourcePath)))
			appendMarkdownKeyValue(&b, "Source Path", strings.TrimSpace(source.SourcePath))
			appendMarkdownKeyValue(&b, "Absolute Path", strings.TrimSpace(source.AbsolutePath))
			appendMarkdownKeyValue(&b, "Exists", markdownBool(source.Exists))
			appendMarkdownKeyValue(&b, "Loaded", markdownBool(source.Loaded))
			appendMarkdownKeyValue(&b, "Item Count", markdownInt(source.ItemCount))
			appendMarkdownStringListSection(&b, 4, "Notes", source.Notes, false)
		}
	}

	if len(document.Status.Integrations) > 0 {
		appendMarkdownHeading(&b, 2, "Integrations")
		for _, integration := range document.Status.Integrations {
			appendMarkdownHeading(&b, 3, firstNonEmpty(strings.TrimSpace(integration.ID), strings.TrimSpace(integration.Summary)))
			appendMarkdownKeyValue(&b, "Summary", strings.TrimSpace(integration.Summary))
			appendMarkdownKeyValue(&b, "Installed", markdownBool(integration.Installed))
			appendMarkdownKeyValue(&b, "Present Targets", markdownInt(integration.PresentTargets))
			appendMarkdownKeyValue(&b, "Expected Targets", markdownInt(integration.ExpectedTargets))
			appendMarkdownStringListSection(&b, 4, "Missing Targets", integration.MissingTargets, false)
		}
	}

	if document.Status.Context != nil {
		appendMarkdownHeading(&b, 2, "Context Preview")
		appendMarkdownKeyValue(&b, "Task", strings.TrimSpace(document.Status.Context.TaskText))
		appendMarkdownKeyValue(&b, "Phase", strings.TrimSpace(string(document.Status.Context.Phase)))
		appendMarkdownKeyValue(&b, "Status", strings.TrimSpace(document.Status.Context.Status))
		appendMarkdownStringListInline(&b, "Resolved Tags", document.Status.Context.ResolvedTags)
		appendMarkdownKeyValue(&b, "Rule Count", markdownInt(document.Status.Context.RuleCount))
		appendMarkdownKeyValue(&b, "Plan Count", markdownInt(document.Status.Context.PlanCount))
		appendMarkdownKeyValue(&b, "Initial Scope Paths", markdownInt(document.Status.Context.InitialScopePathCount))
		appendMarkdownKeyValue(&b, "Error", strings.TrimSpace(document.Status.Context.Error))
	}

	appendStatusMissingSection(&b, "Missing", document.Status.Missing)
	appendStatusMissingSection(&b, "Warnings", document.Status.Warnings)

	return strings.TrimSpace(b.String())
}

func renderBundleMarkdown(document *v1.ExportDocument) string {
	var b strings.Builder
	appendMarkdownHeading(&b, 1, firstNonEmpty(document.Title, "Fetch bundle"))
	appendMarkdownSummary(&b, document.Summary)
	if document.Bundle == nil {
		return strings.TrimSpace(b.String())
	}

	appendMarkdownStringListInline(&b, "Requested Keys", document.Bundle.RequestedKeys)
	appendMarkdownKeyValue(&b, "Item Count", markdownInt(len(document.Bundle.Items)))
	appendMarkdownStringListInline(&b, "Not Found", document.Bundle.NotFound)
	if len(document.Bundle.VersionMismatches) > 0 {
		values := make([]string, 0, len(document.Bundle.VersionMismatches))
		for _, mismatch := range document.Bundle.VersionMismatches {
			values = append(values, fmt.Sprintf("%s expected=%s actual=%s", strings.TrimSpace(mismatch.Key), strings.TrimSpace(mismatch.Expected), strings.TrimSpace(mismatch.Actual)))
		}
		appendMarkdownStringListSection(&b, 2, "Version Mismatches", values, false)
	}

	if len(document.Bundle.Items) > 0 {
		appendMarkdownHeading(&b, 2, "Items")
		for _, item := range document.Bundle.Items {
			appendMarkdownHeading(&b, 3, firstNonEmpty(strings.TrimSpace(item.Summary), strings.TrimSpace(item.Key)))
			appendMarkdownKeyValue(&b, "Key", strings.TrimSpace(item.Key))
			appendMarkdownKeyValue(&b, "Type", strings.TrimSpace(item.Type))
			appendMarkdownKeyValue(&b, "Kind", strings.TrimSpace(string(item.Kind)))
			appendMarkdownKeyValue(&b, "Status", strings.TrimSpace(item.Status))
			appendMarkdownKeyValue(&b, "Version", strings.TrimSpace(item.Version))
			switch item.Kind {
			case v1.ExportBundleItemKindPlan:
				if item.Plan != nil {
					appendMarkdownKeyValue(&b, "Plan", firstNonEmpty(item.Plan.Title, item.Plan.PlanKey))
					appendMarkdownKeyValue(&b, "Plan Status", strings.TrimSpace(string(item.Plan.Status)))
				}
			case v1.ExportBundleItemKindReceipt:
				if item.Receipt != nil {
					appendMarkdownKeyValue(&b, "Receipt", strings.TrimSpace(item.Receipt.ReceiptID))
					appendMarkdownKeyValue(&b, "Task", strings.TrimSpace(item.Receipt.TaskText))
				}
			case v1.ExportBundleItemKindTask:
				if item.Task != nil {
					appendMarkdownKeyValue(&b, "Task Key", strings.TrimSpace(item.Task.Key))
					appendMarkdownKeyValue(&b, "Task Status", strings.TrimSpace(string(item.Task.Status)))
				}
			case v1.ExportBundleItemKindRun:
				if item.Run != nil {
					appendMarkdownKeyValue(&b, "Run ID", markdownInt64(item.Run.RunID))
					appendMarkdownKeyValue(&b, "Run Status", strings.TrimSpace(item.Run.Status))
				}
			default:
				appendMarkdownListItem(&b, "Markdown omits raw pointer or rule file content in v1.")
			}
			b.WriteString("\n")
		}
	}

	return strings.TrimSpace(b.String())
}

func appendPlanMetadata(b *strings.Builder, document *v1.ExportPlanDocument) {
	appendMarkdownKeyValue(b, "Plan Key", strings.TrimSpace(document.PlanKey))
	appendMarkdownKeyValue(b, "Receipt ID", strings.TrimSpace(document.ReceiptID))
	appendMarkdownKeyValue(b, "Status", strings.TrimSpace(string(document.Status)))
	appendMarkdownKeyValue(b, "Title", strings.TrimSpace(document.Title))
	appendMarkdownKeyValue(b, "Objective", strings.TrimSpace(document.Objective))
	appendMarkdownKeyValue(b, "Kind", strings.TrimSpace(document.Kind))
	appendMarkdownKeyValue(b, "Parent Plan", strings.TrimSpace(document.ParentPlanKey))
}

func appendPlanStages(b *strings.Builder, stages *v1.ExportPlanStages) {
	if stages == nil {
		return
	}
	values := make([]string, 0, 3)
	if strings.TrimSpace(string(stages.SpecOutline)) != "" {
		values = append(values, "spec_outline="+strings.TrimSpace(string(stages.SpecOutline)))
	}
	if strings.TrimSpace(string(stages.RefinedSpec)) != "" {
		values = append(values, "refined_spec="+strings.TrimSpace(string(stages.RefinedSpec)))
	}
	if strings.TrimSpace(string(stages.ImplementationPlan)) != "" {
		values = append(values, "implementation_plan="+strings.TrimSpace(string(stages.ImplementationPlan)))
	}
	appendMarkdownStringListSection(b, 2, "Stages", values, false)
}

func appendPlanTasks(b *strings.Builder, tasks []v1.ExportTaskDocument) {
	appendTaskSection(b, 2, "Tasks", tasks)
}

func appendTaskSection(b *strings.Builder, headingLevel int, title string, tasks []v1.ExportTaskDocument) {
	if len(tasks) == 0 {
		return
	}
	appendMarkdownHeading(b, headingLevel, title)
	for _, task := range tasks {
		appendMarkdownHeading(b, headingLevel+1, firstNonEmpty(strings.TrimSpace(task.Summary), strings.TrimSpace(task.Key)))
		appendTaskDetails(b, headingLevel+2, &task, true)
	}
}

func appendTaskDetails(b *strings.Builder, headingLevel int, task *v1.ExportTaskDocument, includeSections bool) {
	if task == nil {
		return
	}
	appendMarkdownKeyValue(b, "Task Key", strings.TrimSpace(task.Key))
	appendMarkdownKeyValue(b, "Plan Key", strings.TrimSpace(task.PlanKey))
	appendMarkdownKeyValue(b, "Status", strings.TrimSpace(string(task.Status)))
	appendMarkdownKeyValue(b, "Summary", strings.TrimSpace(task.Summary))
	appendMarkdownKeyValue(b, "Parent Task", strings.TrimSpace(task.ParentTaskKey))
	appendMarkdownKeyValue(b, "Blocked Reason", strings.TrimSpace(task.BlockedReason))
	appendMarkdownKeyValue(b, "Outcome", strings.TrimSpace(task.Outcome))
	if includeSections {
		appendMarkdownStringListSection(b, headingLevel, "Depends On", task.DependsOn, false)
		appendMarkdownStringListSection(b, headingLevel, "Acceptance Criteria", task.AcceptanceCriteria, false)
		appendMarkdownStringListSection(b, headingLevel, "References", task.References, true)
		appendMarkdownStringListSection(b, headingLevel, "External References", task.ExternalRefs, false)
		appendMarkdownStringListSection(b, headingLevel, "Evidence", task.Evidence, false)
	}
}

func appendMarkdownRuleSection(b *strings.Builder, rules []v1.ContextRule) {
	if len(rules) == 0 {
		return
	}
	appendMarkdownHeading(b, 2, "Rules")
	for _, rule := range rules {
		line := fmt.Sprintf("`%s` [%s] %s", strings.TrimSpace(rule.RuleID), strings.TrimSpace(rule.Enforcement), strings.TrimSpace(rule.Summary))
		if content := strings.TrimSpace(rule.Content); content != "" {
			line = fmt.Sprintf("%s. %s", line, content)
		}
		appendMarkdownListItem(b, line)
	}
	b.WriteString("\n")
}

func appendMarkdownContextPlanSection(b *strings.Builder, plans []v1.ContextPlan) {
	if len(plans) == 0 {
		return
	}
	appendMarkdownHeading(b, 2, "Plans")
	for _, plan := range plans {
		line := fmt.Sprintf("`%s` [%s] %s", strings.TrimSpace(plan.Key), strings.TrimSpace(string(plan.Status)), strings.TrimSpace(plan.Summary))
		if len(plan.FetchKeys) > 0 {
			line = fmt.Sprintf("%s (fetch: %s)", line, strings.Join(wrapValuesWithBackticks(plan.FetchKeys), ", "))
		}
		appendMarkdownListItem(b, line)
	}
	b.WriteString("\n")
}

func appendStatusMissingSection(b *strings.Builder, title string, items []v1.StatusMissingItem) {
	if len(items) == 0 {
		return
	}
	appendMarkdownHeading(b, 2, title)
	for _, item := range items {
		appendMarkdownListItem(b, fmt.Sprintf("`%s` %s", strings.TrimSpace(item.Code), strings.TrimSpace(item.Message)))
	}
	b.WriteString("\n")
}

func appendMarkdownHeading(b *strings.Builder, level int, title string) {
	trimmed := strings.TrimSpace(title)
	if trimmed == "" {
		return
	}
	if b.Len() > 0 && !strings.HasSuffix(b.String(), "\n\n") {
		b.WriteString("\n")
	}
	b.WriteString(strings.Repeat("#", max(level, 1)))
	b.WriteString(" ")
	b.WriteString(trimmed)
	b.WriteString("\n\n")
}

func appendMarkdownSummary(b *strings.Builder, summary string) {
	trimmed := strings.TrimSpace(summary)
	if trimmed == "" {
		return
	}
	b.WriteString(trimmed)
	b.WriteString("\n\n")
}

func appendMarkdownKeyValue(b *strings.Builder, label, value string) {
	trimmed := strings.TrimSpace(value)
	if trimmed == "" {
		return
	}
	appendMarkdownListItem(b, fmt.Sprintf("%s: %s", strings.TrimSpace(label), trimmed))
}

func appendMarkdownStringListInline(b *strings.Builder, label string, values []string) {
	normalized := normalizeValues(values)
	if len(normalized) == 0 {
		return
	}
	appendMarkdownKeyValue(b, label, strings.Join(wrapValuesWithBackticks(normalized), ", "))
}

func appendMarkdownStringListSection(b *strings.Builder, headingLevel int, title string, values []string, code bool) {
	normalized := normalizeValues(values)
	if len(normalized) == 0 {
		return
	}
	appendMarkdownHeading(b, headingLevel, title)
	for _, value := range normalized {
		item := value
		if code {
			item = fmt.Sprintf("`%s`", value)
		}
		appendMarkdownListItem(b, item)
	}
	b.WriteString("\n")
}

func appendMarkdownListItem(b *strings.Builder, value string) {
	trimmed := strings.TrimSpace(value)
	if trimmed == "" {
		return
	}
	b.WriteString("- ")
	b.WriteString(trimmed)
	b.WriteString("\n")
}

func wrapValuesWithBackticks(values []string) []string {
	if len(values) == 0 {
		return nil
	}
	out := make([]string, 0, len(values))
	for _, value := range values {
		trimmed := strings.TrimSpace(value)
		if trimmed == "" {
			continue
		}
		out = append(out, fmt.Sprintf("`%s`", trimmed))
	}
	return out
}

func markdownBool(value bool) string {
	if value {
		return "true"
	}
	return "false"
}

func markdownInt(value int) string {
	if value <= 0 {
		return ""
	}
	return strconv.Itoa(value)
}

func markdownInt64(value int64) string {
	if value <= 0 {
		return ""
	}
	return strconv.FormatInt(value, 10)
}

func cloneStringMap(input map[string]string) map[string]string {
	if len(input) == 0 {
		return nil
	}
	out := make(map[string]string, len(input))
	maps.Copy(out, input)
	return out
}
