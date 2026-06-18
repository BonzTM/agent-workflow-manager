package backend

import (
	_ "embed"
	"encoding/json"
	"errors"
	"os"
	"path"
	"regexp"
	"sort"
	"strings"

	bootstrapkit "github.com/bonztm/agent-workflow-manager/internal/bootstrap"
	"gopkg.in/yaml.v3"
)

const (
	maxTaskCanonicalTags         = 6
	canonicalTagsVersionV1       = "awm.tags.v1"
	canonicalTagsDefaultFilePath = ".awm/awm-tags.yaml"
	maxInitSuggestedTags         = 16
	minInitTagFileCount          = 2
)

var initIgnoredTagTokens = map[string]struct{}{
	"adapter":    {},
	"adapters":   {},
	"agent":      {},
	"agents":     {},
	"cmd":        {},
	"common":     {},
	"config":     {},
	"configs":    {},
	"contract":   {},
	"contracts":  {},
	"core":       {},
	"dir":        {},
	"example":    {},
	"examples":   {},
	"file":       {},
	"files":      {},
	"helper":     {},
	"helpers":    {},
	"impl":       {},
	"index":      {},
	"internal":   {},
	"lib":        {},
	"main":       {},
	"manager":    {},
	"model":      {},
	"models":     {},
	"pkg":        {},
	"project":    {},
	"readme":     {},
	"reference":  {},
	"references": {},
	"schema":     {},
	"schemas":    {},
	"spec":       {},
	"specs":      {},
	"src":        {},
	"type":       {},
	"types":      {},
	"util":       {},
	"utils":      {},
	"vendor":     {},
}

var wordPattern = regexp.MustCompile(`[A-Za-z0-9]+(?:[._:/-][A-Za-z0-9]+)*`)

//go:embed canonical_tags.json
var canonicalTagsJSON []byte

type canonicalTagsDocumentV1 struct {
	Version       string              `json:"version,omitempty" yaml:"version"`
	CanonicalTags map[string][]string `json:"canonical_tags" yaml:"canonical_tags"`
}

type canonicalTagsSource struct {
	SourcePath   string
	AbsolutePath string
	Exists       bool
}

type canonicalTagNormalizer struct {
	aliasMap map[string]string
}

type initTagSuggestion struct {
	Tag       string
	FileCount int
	DirCount  int
	BaseCount int
}

var defaultCanonicalTagNormalizer = canonicalTagNormalizer{
	aliasMap: loadCanonicalTagAliasMap(canonicalTagsJSON),
}

func loadCanonicalTagAliasMap(raw []byte) map[string]string {
	doc := canonicalTagsDocumentV1{}
	if err := json.Unmarshal(raw, &doc); err != nil {
		return map[string]string{}
	}
	return loadCanonicalTagAliasMapFromDocument(doc)
}

func loadCanonicalTagAliasMapFromDocument(doc canonicalTagsDocumentV1) map[string]string {
	aliasMap := make(map[string]string)
	for canonical, aliases := range doc.CanonicalTags {
		canonical = strings.ToLower(strings.TrimSpace(canonical))
		if canonical == "" {
			continue
		}
		aliasMap[canonical] = canonical
		for _, alias := range aliases {
			alias = strings.ToLower(strings.TrimSpace(alias))
			if alias == "" {
				continue
			}
			aliasMap[alias] = canonical
		}
	}
	return aliasMap
}

func (s *Service) loadCanonicalTagNormalizer(projectRoot, tagsFile string) (canonicalTagNormalizer, error) {
	aliasMap, err := loadMergedCanonicalTagAliasMap(projectRoot, tagsFile)
	if err != nil {
		return canonicalTagNormalizer{}, err
	}
	return canonicalTagNormalizer{aliasMap: aliasMap}, nil
}

func loadMergedCanonicalTagAliasMap(projectRoot, tagsFile string) (map[string]string, error) {
	aliasMap := cloneCanonicalTagAliasMap(defaultCanonicalTagNormalizer.aliasMap)

	source, err := discoverCanonicalTagsSource(projectRoot, tagsFile)
	if err != nil {
		return nil, err
	}
	if !source.Exists {
		return aliasMap, nil
	}

	document, err := parseCanonicalTagsFile(source)
	if err != nil {
		return nil, err
	}
	for alias, canonical := range loadCanonicalTagAliasMapFromDocument(document) {
		aliasMap[alias] = canonical
	}
	return aliasMap, nil
}

func discoverCanonicalTagsSource(projectRoot, tagsFile string) (canonicalTagsSource, error) {
	sourcePath := canonicalTagsDefaultFilePath
	if trimmedTagsFile := strings.TrimSpace(tagsFile); trimmedTagsFile != "" {
		sourcePath = trimmedTagsFile
	}

	normalizedPath, absolutePath, err := resolveProjectSourcePath(projectRoot, sourcePath)
	if err != nil {
		return canonicalTagsSource{}, err
	}
	stat, err := os.Stat(absolutePath)
	exists := false
	switch {
	case err == nil:
		exists = !stat.IsDir()
	case errors.Is(err, os.ErrNotExist):
		exists = false
	default:
		return canonicalTagsSource{}, err
	}

	return canonicalTagsSource{
		SourcePath:   normalizedPath,
		AbsolutePath: absolutePath,
		Exists:       exists,
	}, nil
}

func parseCanonicalTagsFile(source canonicalTagsSource) (canonicalTagsDocumentV1, error) {
	blob, err := os.ReadFile(source.AbsolutePath)
	if err != nil {
		return canonicalTagsDocumentV1{}, err
	}

	decoder := yaml.NewDecoder(strings.NewReader(string(blob)))
	decoder.KnownFields(true)

	doc := canonicalTagsDocumentV1{}
	if err := decoder.Decode(&doc); err != nil {
		return canonicalTagsDocumentV1{}, err
	}
	if strings.TrimSpace(doc.Version) != canonicalTagsVersionV1 {
		return canonicalTagsDocumentV1{}, errors.New("canonical tags file has unsupported version")
	}
	if doc.CanonicalTags == nil {
		doc.CanonicalTags = map[string][]string{}
	}
	return doc, nil
}

func cloneCanonicalTagAliasMap(source map[string]string) map[string]string {
	if len(source) == 0 {
		return map[string]string{}
	}
	cloned := make(map[string]string, len(source))
	for alias, canonical := range source {
		cloned[alias] = canonical
	}
	return cloned
}

func syncInitCanonicalTagsFile(projectRoot, tagsFile string, candidatePaths []string) error {
	source, err := discoverCanonicalTagsSource(projectRoot, tagsFile)
	if err != nil {
		return err
	}
	if strings.TrimSpace(source.AbsolutePath) == "" {
		return nil
	}

	suggestions := initCanonicalTagsDocument(candidatePaths)
	if source.Exists {
		existing, err := parseCanonicalTagsFile(source)
		if err != nil {
			return err
		}
		if len(existing.CanonicalTags) > 0 || len(suggestions.CanonicalTags) == 0 {
			return nil
		}
		return os.WriteFile(source.AbsolutePath, renderCanonicalTagsDocumentYAML(suggestions), 0o644)
	}

	return bootstrapkit.WriteScaffoldFile(source.AbsolutePath, renderCanonicalTagsDocumentYAML(suggestions))
}

func initCanonicalTagsDocument(candidatePaths []string) canonicalTagsDocumentV1 {
	document := canonicalTagsDocumentV1{
		Version:       canonicalTagsVersionV1,
		CanonicalTags: map[string][]string{},
	}

	suggestions := suggestInitCanonicalTags(candidatePaths)
	for _, suggestion := range suggestions {
		document.CanonicalTags[suggestion.Tag] = []string{}
	}
	return document
}

func suggestInitCanonicalTags(candidatePaths []string) []initTagSuggestion {
	type tagStats struct {
		fileCount int
		dirCount  int
		baseCount int
	}

	stats := map[string]*tagStats{}
	for _, candidatePath := range normalizeCompletionPaths(candidatePaths) {
		dirTokens, baseTokens := initTagTokensForPath(candidatePath)

		seenInFile := map[string]struct{}{}
		for _, token := range dirTokens {
			stat := stats[token]
			if stat == nil {
				stat = &tagStats{}
				stats[token] = stat
			}
			stat.dirCount++
			seenInFile[token] = struct{}{}
		}
		for _, token := range baseTokens {
			stat := stats[token]
			if stat == nil {
				stat = &tagStats{}
				stats[token] = stat
			}
			stat.baseCount++
			seenInFile[token] = struct{}{}
		}
		for token := range seenInFile {
			stats[token].fileCount++
		}
	}

	suggestions := make([]initTagSuggestion, 0, len(stats))
	for token, stat := range stats {
		if stat.fileCount < minInitTagFileCount {
			continue
		}
		suggestions = append(suggestions, initTagSuggestion{
			Tag:       token,
			FileCount: stat.fileCount,
			DirCount:  stat.dirCount,
			BaseCount: stat.baseCount,
		})
	}

	sort.Slice(suggestions, func(i, j int) bool {
		leftScore := initTagSuggestionScore(suggestions[i])
		rightScore := initTagSuggestionScore(suggestions[j])
		if leftScore != rightScore {
			return leftScore > rightScore
		}
		if suggestions[i].FileCount != suggestions[j].FileCount {
			return suggestions[i].FileCount > suggestions[j].FileCount
		}
		return suggestions[i].Tag < suggestions[j].Tag
	})
	if len(suggestions) > maxInitSuggestedTags {
		suggestions = suggestions[:maxInitSuggestedTags]
	}
	sort.Slice(suggestions, func(i, j int) bool {
		return suggestions[i].Tag < suggestions[j].Tag
	})

	return suggestions
}

func initTagSuggestionScore(suggestion initTagSuggestion) int {
	return (suggestion.FileCount * 3) + (suggestion.DirCount * 2) + suggestion.BaseCount
}

func initTagTokensForPath(candidatePath string) ([]string, []string) {
	normalizedPath := normalizeCompletionPath(candidatePath)
	if normalizedPath == "" {
		return nil, nil
	}

	dirTokens := make(map[string]struct{})
	dirPath := path.Dir(normalizedPath)
	if dirPath != "" && dirPath != "." {
		for _, segment := range strings.Split(dirPath, "/") {
			addInitTagTokens(dirTokens, segment)
		}
	}

	baseTokens := make(map[string]struct{})
	baseName := strings.TrimSuffix(path.Base(normalizedPath), path.Ext(normalizedPath))
	addInitTagTokens(baseTokens, baseName)

	return mapKeysSorted(dirTokens), mapKeysSorted(baseTokens)
}

func addInitTagTokens(dest map[string]struct{}, raw string) {
	for _, token := range strings.FieldsFunc(strings.ToLower(strings.TrimSpace(raw)), isTagTokenSeparator) {
		token = strings.TrimSpace(token)
		if !shouldSuggestInitTagToken(token) {
			continue
		}
		dest[token] = struct{}{}
	}
}

func shouldSuggestInitTagToken(token string) bool {
	if len(token) < 3 {
		return false
	}
	if token[0] < 'a' || token[0] > 'z' {
		return false
	}
	if !healthTagPattern.MatchString(token) {
		return false
	}
	if _, ignored := initIgnoredTagTokens[token]; ignored {
		return false
	}
	if _, known := defaultCanonicalTagNormalizer.aliasMap[token]; known {
		return false
	}
	return true
}

func renderCanonicalTagsDocumentYAML(document canonicalTagsDocumentV1) []byte {
	var builder strings.Builder
	builder.WriteString("version: ")
	builder.WriteString(canonicalTagsVersionV1)
	builder.WriteString("\n")
	if len(document.CanonicalTags) == 0 {
		builder.WriteString("canonical_tags: {}\n")
		return []byte(builder.String())
	}

	builder.WriteString("canonical_tags:\n")
	keys := make([]string, 0, len(document.CanonicalTags))
	for key := range document.CanonicalTags {
		keys = append(keys, key)
	}
	sort.Strings(keys)
	for _, key := range keys {
		aliases := append([]string(nil), document.CanonicalTags[key]...)
		sort.Strings(aliases)
		builder.WriteString("  ")
		builder.WriteString(key)
		builder.WriteString(": ")
		if len(aliases) == 0 {
			builder.WriteString("[]\n")
			continue
		}
		builder.WriteString("[")
		builder.WriteString(strings.Join(aliases, ", "))
		builder.WriteString("]\n")
	}
	return []byte(builder.String())
}

func normalizeCanonicalTag(raw string) string {
	return defaultCanonicalTagNormalizer.normalizeTag(raw)
}

func normalizeCanonicalTags(values []string) []string {
	return defaultCanonicalTagNormalizer.normalizeTags(values)
}

func canonicalTagsFromTaskText(taskText string) []string {
	return defaultCanonicalTagNormalizer.canonicalTagsFromTaskText(taskText)
}

func (n canonicalTagNormalizer) normalizeTag(raw string) string {
	tag := strings.ToLower(strings.TrimSpace(raw))
	if tag == "" {
		return ""
	}
	if canonical, ok := n.aliasMap[tag]; ok {
		return canonical
	}
	return tag
}

func (n canonicalTagNormalizer) normalizeTags(values []string) []string {
	if len(values) == 0 {
		return nil
	}

	seen := make(map[string]struct{}, len(values))
	out := make([]string, 0, len(values))
	for _, raw := range values {
		tag := n.normalizeTag(raw)
		if tag == "" {
			continue
		}
		if _, ok := seen[tag]; ok {
			continue
		}
		seen[tag] = struct{}{}
		out = append(out, tag)
	}
	if len(out) == 0 {
		return nil
	}
	sort.Strings(out)
	return out
}

func (n canonicalTagNormalizer) canonicalTagsFromTaskText(taskText string) []string {
	if strings.TrimSpace(taskText) == "" || len(n.aliasMap) == 0 {
		return nil
	}

	seen := make(map[string]struct{})
	tokens := wordPattern.FindAllString(taskText, -1)
	for _, token := range tokens {
		token = strings.ToLower(strings.TrimSpace(token))
		if token == "" {
			continue
		}
		if canonical, ok := n.aliasMap[token]; ok {
			seen[canonical] = struct{}{}
		}
		for _, part := range strings.FieldsFunc(token, isTagTokenSeparator) {
			if canonical, ok := n.aliasMap[part]; ok {
				seen[canonical] = struct{}{}
			}
		}
	}

	out := mapKeysSorted(seen)
	if len(out) > maxTaskCanonicalTags {
		return out[:maxTaskCanonicalTags]
	}
	return out
}

func isTagTokenSeparator(r rune) bool {
	switch r {
	case '.', '_', ':', '/', '-':
		return true
	default:
		return false
	}
}
