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
	DefaultSQLiteRelativePath = ".awm/context.db"
	DotEnvFileName            = ".env"
	DotEnvExampleFileName     = ".env.example"
)

type Root struct {
	Path   string
	IsRepo bool
}

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

func EnsureGitIgnoreContains(rootPath string, entries ...string) error {
	root := strings.TrimSpace(rootPath)
	if root == "" {
		return nil
	}
	gitignorePath := filepath.Join(root, ".gitignore")
	return appendUniqueLines(gitignorePath, entries)
}

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

func ParseDotEnvFile(path string) (map[string]string, error) {
	blob, err := os.ReadFile(path)
	if err != nil {
		return nil, err
	}
	return ParseDotEnv(blob), nil
}

func ParseDotEnv(raw []byte) map[string]string {
	parsed := make(map[string]string)
	scanner := bufio.NewScanner(bytes.NewReader(raw))
	for scanner.Scan() {
		line := strings.TrimSpace(scanner.Text())
		if line == "" || strings.HasPrefix(line, "#") {
			continue
		}
		if strings.HasPrefix(line, "export ") {
			line = strings.TrimSpace(strings.TrimPrefix(line, "export "))
		}

		key, value, ok := parseDotEnvAssignment(line)
		if !ok {
			continue
		}
		parsed[key] = value
	}
	return parsed
}

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
	existingLines := strings.Split(string(existingRaw), "\n")
	for _, line := range existingLines {
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

	if err := os.MkdirAll(filepath.Dir(path), 0o755); err != nil {
		return err
	}

	file, err := os.OpenFile(path, os.O_CREATE|os.O_RDWR, 0o644)
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

	if err := os.WriteFile(path, []byte(builder.String()), 0o644); err != nil {
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
