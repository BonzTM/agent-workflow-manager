package scripts_test

import (
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"
)

func TestInstallSkillPackInstallsCodexSkillLocally(t *testing.T) {
	t.Parallel()

	codexHome := filepath.Join(t.TempDir(), "codex-home")
	cmd := exec.Command("bash", "scripts/install-skill-pack.sh", "--codex", codexHome)
	cmd.Dir = repoRoot(t)

	output, err := cmd.CombinedOutput()
	if err != nil {
		t.Fatalf("install skill pack: %v\n%s", err, string(output))
	}
	if !strings.Contains(string(output), "Installed Codex skill:") {
		t.Fatalf("expected install output, got %s", string(output))
	}

	raw, err := os.ReadFile(filepath.Join(codexHome, "skills", "awm-broker", "SKILL.md"))
	if err != nil {
		t.Fatalf("read installed skill: %v", err)
	}
	if !strings.Contains(string(raw), "# awm-broker") {
		t.Fatalf("unexpected installed skill content: %s", string(raw))
	}
}

func TestInstallSkillPackPreservesExistingCodexSkillWhenCopyFails(t *testing.T) {
	t.Parallel()

	tempRoot := t.TempDir()
	binDir := filepath.Join(tempRoot, "bin")
	if err := os.MkdirAll(binDir, 0o755); err != nil {
		t.Fatalf("mkdir bin: %v", err)
	}
	writeExecutable(t, filepath.Join(binDir, "cp"), `#!/usr/bin/env bash
set -euo pipefail
echo "cp failed" >&2
exit 23
`)

	codexHome := filepath.Join(tempRoot, "codex-home")
	installedSkill := filepath.Join(codexHome, "skills", "awm-broker")
	if err := os.MkdirAll(installedSkill, 0o755); err != nil {
		t.Fatalf("mkdir installed skill: %v", err)
	}
	if err := os.WriteFile(filepath.Join(installedSkill, "SKILL.md"), []byte("existing skill\n"), 0o644); err != nil {
		t.Fatalf("write existing skill: %v", err)
	}

	cmd := exec.Command("bash", "scripts/install-skill-pack.sh", "--codex", codexHome)
	cmd.Dir = repoRoot(t)
	cmd.Env = append(os.Environ(), "PATH="+binDir+string(os.PathListSeparator)+os.Getenv("PATH"))

	output, err := cmd.CombinedOutput()
	if err == nil {
		t.Fatalf("expected install failure when copy fails, output=%s", string(output))
	}
	if !strings.Contains(string(output), "cp failed") {
		t.Fatalf("expected surfaced copy failure, got %s", string(output))
	}

	raw, err := os.ReadFile(filepath.Join(installedSkill, "SKILL.md"))
	if err != nil {
		t.Fatalf("read preserved skill: %v", err)
	}
	if string(raw) != "existing skill\n" {
		t.Fatalf("expected existing install to remain intact, got %q", string(raw))
	}
}
