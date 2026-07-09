package workspace

import (
	"bufio"
	"bytes"
	"os"
	"path/filepath"
	"strconv"
	"strings"
)

const (
	// DefaultSQLiteRelativePath is the repo-relative location of the SQLite
	// database when no explicit path is configured.
	DefaultSQLiteRelativePath = ".awm/context.db"
	// DotEnvFileName is the file name of the per-project env override file.
	DotEnvFileName = ".env"
	// DotEnvExampleFileName is the file name of the scaffolded env template.
	DotEnvExampleFileName = ".env.example"
)

// Root describes a detected workspace root: its absolute path and whether it
// was identified by the presence of a .git entry.
type Root struct {
	Path   string
	IsRepo bool
}

// DetectRoot walks upward from startDir (or the current working directory
// when blank) looking for a .git entry. It returns the containing repository
// root when found, otherwise the cleaned absolute start directory with IsRepo
// set to false.
func DetectRoot(startDir string) Root {
	base := strings.TrimSpace(startDir)
	if base == "" {
		cwd, err := os.Getwd()
		if err != nil {
			return Root{}
		}
		base = cwd
	}

	absBase, err := filepath.Abs(base)
	if err != nil {
		absBase = filepath.Clean(base)
	}
	absBase = filepath.Clean(absBase)

	for current := absBase; ; current = filepath.Dir(current) {
		gitPath := filepath.Join(current, ".git")
		if _, err := os.Stat(gitPath); err == nil {
			return Root{Path: current, IsRepo: true}
		}
		parent := filepath.Dir(current)
		if parent == current {
			return Root{Path: absBase, IsRepo: false}
		}
	}
}

// RelativePathWithinRoot returns targetPath expressed as a slash-normalized
// path relative to rootPath, or "" when either argument is blank or the
// target escapes the root.
func RelativePathWithinRoot(rootPath, targetPath string) string {
	root := strings.TrimSpace(rootPath)
	target := strings.TrimSpace(targetPath)
	if root == "" || target == "" {
		return ""
	}

	cleanRoot := filepath.Clean(root)
	cleanTarget := target
	if !filepath.IsAbs(cleanTarget) {
		cleanTarget = filepath.Join(cleanRoot, cleanTarget)
	}
	cleanTarget = filepath.Clean(cleanTarget)

	relative, err := filepath.Rel(cleanRoot, cleanTarget)
	if err != nil {
		return ""
	}
	normalized := normalizeRelativePath(relative)
	if normalized == "" || strings.HasPrefix(normalized, "../") {
		return ""
	}
	return normalized
}

// EnsureGitIgnoreContains appends any of the given entries that are missing
// from rootPath's .gitignore, creating the file when needed. Blank rootPath
// is a no-op.
func EnsureGitIgnoreContains(rootPath string, entries ...string) error {
	root := strings.TrimSpace(rootPath)
	if root == "" {
		return nil
	}
	gitignorePath := filepath.Join(root, ".gitignore")
	return appendUniqueLines(gitignorePath, entries)
}

// SQLiteGitIgnoreEntries returns the .gitignore entries covering a SQLite
// database at relativePath plus its -shm and -wal sidecar files, or nil when
// the path is blank.
func SQLiteGitIgnoreEntries(relativePath string) []string {
	normalized := normalizeRelativePath(relativePath)
	if normalized == "" {
		return nil
	}
	return []string{
		normalized,
		normalized + "-shm",
		normalized + "-wal",
	}
}

// ParseDotEnvFile reads the file at path and parses it as dotenv content via
// ParseDotEnv.
func ParseDotEnvFile(path string) (map[string]string, error) {
	blob, err := os.ReadFile(path)
	if err != nil {
		return nil, err
	}
	return ParseDotEnv(blob), nil
}

// ParseDotEnv parses dotenv-style content into a key/value map, skipping
// blank lines and comments, honoring "export " prefixes, and unquoting
// single- or double-quoted values.
func ParseDotEnv(raw []byte) map[string]string {
	parsed := make(map[string]string)
	scanner := bufio.NewScanner(bytes.NewReader(raw))
	for scanner.Scan() {
		line := strings.TrimSpace(scanner.Text())
		if line == "" || strings.HasPrefix(line, "#") {
			continue
		}
		if after, ok := strings.CutPrefix(line, "export "); ok {
			line = strings.TrimSpace(after)
		}

		key, value, ok := parseDotEnvAssignment(line)
		if !ok {
			continue
		}
		parsed[key] = value
	}
	return parsed
}

// LookupEnvValue resolves key first from the process environment (via
// lookupEnv, defaulting to os.LookupEnv) and then from the .env file at the
// workspace root detected from startDir. It returns the trimmed value, or ""
// when the key is unset.
func LookupEnvValue(startDir, key string, lookupEnv func(string) (string, bool)) string {
	trimmedKey := strings.TrimSpace(key)
	if trimmedKey == "" {
		return ""
	}
	if lookupEnv == nil {
		lookupEnv = os.LookupEnv
	}
	if value, ok := lookupEnv(trimmedKey); ok {
		return strings.TrimSpace(value)
	}

	root := DetectRoot(startDir)
	if strings.TrimSpace(root.Path) == "" {
		return ""
	}
	values, err := ParseDotEnvFile(filepath.Join(root.Path, DotEnvFileName))
	if err != nil {
		return ""
	}
	return strings.TrimSpace(values[trimmedKey])
}

// LookupEnvBool resolves key like LookupEnvValue and interprets the value as
// a boolean, returning false when unset or unparsable.
func LookupEnvBool(startDir, key string, lookupEnv func(string) (string, bool)) bool {
	value := LookupEnvValue(startDir, key, lookupEnv)
	if value == "" {
		return false
	}
	parsed, err := strconv.ParseBool(value)
	if err != nil {
		return false
	}
	return parsed
}

func parseDotEnvAssignment(line string) (string, string, bool) {
	index := strings.IndexRune(line, '=')
	if index <= 0 {
		return "", "", false
	}

	key := strings.TrimSpace(line[:index])
	if !validDotEnvKey(key) {
		return "", "", false
	}

	value := strings.TrimSpace(line[index+1:])
	if len(value) >= 2 && value[0] == '"' && value[len(value)-1] == '"' {
		unquoted, err := strconv.Unquote(value)
		if err == nil {
			value = unquoted
		} else {
			value = value[1 : len(value)-1]
		}
	} else if len(value) >= 2 && value[0] == '\'' && value[len(value)-1] == '\'' {
		value = value[1 : len(value)-1]
	}
	return key, value, true
}

func validDotEnvKey(key string) bool {
	if key == "" {
		return false
	}
	for i, r := range key {
		switch {
		case r == '_':
		case r >= 'A' && r <= 'Z':
		case r >= 'a' && r <= 'z':
		case r >= '0' && r <= '9' && i > 0:
		default:
			return false
		}
	}
	first := key[0]
	return first == '_' || (first >= 'A' && first <= 'Z') || (first >= 'a' && first <= 'z')
}

func appendUniqueLines(path string, lines []string) error {
	normalizedLines := normalizeUniqueLines(lines)
	if len(normalizedLines) == 0 {
		return nil
	}

	existingRaw, err := os.ReadFile(path)
	switch {
	case err == nil:
	case os.IsNotExist(err):
		existingRaw = nil
	default:
		return err
	}

	existingSet := make(map[string]struct{})
	existingLines := strings.SplitSeq(string(existingRaw), "\n")
	for line := range existingLines {
		trimmed := strings.TrimSpace(line)
		if trimmed == "" {
			continue
		}
		existingSet[trimmed] = struct{}{}
	}

	missing := make([]string, 0, len(normalizedLines))
	for _, line := range normalizedLines {
		if _, ok := existingSet[line]; ok {
			continue
		}
		missing = append(missing, line)
	}
	if len(missing) == 0 {
		return nil
	}

	if mkdirErr := os.MkdirAll(filepath.Dir(path), 0o755); mkdirErr != nil { //nolint:gosec // G301: repo directory, standard world-readable permissions by design
		return mkdirErr
	}

	file, err := os.OpenFile(path, os.O_CREATE|os.O_RDWR, 0o644) //nolint:gosec // G302: .gitignore is a committed repo file, world-readable by design
	if err != nil {
		return err
	}
	defer file.Close()

	existingRaw, err = os.ReadFile(path)
	if err != nil {
		return err
	}

	var builder strings.Builder
	builder.Write(existingRaw)
	if len(existingRaw) > 0 && existingRaw[len(existingRaw)-1] != '\n' {
		builder.WriteByte('\n')
	}
	for _, line := range missing {
		builder.WriteString(line)
		builder.WriteByte('\n')
	}

	if err := os.WriteFile(path, []byte(builder.String()), 0o644); err != nil { //nolint:gosec // G306: .gitignore is a committed repo file, world-readable by design
		return err
	}
	return nil
}

func normalizeUniqueLines(lines []string) []string {
	seen := make(map[string]struct{}, len(lines))
	out := make([]string, 0, len(lines))
	for _, raw := range lines {
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
	return out
}

func normalizeRelativePath(raw string) string {
	trimmed := strings.TrimSpace(raw)
	if trimmed == "" {
		return ""
	}
	cleaned := filepath.Clean(trimmed)
	if cleaned == "." {
		return ""
	}
	return filepath.ToSlash(cleaned)
}
