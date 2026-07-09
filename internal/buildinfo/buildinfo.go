package buildinfo

import (
	"runtime/debug"
	"strings"
)

var (
	version     string
	commitShort string
)

var readBuildInfo = debug.ReadBuildInfo

// Version reports the version string displayed to users. It prefers the
// injected version, then the injected short commit, then VCS build metadata
// (short revision plus a "-dirty" suffix when modified), then the module
// version, and finally "dev". Displayed versions never carry a leading "v";
// that prefix exists only on git tags and is stripped here.
func Version() string {
	if v := strings.TrimSpace(version); v != "" {
		return strings.TrimPrefix(v, "v")
	}
	if injected := strings.TrimSpace(commitShort); injected != "" {
		return injected
	}

	info, ok := readBuildInfo()
	if ok {
		if revision := vcsRevision(info); revision != "" {
			if vcsModified(info) {
				return revision + "-dirty"
			}
			return revision
		}
		if modVersion := moduleVersion(info); modVersion != "" {
			return strings.TrimPrefix(modVersion, "v")
		}
	}

	return "dev"
}

// Banner formats a one-line version banner as "<binaryName> <version>", or
// just the version when binaryName is blank.
func Banner(binaryName string) string {
	name := strings.TrimSpace(binaryName)
	if name == "" {
		return Version()
	}
	return name + " " + Version()
}

func vcsRevision(info *debug.BuildInfo) string {
	if info == nil {
		return ""
	}
	for _, setting := range info.Settings {
		if setting.Key != "vcs.revision" {
			continue
		}
		revision := strings.TrimSpace(setting.Value)
		if revision == "" {
			return ""
		}
		if len(revision) > 7 {
			return revision[:7]
		}
		return revision
	}
	return ""
}

func vcsModified(info *debug.BuildInfo) bool {
	if info == nil {
		return false
	}
	for _, setting := range info.Settings {
		if setting.Key == "vcs.modified" && strings.EqualFold(strings.TrimSpace(setting.Value), "true") {
			return true
		}
	}
	return false
}

func moduleVersion(info *debug.BuildInfo) string {
	if info == nil {
		return ""
	}

	mainVersion := strings.TrimSpace(info.Main.Version)
	if mainVersion == "" || mainVersion == "(devel)" {
		return ""
	}

	if pseudo := pseudoVersionCommit(mainVersion); pseudo != "" {
		return pseudo
	}
	return mainVersion
}

func pseudoVersionCommit(version string) string {
	trimmed := strings.TrimSpace(version)
	if trimmed == "" {
		return ""
	}

	lastDash := strings.LastIndex(trimmed, "-")
	if lastDash <= 0 || lastDash == len(trimmed)-1 {
		return ""
	}
	suffix := trimmed[lastDash+1:]
	if len(suffix) < 7 || !isLowerHex(suffix) {
		return ""
	}
	if len(suffix) > 7 {
		return suffix[:7]
	}
	return suffix
}

func isLowerHex(value string) bool {
	if strings.TrimSpace(value) == "" {
		return false
	}
	for _, r := range value {
		if (r < '0' || r > '9') && (r < 'a' || r > 'f') {
			return false
		}
	}
	return true
}
