package main

import (
    "fmt"
    "strings"

    "github.com/restic/restic/internal/filter"
)

const vaulticMaxExclusionRules = 1000
const vaulticMaxExclusionBytes = 64 << 10

// Exclusion rules are native restic exclude patterns. Empty lines and lines
// beginning with '#' are treated as editor comments. Nothing here is sent to
// a shell or interpreted as a regular expression.
func vaulticParseExclusions(text string) ([]string, error) {
    if len(text) > vaulticMaxExclusionBytes {
        return nil, fmt.Errorf("La liste d'exclusions dépasse %d Kio", vaulticMaxExclusionBytes/1024)
    }
    var rules []string
    seen := make(map[string]bool)
    for lineNumber, raw := range strings.Split(text, "\n") {
        rule := strings.TrimSpace(strings.TrimSuffix(raw, "\r"))
        if rule == "" || strings.HasPrefix(rule, "#") {
            continue
        }
        if strings.ContainsRune(rule, 0) {
            return nil, fmt.Errorf("Caractère non valide à la ligne %d", lineNumber+1)
        }
        if !seen[rule] {
            seen[rule] = true
            rules = append(rules, rule)
        }
        if len(rules) > vaulticMaxExclusionRules {
            return nil, fmt.Errorf("Limite de %d règles d'exclusion dépassée", vaulticMaxExclusionRules)
        }
    }
    if err := filter.ValidatePatterns(rules); err != nil {
        return nil, fmt.Errorf("Motif d'exclusion invalide : %w", err)
    }
    return rules, nil
}
