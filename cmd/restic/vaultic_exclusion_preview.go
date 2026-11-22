package main

import (
	"context"
	"errors"
	"fmt"
	"io/fs"
	"path/filepath"
	"strings"

	"github.com/restic/restic/internal/filter"
)

const (
	vaulticPreviewMaxEntries  = 25000
	vaulticPreviewMaxExamples = 80
)

type vaulticExclusionPreview struct {
	Scanned   int
	Matched   int
	Examples  []string
	Truncated bool
}

// An indicative, read-only local preview of restic exclude patterns. This does
// not invoke backup or mutate sources. One excluded directory represents its
// entire subtree; the count is *not* the number of skipped files. Other restic
// policies and changes during scanning can change the real backup result.
func vaulticPreviewExcludedPaths(ctx context.Context, sources, rules []string) (vaulticExclusionPreview, error) {
	var preview vaulticExclusionPreview
	if len(rules) == 0 {
		return preview, nil
	}
	if err := filter.ValidatePatterns(rules); err != nil {
		return preview, err
	}
	hasNegations := false
	for _, rule := range rules {
		if strings.HasPrefix(rule, "!") {
			hasNegations = true
			break
		}
	}
	matches := filter.RejectByPattern(rules, func(string, ...interface{}) {})
	limitReached := errors.New(vaulticT("preview limit reached"))
	for _, source := range sources {
		err := filepath.WalkDir(source, func(p string, d fs.DirEntry, walkErr error) error {
			if err := ctx.Err(); err != nil {
				return err
			}
			if walkErr != nil {
				return fmt.Errorf(vaulticT("Lecture de %q : %w"), p, walkErr)
			}
			preview.Scanned++
			if preview.Scanned > vaulticPreviewMaxEntries {
				return limitReached
			}
			if matches(p) {
				preview.Matched++
				if len(preview.Examples) < vaulticPreviewMaxExamples {
					item := p
					if d.IsDir() {
						item += " [dossier]"
					}
					preview.Examples = append(preview.Examples, item)
				}
				// Keep exploring if a '!' rule could re-include descendants.
				if d.IsDir() && !hasNegations {
					return filepath.SkipDir
				}
			}
			return nil
		})
		if errors.Is(err, limitReached) {
			preview.Truncated = true
			break
		}
		if err != nil {
			return preview, err
		}
	}
	return preview, nil
}

func vaulticFormatExclusionPreview(result vaulticExclusionPreview) string {
	var b strings.Builder
	fmt.Fprintf(&b, vaulticT("Éléments parcourus : %d\nCorrespondances détectées : %d\n"), result.Scanned, result.Matched)
	if len(result.Examples) == 0 {
		b.WriteString(vaulticT("\nAucun élément correspondant dans les sources parcourues.\n"))
	} else {
		b.WriteString(vaulticT("\nExemples de chemins correspondants :\n"))
		for _, p := range result.Examples {
			fmt.Fprintf(&b, "• %s\n", p)
		}
		if result.Matched > len(result.Examples) {
			fmt.Fprintf(&b, vaulticT("… et %d autre(s)\n"), result.Matched-len(result.Examples))
		}
	}
	if result.Truncated {
		b.WriteString(vaulticT("\nLimite de parcours atteinte : aperçu partiel.\n"))
	}
	b.WriteString(vaulticT("\nAperçu indicatif : un dossier exclu compte pour une correspondance, pas pour tous ses fichiers. Les règles de ré-inclusion (!), les permissions et les changements de fichiers peuvent modifier le résultat réel."))
	return b.String()
}
