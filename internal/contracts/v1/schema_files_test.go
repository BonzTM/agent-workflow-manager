package v1

import (
	"encoding/json"
	"os"
	"path/filepath"
	"reflect"
	"slices"
	"strings"
	"testing"
)

func TestSharedSchemaCommandEnumMatchesCommandCatalog(t *testing.T) {
	raw := readSchemaFixture(t, "shared.schema.json")

	var doc struct {
		Defs struct {
			CommandName struct {
				Enum []string `json:"enum"`
			} `json:"commandName"`
		} `json:"$defs"`
	}
	if err := json.Unmarshal(raw, &doc); err != nil {
		t.Fatalf("unmarshal shared schema: %v", err)
	}

	if got, want := doc.Defs.CommandName.Enum, CommandNames(); !reflect.DeepEqual(got, want) {
		t.Fatalf("command enum drift: got %v want %v", got, want)
	}
}

func TestCommandAndResultSchemasParseAndRejectRemovedLegacyNames(t *testing.T) {
	for _, filename := range []string{"cli.command.schema.json", "cli.result.schema.json"} {
		t.Run(filename, func(t *testing.T) {
			raw := readSchemaFixture(t, filename)

			var doc map[string]any
			if err := json.Unmarshal(raw, &doc); err != nil {
				t.Fatalf("unmarshal %s: %v", filename, err)
			}

			for _, removed := range []string{`"get_context"`, `"report_completion"`, `"history_search"`, `"bootstrapTemplateResult"`, `"bootstrapTemplateConflict"`} {
				if strings.Contains(string(raw), removed) {
					t.Fatalf("%s still contains removed legacy command %s", filename, removed)
				}
			}
		})
	}
}

func TestCommandSchema_DoneRequiresReceiptOrPlanSelection(t *testing.T) {
	raw := readSchemaFixture(t, "cli.command.schema.json")

	var doc struct {
		Defs map[string]map[string]any `json:"$defs"`
	}
	if err := json.Unmarshal(raw, &doc); err != nil {
		t.Fatalf("unmarshal cli command schema: %v", err)
	}

	assertReceiptOrPlanSelection := func(defName string) {
		t.Helper()

		def, ok := doc.Defs[defName]
		if !ok {
			t.Fatalf("missing schema def %q", defName)
		}
		properties, ok := def["properties"].(map[string]any)
		if !ok {
			t.Fatalf("%s.properties missing or invalid", defName)
		}
		if _, exists := properties["receipt_id"]; !exists {
			t.Fatalf("%s missing receipt_id property", defName)
		}
		if _, exists := properties["plan_key"]; !exists {
			t.Fatalf("%s missing plan_key property", defName)
		}

		if required, isList := def["required"].([]any); isList {
			for _, value := range required {
				if value == "receipt_id" {
					t.Fatalf("%s should not require receipt_id directly once plan_key is supported", defName)
				}
			}
		}

		allOf, ok := def["allOf"].([]any)
		if !ok {
			t.Fatalf("%s.allOf missing or invalid", defName)
		}
		foundSelection := false
		for _, rawClause := range allOf {
			clause, ok := rawClause.(map[string]any)
			if !ok {
				continue
			}
			anyOf, ok := clause["anyOf"].([]any)
			if !ok {
				continue
			}
			requiresReceipt := false
			requiresPlan := false
			for _, rawOption := range anyOf {
				option, ok := rawOption.(map[string]any)
				if !ok {
					continue
				}
				requiredList, ok := option["required"].([]any)
				if !ok || len(requiredList) != 1 {
					continue
				}
				switch requiredList[0] {
				case "receipt_id":
					requiresReceipt = true
				case "plan_key":
					requiresPlan = true
				}
			}
			if requiresReceipt && requiresPlan {
				foundSelection = true
				break
			}
		}
		if !foundSelection {
			t.Fatalf("%s missing receipt_id|plan_key selection guard", defName)
		}
	}

	assertReceiptOrPlanSelection("donePayload")
}

func TestSharedAndResultSchemasExposeSupersededStatusesAndStatusWarnings(t *testing.T) {
	sharedRaw := readSchemaFixture(t, "shared.schema.json")
	var sharedDoc struct {
		Defs struct {
			WorkItemStatus struct {
				Enum []string `json:"enum"`
			} `json:"workItemStatus"`
		} `json:"$defs"`
	}
	if err := json.Unmarshal(sharedRaw, &sharedDoc); err != nil {
		t.Fatalf("unmarshal shared schema: %v", err)
	}
	if !reflect.DeepEqual(sharedDoc.Defs.WorkItemStatus.Enum, []string{"pending", "in_progress", "complete", "blocked", "superseded"}) {
		t.Fatalf("unexpected work item status enum: %v", sharedDoc.Defs.WorkItemStatus.Enum)
	}

	resultRaw := readSchemaFixture(t, "cli.result.schema.json")
	var resultDoc struct {
		Defs map[string]map[string]any `json:"$defs"`
	}
	if err := json.Unmarshal(resultRaw, &resultDoc); err != nil {
		t.Fatalf("unmarshal cli result schema: %v", err)
	}

	statusSummary, ok := resultDoc.Defs["statusSummary"]
	if !ok {
		t.Fatal("missing statusSummary schema")
	}
	properties, ok := statusSummary["properties"].(map[string]any)
	if !ok {
		t.Fatal("statusSummary.properties missing or invalid")
	}
	if _, exists := properties["warning_count"]; !exists {
		t.Fatal("statusSummary missing warning_count property")
	}

	statusResult, ok := resultDoc.Defs["statusResult"]
	if !ok {
		t.Fatal("missing statusResult schema")
	}
	resultProperties, ok := statusResult["properties"].(map[string]any)
	if !ok {
		t.Fatal("statusResult.properties missing or invalid")
	}
	if _, ok := resultProperties["warnings"]; !ok {
		t.Fatal("statusResult missing warnings property")
	}

	for _, defName := range []string{"reviewResult", "workResult"} {
		def, ok := resultDoc.Defs[defName]
		if !ok {
			t.Fatalf("missing %s schema", defName)
		}
		properties, ok := def["properties"].(map[string]any)
		if !ok {
			t.Fatalf("%s.properties missing or invalid", defName)
		}
		planStatus, ok := properties["plan_status"].(map[string]any)
		if !ok {
			t.Fatalf("%s.plan_status missing or invalid", defName)
		}
		enumValues, ok := planStatus["enum"].([]any)
		if !ok {
			t.Fatalf("%s.plan_status enum missing or invalid", defName)
		}
		values := make([]string, 0, len(enumValues))
		for _, rawValue := range enumValues {
			value, isString := rawValue.(string)
			if !isString {
				t.Fatalf("%s.plan_status enum contains non-string value %v", defName, rawValue)
			}
			values = append(values, value)
		}
		if !containsString(values, "superseded") {
			t.Fatalf("%s.plan_status enum missing superseded: %v", defName, values)
		}
	}
}

func TestCommandSchema_ExportPayloadIncludesSelectorAndFormatGuards(t *testing.T) {
	raw := readSchemaFixture(t, "cli.command.schema.json")

	var doc struct {
		Defs map[string]map[string]any `json:"$defs"`
	}
	if err := json.Unmarshal(raw, &doc); err != nil {
		t.Fatalf("unmarshal cli command schema: %v", err)
	}

	exportFormat, ok := doc.Defs["exportFormat"]
	if !ok {
		t.Fatal("missing exportFormat schema")
	}
	formatEnum := stringSliceFromAny(exportFormat["enum"])
	if !reflect.DeepEqual(formatEnum, []string{"json", "markdown"}) {
		t.Fatalf("unexpected exportFormat enum: %v", formatEnum)
	}

	exportPayload, ok := doc.Defs["exportPayload"]
	if !ok {
		t.Fatal("missing exportPayload schema")
	}
	required := stringSliceFromAny(exportPayload["required"])
	if !reflect.DeepEqual(required, []string{"format"}) {
		t.Fatalf("unexpected exportPayload required fields: %v", required)
	}
	properties, ok := exportPayload["properties"].(map[string]any)
	if !ok {
		t.Fatal("exportPayload.properties missing or invalid")
	}
	for _, selector := range []string{"context", "fetch", "history", "status"} {
		if _, exists := properties[selector]; !exists {
			t.Fatalf("exportPayload missing %s selector", selector)
		}
	}
	formatProperty, ok := properties["format"].(map[string]any)
	if !ok {
		t.Fatal("exportPayload.format missing or invalid")
	}
	if got := formatProperty["$ref"]; got != "#/$defs/exportFormat" {
		t.Fatalf("unexpected exportPayload.format ref: %v", got)
	}

	oneOf, ok := exportPayload["oneOf"].([]any)
	if !ok || len(oneOf) != 4 {
		t.Fatal("exportPayload.oneOf missing or invalid")
	}
	requiredFields := make([]string, 0, len(oneOf))
	for _, rawOption := range oneOf {
		option, ok := rawOption.(map[string]any)
		if !ok {
			continue
		}
		requiredList := stringSliceFromAny(option["required"])
		if len(requiredList) == 1 {
			requiredFields = append(requiredFields, requiredList[0])
		}
	}
	if !reflect.DeepEqual(requiredFields, []string{"context", "fetch", "history", "status"}) {
		t.Fatal("exportPayload missing exactly-one selector guard")
	}
}

func TestResultSchema_ExportDefinitionsMatchRuntimeEnums(t *testing.T) {
	raw := readSchemaFixture(t, "cli.result.schema.json")

	var doc struct {
		Defs map[string]map[string]any `json:"$defs"`
	}
	if err := json.Unmarshal(raw, &doc); err != nil {
		t.Fatalf("unmarshal cli result schema: %v", err)
	}

	exportDocumentKind, ok := doc.Defs["exportDocumentKind"]
	if !ok {
		t.Fatal("missing exportDocumentKind schema")
	}
	if got, want := stringSliceFromAny(exportDocumentKind["enum"]), []string{
		string(ExportDocumentKindContext),
		string(ExportDocumentKindPlan),
		string(ExportDocumentKindReceipt),
		string(ExportDocumentKindTask),
		string(ExportDocumentKindRun),
		string(ExportDocumentKindFetchBundle),
		string(ExportDocumentKindHistory),
		string(ExportDocumentKindStatus),
	}; !reflect.DeepEqual(got, want) {
		t.Fatalf("unexpected exportDocumentKind enum: got %v want %v", got, want)
	}

	exportBundleItemKind, ok := doc.Defs["exportBundleItemKind"]
	if !ok {
		t.Fatal("missing exportBundleItemKind schema")
	}
	if got, want := stringSliceFromAny(exportBundleItemKind["enum"]), []string{
		string(ExportBundleItemKindPlan),
		string(ExportBundleItemKindReceipt),
		string(ExportBundleItemKindTask),
		string(ExportBundleItemKindRun),
		string(ExportBundleItemKindPointer),
		string(ExportBundleItemKindRule),
	}; !reflect.DeepEqual(got, want) {
		t.Fatalf("unexpected exportBundleItemKind enum: got %v want %v", got, want)
	}

	exportResult, ok := doc.Defs["exportResult"]
	if !ok {
		t.Fatal("missing exportResult schema")
	}
	properties, ok := exportResult["properties"].(map[string]any)
	if !ok {
		t.Fatal("exportResult.properties missing or invalid")
	}
	for _, property := range []string{"format", "document", "content"} {
		if _, exists := properties[property]; !exists {
			t.Fatalf("exportResult missing %s property", property)
		}
	}
	documentProperty, ok := properties["document"].(map[string]any)
	if !ok {
		t.Fatal("exportResult.document missing or invalid")
	}
	if got := documentProperty["$ref"]; got != "#/$defs/exportDocument" {
		t.Fatalf("unexpected exportResult.document ref: %v", got)
	}
}

func TestSharedAndResultSchemasRejectMemoryResidueInCurrentSurfaces(t *testing.T) {
	sharedRaw := string(readSchemaFixture(t, "shared.schema.json"))
	resultRaw := string(readSchemaFixture(t, "cli.result.schema.json"))
	commandRaw := string(readSchemaFixture(t, "cli.command.schema.json"))

	for _, forbidden := range []string{"\"memoryCategory\"", "\"memorySummary\"", "\"contextMemory\"", "\"memoryPayload\"", "\"memories\""} {
		if strings.Contains(sharedRaw, forbidden) {
			t.Fatalf("shared.schema.json still contains memory residue %s", forbidden)
		}
	}

	for _, forbidden := range []string{"\"exportMemoryDocument\"", "\"memory_count\"", "\"memory_keys\"", "shared.schema.json#/$defs/memoryCategory"} {
		if strings.Contains(resultRaw, forbidden) {
			t.Fatalf("cli.result.schema.json still contains memory residue %s", forbidden)
		}
	}

	if strings.Contains(commandRaw, "\"memory\"") {
		t.Fatal("cli.command.schema.json still exposes memory command residue")
	}
}

func containsString(values []string, want string) bool {
	return slices.Contains(values, want)
}

func stringSliceFromAny(raw any) []string {
	items, isList := raw.([]any)
	if !isList {
		return []string{}
	}
	values := make([]string, 0, len(items))
	for _, item := range items {
		if value, ok := item.(string); ok {
			values = append(values, value)
		}
	}
	return values
}

func readSchemaFixture(t *testing.T, name string) []byte {
	t.Helper()

	path := filepath.Join("..", "..", "..", "spec", "v1", name)
	raw, err := os.ReadFile(path)
	if err != nil {
		t.Fatalf("read %s: %v", name, err)
	}
	return raw
}
