package plans

import (
	"fmt"
	"regexp"
	"strings"
	"unicode"
)

var (
	stepPattern    = regexp.MustCompile(`(?m)^\s*(?:\d+[.)]\s+|-\s+Step:\s+)(.+)`)
	keywordPattern = regexp.MustCompile(`(?i)\b(?:plan|steps|todo|phase|milestone|implementation)\b`)
)

func IsPlanLike(content string) bool {
	return keywordPattern.MatchString(content) && len(stepPattern.FindAllString(content, -1)) >= 3
}

func ParseSteps(content string) []StepInput {
	matches := stepPattern.FindAllStringSubmatch(content, -1)
	steps := make([]StepInput, 0, len(matches))
	for _, m := range matches {
		if title := strings.TrimSpace(m[1]); title != "" {
			steps = append(steps, StepInput{Title: title})
		}
	}
	return steps
}

const matchThreshold = 0.3

func (m *Manager) MatchCommitToSteps(commitMessage, featureID string) (*PlanStep, error) {
	plan, err := m.GetActivePlan(featureID)
	if err != nil {
		return nil, fmt.Errorf("get active plan: %w", err)
	}
	steps, err := m.GetPlanSteps(plan.ID)
	if err != nil {
		return nil, fmt.Errorf("get plan steps: %w", err)
	}
	commitWords := tokenize(commitMessage)
	if len(commitWords) == 0 {
		return nil, nil
	}
	var bestStep *PlanStep
	bestScore := 0.0
	for i := range steps {
		if steps[i].Status == "completed" || steps[i].Status == "skipped" {
			continue
		}
		score := jaccard(commitWords, tokenize(steps[i].Title))
		if steps[i].Description != "" {
			if ds := jaccard(commitWords, tokenize(steps[i].Description)); ds > score {
				score = ds
			}
		}
		if score > bestScore {
			bestScore, bestStep = score, &steps[i]
		}
	}
	if bestScore < matchThreshold {
		return nil, nil
	}
	result := *bestStep
	return &result, nil
}

func tokenize(text string) map[string]bool {
	words := make(map[string]bool)
	for _, w := range strings.FieldsFunc(strings.ToLower(text), func(r rune) bool {
		return !unicode.IsLetter(r) && !unicode.IsDigit(r)
	}) {
		words[w] = true
	}
	return words
}

func jaccard(a, b map[string]bool) float64 {
	if len(a) == 0 && len(b) == 0 {
		return 0
	}
	inter := 0
	for w := range a {
		if b[w] {
			inter++
		}
	}
	if union := len(a) + len(b) - inter; union > 0 {
		return float64(inter) / float64(union)
	}
	return 0
}
