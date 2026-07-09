package bootstrap

import (
	"io/fs"
	"os"
	"path/filepath"
	"sort"
	"strings"
	"testing"
)

func TestResolveTemplates_DeprecatedAliasResolvesToCanonical(t *testing.T) {
	t.Parallel()

	templates, err := ResolveTemplates([]string{"claude-receipt-guard"})
	if err != nil {
		t.Fatalf("expected deprecated alias to resolve, got error: %v", err)
	}
	if len(templates) != 1 || templates[0].ID != "claude-hooks" {
		t.Fatalf("expected alias to resolve to claude-hooks, got %+v", templates)
	}
}

func TestResolveTemplates_AliasDeduplicatesAgainstCanonical(t *testing.T) {
	t.Parallel()

	templates, err := ResolveTemplates([]string{"claude-hooks", "claude-receipt-guard"})
	if err != nil {
		t.Fatalf("expected alias plus canonical to resolve, got error: %v", err)
	}
	if len(templates) != 1 || templates[0].ID != "claude-hooks" {
		t.Fatalf("expected one deduplicated claude-hooks template, got %+v", templates)
	}
}

func TestTemplateIDsMatchEmbeddedManifests(t *testing.T) {
	t.Parallel()

	manifestPaths, err := fs.Glob(initTemplateFS, "bootstrap_templates/*/template.yaml")
	if err != nil {
		t.Fatalf("glob manifests: %v", err)
	}
	want := make([]string, 0, len(manifestPaths))
	for _, manifestPath := range manifestPaths {
		want = append(want, filepath.Base(filepath.Dir(manifestPath)))
	}
	sort.Strings(want)

	got, err := TemplateIDs()
	if err != nil {
		t.Fatalf("template ids: %v", err)
	}
	if len(got) != len(want) {
		t.Fatalf("unexpected template id count: got %v want %v", got, want)
	}
	for i := range want {
		if got[i] != want[i] {
			t.Fatalf("unexpected template ids: got %v want %v", got, want)
		}
	}
}

func TestDetailedPlanningEnforcementUpgradesLanguageVerifyProfiles(t *testing.T) {
	t.Parallel()

	for _, profile := range []string{"verify-generic", "verify-go", "verify-ts", "verify-python", "verify-rust"} {
		root := t.TempDir()
		if err := EnsureProjectScaffold(root, ""); err != nil {
			t.Fatalf("%s: ensure scaffold: %v", profile, err)
		}

		first, err := ResolveTemplates([]string{profile})
		if err != nil {
			t.Fatalf("%s: resolve profile: %v", profile, err)
		}
		if _, applyErr := ApplyTemplates(root, "project.alpha", first); applyErr != nil {
			t.Fatalf("%s: apply profile: %v", profile, applyErr)
		}

		second, err := ResolveTemplates([]string{"detailed-planning-enforcement"})
		if err != nil {
			t.Fatalf("%s: resolve detailed-planning-enforcement: %v", profile, err)
		}
		result, err := ApplyTemplates(root, "project.alpha", second)
		if err != nil {
			t.Fatalf("%s: apply detailed-planning-enforcement: %v", profile, err)
		}

		raw, err := os.ReadFile(filepath.Join(root, ".awm", "awm-tests.yaml"))
		if err != nil {
			t.Fatalf("%s: read seeded tests file: %v", profile, err)
		}
		if !strings.Contains(string(raw), "awm-feature-plan-validate") {
			t.Fatalf("%s: expected detailed-planning-enforcement to upgrade the pristine %s tests file, got conflicts %+v and content:\n%s",
				profile, profile, result.TemplateResults, string(raw))
		}
	}
}
