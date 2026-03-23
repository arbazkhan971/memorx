package git

import (
	"path/filepath"
	"strings"
)

var (
	conventionalPrefixes = map[string]string{
		"feat": "feature", "fix": "bugfix", "docs": "docs", "test": "test",
		"refactor": "refactor", "chore": "cleanup", "ci": "infra",
	}
	messageKeywords = map[string]string{
		"fix": "bugfix", "bug": "bugfix", "patch": "bugfix", "resolve": "bugfix",
		"crash": "bugfix", "issue": "bugfix", "error": "bugfix",
		"feat": "feature", "add": "feature", "implement": "feature",
		"support": "feature", "introduce": "feature", "new": "feature",
		"refactor": "refactor", "clean": "refactor", "rename": "refactor",
		"simplify": "refactor", "reorganize": "refactor", "restructure": "refactor",
		"test": "test", "spec": "test", "coverage": "test",
		"doc": "docs", "readme": "docs", "comment": "docs", "docs": "docs",
		"ci": "infra", "cd": "infra", "docker": "infra",
		"deploy": "infra", "infra": "infra", "config": "infra",
		"chore": "cleanup", "cleanup": "cleanup", "lint": "cleanup", "format": "cleanup",
	}
	fileSignalRules = []struct {
		intent    string
		predicate func(string) bool
	}{{"test", isTestFile}, {"infra", isInfraFile}, {"docs", isDocFile}}
)

func ClassifyIntent(message string, files []string) (string, float64) {
	if it, ok := checkConventionalPrefix(message); ok {
		return it, 0.9
	}
	if it, ok := checkMessageKeywords(message); ok {
		return it, 0.8
	}
	if it, ok := checkFileSignals(files); ok {
		return it, 0.6
	}
	return "unknown", 0.0
}

func checkConventionalPrefix(message string) (string, bool) {
	lower := strings.ToLower(strings.TrimSpace(message))
	for prefix, intentType := range conventionalPrefixes {
		if strings.HasPrefix(lower, prefix+":") || strings.HasPrefix(lower, prefix+"(") {
			return intentType, true
		}
	}
	return "", false
}

func checkMessageKeywords(message string) (string, bool) {
	for _, word := range tokenize(strings.ToLower(message)) {
		if intentType, ok := messageKeywords[word]; ok {
			return intentType, true
		}
	}
	return "", false
}

func tokenize(message string) []string {
	return strings.Fields(strings.NewReplacer(
		":", " ", "/", " ", "-", " ", "_", " ", "(", " ", ")", " ",
		"[", " ", "]", " ", ",", " ", ".", " ", "!", " ", "#", " ",
	).Replace(message))
}

func checkFileSignals(files []string) (string, bool) {
	if len(files) == 0 {
		return "", false
	}
	for _, rule := range fileSignalRules {
		if allMatch(files, rule.predicate) {
			return rule.intent, true
		}
	}
	return "", false
}

func allMatch(files []string, predicate func(string) bool) bool {
	for _, f := range files {
		if !predicate(f) {
			return false
		}
	}
	return true
}

func isTestFile(path string) bool {
	b := filepath.Base(path)
	return strings.HasSuffix(b, "_test.go") || strings.Contains(b, ".test.") || strings.Contains(b, ".spec.")
}

func isInfraFile(path string) bool {
	lower, ext := strings.ToLower(filepath.Base(path)), strings.ToLower(filepath.Ext(path))
	return lower == "dockerfile" || lower == "docker-compose.yml" || lower == "docker-compose.yaml" ||
		ext == ".yml" || ext == ".yaml" ||
		strings.HasPrefix(path, ".github/") || strings.HasPrefix(path, ".github"+string(filepath.Separator))
}

func isDocFile(path string) bool {
	return strings.ToLower(filepath.Ext(path)) == ".md" ||
		strings.HasPrefix(path, "docs/") || strings.HasPrefix(path, "docs"+string(filepath.Separator))
}
