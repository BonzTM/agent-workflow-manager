package buildinfo

import (
	"runtime/debug"
	"testing"
)

func TestVersion_UsesInjectedVersionOverCommitShort(t *testing.T) {
	previousVersion := version
	previousCommit := commitShort
	previousReader := readBuildInfo
	t.Cleanup(func() {
		version = previousVersion
		commitShort = previousCommit
		readBuildInfo = previousReader
	})

	version = "v1.2.3"
	commitShort = "abc1234"
	readBuildInfo = func() (*debug.BuildInfo, bool) {
		t.Fatal("readBuildInfo should not be called when version is injected")
		return nil, false
	}

	if got := Version(); got != "v1.2.3" {
		t.Fatalf("unexpected version: got %q want %q", got, "v1.2.3")
	}
}

func TestVersion_UsesInjectedCommitShortWhenPresent(t *testing.T) {
	previousVersion := version
	previousCommit := commitShort
	previousReader := readBuildInfo
	t.Cleanup(func() {
		version = previousVersion
		commitShort = previousCommit
		readBuildInfo = previousReader
	})

	version = ""
	commitShort = "abc1234"
	readBuildInfo = func() (*debug.BuildInfo, bool) {
		t.Fatal("readBuildInfo should not be called when commitShort is injected")
		return nil, false
	}

	if got := Version(); got != "abc1234" {
		t.Fatalf("unexpected version: got %q want %q", got, "abc1234")
	}
}

func TestVersion_UsesShortVCSRevisionAndDirtySuffix(t *testing.T) {
	previousVersion := version
	previousCommit := commitShort
	previousReader := readBuildInfo
	t.Cleanup(func() {
		version = previousVersion
		commitShort = previousCommit
		readBuildInfo = previousReader
	})

	version = ""
	commitShort = ""
	readBuildInfo = func() (*debug.BuildInfo, bool) {
		return &debug.BuildInfo{
			Main: debug.Module{Path: "github.com/bonztm/agent-workflow-manager", Version: "(devel)"},
			Settings: []debug.BuildSetting{
				{Key: "vcs.revision", Value: "0123456789abcdef"},
				{Key: "vcs.modified", Value: "true"},
			},
		}, true
	}

	if got := Version(); got != "0123456-dirty" {
		t.Fatalf("unexpected version: got %q want %q", got, "0123456-dirty")
	}
}

func TestVersion_UsesPseudoVersionCommitWhenRevisionMissing(t *testing.T) {
	previousVersion := version
	previousCommit := commitShort
	previousReader := readBuildInfo
	t.Cleanup(func() {
		version = previousVersion
		commitShort = previousCommit
		readBuildInfo = previousReader
	})

	version = ""
	commitShort = ""
	readBuildInfo = func() (*debug.BuildInfo, bool) {
		return &debug.BuildInfo{
			Main: debug.Module{
				Path:    "github.com/bonztm/agent-workflow-manager",
				Version: "v0.0.0-20260305211823-89abcdef0123",
			},
		}, true
	}

	if got := Version(); got != "89abcde" {
		t.Fatalf("unexpected version: got %q want %q", got, "89abcde")
	}
}

func TestVersion_FallsBackToDev(t *testing.T) {
	previousVersion := version
	previousCommit := commitShort
	previousReader := readBuildInfo
	t.Cleanup(func() {
		version = previousVersion
		commitShort = previousCommit
		readBuildInfo = previousReader
	})

	version = ""
	commitShort = ""
	readBuildInfo = func() (*debug.BuildInfo, bool) {
		return nil, false
	}

	if got := Version(); got != "dev" {
		t.Fatalf("unexpected version: got %q want %q", got, "dev")
	}
}

func TestBanner(t *testing.T) {
	previousVersion := version
	previousCommit := commitShort
	previousReader := readBuildInfo
	t.Cleanup(func() {
		version = previousVersion
		commitShort = previousCommit
		readBuildInfo = previousReader
	})

	version = ""
	commitShort = "abc1234"
	readBuildInfo = func() (*debug.BuildInfo, bool) {
		return nil, false
	}

	if got := Banner("awm"); got != "awm abc1234" {
		t.Fatalf("unexpected banner: got %q want %q", got, "awm abc1234")
	}
}
