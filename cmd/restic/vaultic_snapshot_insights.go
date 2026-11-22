package main

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"strings"

	"github.com/restic/restic/internal/data"
	"github.com/restic/restic/internal/global"
	"github.com/restic/restic/internal/restic"
	"github.com/restic/restic/internal/ui"
	"github.com/restic/restic/internal/walker"
)

type vaulticSnapshotMetrics struct {
	Files            uint64
	Directories      uint64
	OtherEntries     uint64
	LogicalFileBytes uint64
}

// Count ordinary files and directories separately. restic stats --mode
// restore-size counts every node in total_file_count, including directories.
// Hardlinks count once per visible filename here, reflecting the snapshot
// browser rather than the compressed on-disk repository size.
func vaulticMeasureSnapshot(ctx context.Context, opts global.Options, id string) (vaulticSnapshotMetrics, error) {
	var result vaulticSnapshotMetrics
	if !vaulticSnapshotID.MatchString(id) {
		return result, errors.New(vaulticT("Identifiant d'instantané invalide"))
	}
	printer := ui.NewProgressPrinter(opts.JSON, opts.Verbosity, opts.Term)
	ctx, repo, unlock, err := openWithReadLock(ctx, opts, opts.NoLock, printer)
	if err != nil {
		return result, err
	}
	defer unlock()
	filter := data.SnapshotFilter{}
	snapshot, subfolder, err := filter.FindLatest(ctx, repo, repo, id)
	if err != nil {
		return result, err
	}
	if snapshot.Tree == nil {
		return result, errors.New(vaulticT("Cet instantané n'a pas d'arborescence"))
	}
	if err := repo.LoadIndex(ctx, printer); err != nil {
		return result, err
	}
	snapshot.Tree, err = data.FindTreeDirectory(ctx, repo, snapshot.Tree, subfolder)
	if err != nil {
		return result, err
	}
	if snapshot.Tree == nil {
		return result, errors.New(vaulticT("Impossible de trouver l'arborescence de l'instantané"))
	}
	err = walker.Walk(ctx, repo, *snapshot.Tree, walker.WalkVisitor{
		ProcessNode: func(_ restic.ID, _ string, node *data.Node, nodeErr error) error {
			if nodeErr != nil {
				return nodeErr
			}
			if err := ctx.Err(); err != nil {
				return err
			}
			if node == nil {
				return nil
			}
			switch node.Type {
			case data.NodeTypeFile:
				result.Files++
				result.LogicalFileBytes += node.Size
			case data.NodeTypeDir:
				result.Directories++
			default:
				result.OtherEntries++
			}
			return nil
		},
	})
	return result, err
}

const vaulticDiffPreviewLimit = 180
const vaulticDiffMaxLineBytes = 1 << 20

type vaulticDiffRecord struct {
	MessageType string `json:"message_type"`
	Path        string `json:"path"`
	Modifier    string `json:"modifier"`
	Truncated   bool   `json:"-"`
}
type vaulticSnapshotComparison struct {
	SourceID        string
	TargetID        string
	Stats           DiffStatsContainer
	Changes         []vaulticDiffRecord
	TotalChanges    int
	AddedChanges    int
	RemovedChanges  int
	ModifiedChanges int
}

type vaulticDiffCollector struct {
	pending   []byte
	result    vaulticSnapshotComparison
	gotStats  bool
	decodeErr error
}

func (c *vaulticDiffCollector) collectLine(line []byte) {
	if c.decodeErr != nil || len(bytes.TrimSpace(line)) == 0 {
		return
	}
	var head struct {
		MessageType string `json:"message_type"`
	}
	if err := json.Unmarshal(line, &head); err != nil {
		c.decodeErr = fmt.Errorf(vaulticT("Résultat de comparaison illisible : %w"), err)
		return
	}
	switch head.MessageType {
	case "change":
		var item vaulticDiffRecord
		if err := json.Unmarshal(line, &item); err != nil {
			c.decodeErr = err
			return
		}
		c.result.TotalChanges++
		// Keep a separate bounded sample of EACH category. Otherwise the first
		// 180 additions might hide every deletion and modification in the UI.
		count := 0
		switch vaulticDiffCategory(item.Modifier) {
		case vaulticDiffAdded:
			c.result.AddedChanges++
			count = c.result.AddedChanges
		case vaulticDiffRemoved:
			c.result.RemovedChanges++
			count = c.result.RemovedChanges
		default:
			c.result.ModifiedChanges++
			count = c.result.ModifiedChanges
		}
		if count <= vaulticDiffPreviewLimit {
			// Long and control-character paths must not overwhelm the UI.
			if len(item.Path) > 700 {
				chars := []rune(item.Path)
				if len(chars) > 300 {
					item.Path = string(chars[:300]) + "…"
					item.Truncated = true
				}
			}
			c.result.Changes = append(c.result.Changes, item)
		}
	case "statistics":
		if err := json.Unmarshal(line, &c.result.Stats); err != nil {
			c.decodeErr = err
			return
		}
		c.gotStats = true
	}
}

// Streaming collector. Only the first N changes per category are retained;
// restic diff can emit hundreds of thousands of entries on large repositories.
func (c *vaulticDiffCollector) Write(p []byte) (int, error) {
	n := len(p)
	if c.decodeErr != nil {
		return n, nil
	}
	for len(p) > 0 {
		index := bytes.IndexByte(p, '\n')
		if index < 0 {
			if len(c.pending)+len(p) > vaulticDiffMaxLineBytes {
				c.decodeErr = errors.New(vaulticT("Une entrée de comparaison dépasse la limite autorisée"))
			} else {
				c.pending = append(c.pending, p...)
			}
			break
		}
		if len(c.pending)+index > vaulticDiffMaxLineBytes {
			c.decodeErr = errors.New(vaulticT("Une entrée de comparaison dépasse la limite autorisée"))
			break
		}
		c.pending = append(c.pending, p[:index]...)
		c.collectLine(c.pending)
		c.pending = c.pending[:0]
		p = p[index+1:]
		if c.decodeErr != nil {
			break
		}
	}
	return n, nil
}

// Compare older -> newer with the official in-process restic diff command.
func vaulticCompareSnapshots(ctx context.Context, opts global.Options, sourceID, targetID string) (vaulticSnapshotComparison, error) {
	var zero vaulticSnapshotComparison
	if !vaulticSnapshotID.MatchString(sourceID) || !vaulticSnapshotID.MatchString(targetID) {
		return zero, errors.New(vaulticT("Identifiants d'instantané invalides"))
	}
	if sourceID == targetID {
		return zero, errors.New(vaulticT("Choisissez deux instantanés différents"))
	}
	terminal, ok := opts.Term.(*vaulticTerminal)
	if !ok {
		return zero, errors.New(vaulticT("Terminal Vaultic requis"))
	}
	collector := &vaulticDiffCollector{}
	original := terminal.output
	terminal.output = collector
	defer func() { terminal.output = original }()
	opts.JSON = true
	if err := runDiff(ctx, DiffOptions{}, opts, []string{sourceID, targetID}, terminal); err != nil {
		return zero, err
	}
	if collector.decodeErr != nil {
		return zero, collector.decodeErr
	}
	if len(bytes.TrimSpace(collector.pending)) != 0 {
		collector.collectLine(collector.pending)
	}
	if collector.decodeErr != nil {
		return zero, collector.decodeErr
	}
	if !collector.gotStats {
		return zero, errors.New(vaulticT("Statistiques de comparaison manquantes"))
	}
	collector.result.SourceID = sourceID
	collector.result.TargetID = targetID
	return collector.result, nil
}

func vaulticSameSnapshotSources(a, b vaulticSnapshot) bool {
	if a.Hostname != b.Hostname || len(a.Paths) != len(b.Paths) {
		return false
	}
	seen := make(map[string]int, len(a.Paths))
	for _, p := range a.Paths {
		seen[p]++
	}
	for _, p := range b.Paths {
		seen[p]--
		if seen[p] < 0 {
			return false
		}
	}
	return true
}

// Default to the latest older snapshot of the same host+source set.
// If none exists, choose the closest earlier snapshot in this repository.
func vaulticSuggestedComparisonBase(snapshots []vaulticSnapshot, target vaulticSnapshot) string {
	for priority := 0; priority < 4; priority++ {
		var best *vaulticSnapshot
		for i := range snapshots {
			candidate := &snapshots[i]
			if candidate.ID == target.ID {
				continue
			}
			same := vaulticSameSnapshotSources(*candidate, target)
			older := candidate.Time.Before(target.Time)
			var qualifies bool
			switch priority {
			case 0:
				qualifies = same && older
			case 1:
				qualifies = older
			case 2:
				qualifies = same
			default:
				qualifies = true
			}
			if !qualifies {
				continue
			}
			if best == nil || (older && candidate.Time.After(best.Time)) || (!older && candidate.Time.Before(best.Time)) {
				best = candidate
			}
		}
		if best != nil {
			return best.ID
		}
	}
	return ""
}

func vaulticFormatSnapshotMetrics(item vaulticSnapshotMetrics) string {
	return fmt.Sprintf(vaulticT("Fichiers : %d\nDossiers : %d\nAutres éléments : %d\nTaille logique des fichiers : %s"), item.Files, item.Directories, item.OtherEntries, vaulticBytes(item.LogicalFileBytes))
}

func vaulticFormatComparison(c vaulticSnapshotComparison) string {
	var b strings.Builder
	fmt.Fprintf(&b, vaulticT("Comparaison %s → %s\n\n"), c.SourceID, c.TargetID)
	fmt.Fprintf(&b, vaulticT("Fichiers ajoutés : %d\nFichiers supprimés : %d\nFichiers modifiés : %d\n"), c.Stats.Added.Files, c.Stats.Removed.Files, c.Stats.ChangedFiles)
	fmt.Fprintf(&b, vaulticT("Dossiers ajoutés : %d\nDossiers supprimés : %d\n"), c.Stats.Added.Dirs, c.Stats.Removed.Dirs)
	fmt.Fprintf(&b, vaulticT("Autres éléments ajoutés : %d\nAutres éléments supprimés : %d\n"), c.Stats.Added.Others, c.Stats.Removed.Others)
	fmt.Fprintf(&b, vaulticT("Blobs ajoutés : %s\nBlobs retirés : %s\n"), vaulticBytes(c.Stats.Added.Bytes), vaulticBytes(c.Stats.Removed.Bytes))
	fmt.Fprintf(&b, vaulticT("\n%d modification(s) détectée(s).\n"), c.TotalChanges)
	if c.TotalChanges == 0 {
		return b.String() + vaulticT("Aucune différence de contenu détectée.\n")
	}
	b.WriteString(vaulticT("\nPremières modifications :\n"))
	for _, entry := range c.Changes {
		label := vaulticT("Modifié")
		switch {
		case strings.Contains(entry.Modifier, "+"):
			label = vaulticT("Ajouté")
		case strings.Contains(entry.Modifier, "-"):
			label = vaulticT("Supprimé")
		case strings.Contains(entry.Modifier, "T"):
			label = vaulticT("Type modifié")
		case strings.Contains(entry.Modifier, "U"):
			label = vaulticT("Métadonnées")
		}
		// Escaping control characters avoids confusing multiline file names.
		fmt.Fprintf(&b, "%s  %q\n", label, entry.Path)
	}
	if c.TotalChanges > len(c.Changes) {
		fmt.Fprintf(&b, vaulticT("… %d autres modifications non affichées.\n"), c.TotalChanges-len(c.Changes))
	}
	b.WriteString(vaulticT("\nLes tailles ajoutées/retirées correspondent aux blocs restic, pas à l'espace disque total. Les changements de métadonnées seules ne sont pas inclus."))
	return b.String()
}
