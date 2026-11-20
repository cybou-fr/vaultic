package main

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"path"
	"path/filepath"
	"sort"
	"strings"

	"github.com/restic/restic/internal/data"
	"github.com/restic/restic/internal/global"
	"github.com/restic/restic/internal/restic"
	"github.com/restic/restic/internal/restorer"
	"github.com/restic/restic/internal/ui"
	restoreui "github.com/restic/restic/internal/ui/restore"
	"github.com/restic/restic/internal/walker"
)

// A restic ls --json node. Repository paths use forward slashes on all OSes.
type vaulticSnapshotNode struct {
	Name        string  `json:"name"`
	Path        string  `json:"path"`
	Type        string  `json:"type"`
	Size        *uint64 `json:"size,omitempty"`
	MessageType string  `json:"message_type"`
}

type vaulticLimitedBuffer struct {
	bytes.Buffer
	Limit int
}

func (b *vaulticLimitedBuffer) Write(data []byte) (int, error) {
	if len(data) > b.Limit-b.Len() {
		return 0, errors.New("Le dossier contient trop d'éléments pour être affiché")
	}
	return b.Buffer.Write(data)
}

func vaulticValidateSnapshotPath(p string) error {
	if p == "" || !strings.HasPrefix(p, "/") || path.Clean(p) != p || strings.ContainsRune(p, '\x00') {
		return fmt.Errorf("Chemin d'instantané invalide : %q", p)
	}
	return nil
}

// Calls upstream restic's in-process ls implementation, without any CLI process.
func vaulticListSnapshotDirectory(ctx context.Context, opts global.Options, id, dir string) ([]vaulticSnapshotNode, error) {
	if !vaulticSnapshotID.MatchString(id) {
		return nil, errors.New("Identifiant d'instantané invalide")
	}
	if err := vaulticValidateSnapshotPath(dir); err != nil {
		return nil, err
	}
	buffer := &vaulticLimitedBuffer{Limit: 16 << 20}
	term := &vaulticTerminal{output: buffer}
	opts.Term = term
	opts.JSON = true
	args := []string{id}
	if dir != "/" {
		args = append(args, dir)
	}
	if err := runLs(ctx, LsOptions{}, opts, args, term); err != nil {
		return nil, err
	}
	dec := json.NewDecoder(bytes.NewReader(buffer.Bytes()))
	list := make([]vaulticSnapshotNode, 0)
	for {
		var n vaulticSnapshotNode
		if err := dec.Decode(&n); err != nil {
			if errors.Is(err, io.EOF) {
				break
			}
			return nil, fmt.Errorf("Lecture des éléments : %w", err)
		}
		if n.MessageType != "node" {
			continue
		}
		if err := vaulticValidateSnapshotPath(n.Path); err != nil {
			return nil, err
		}
		if n.Path != "/" && path.Dir(n.Path) == dir {
			list = append(list, n)
		}
	}
	sort.SliceStable(list, func(i, j int) bool {
		ai, aj := list[i].Type == "dir", list[j].Type == "dir"
		if ai != aj {
			return ai
		}
		return strings.ToLower(list[i].Name) < strings.ToLower(list[j].Name)
	})
	return list, nil
}

// Walk the snapshot once and stop after one additional match confirms truncation.
func vaulticSearchSnapshot(ctx context.Context, opts global.Options, id, query string, limit int) ([]vaulticSnapshotNode, bool, error) {
	if !vaulticSnapshotID.MatchString(id) {
		return nil, false, errors.New("Identifiant d'instantané invalide")
	}
	query = strings.ToLower(strings.TrimSpace(query))
	if query == "" {
		return nil, false, errors.New("Saisissez un nom ou un chemin à rechercher")
	}
	printer := ui.NewProgressPrinter(opts.JSON, opts.Verbosity, opts.Term)
	ctx, repo, unlock, err := openWithReadLock(ctx, opts, opts.NoLock, printer)
	if err != nil {
		return nil, false, err
	}
	defer repo.Close()
	defer unlock()
	var filter data.SnapshotFilter
	snapshot, _, err := filter.FindLatest(ctx, repo, repo, id)
	if err != nil {
		return nil, false, err
	}
	if err := repo.LoadIndex(ctx, printer); err != nil {
		return nil, false, err
	}
	if snapshot.Tree == nil {
		return nil, false, errors.New("Cet instantané n’a pas d’arborescence")
	}
	var matches []vaulticSnapshotNode
	limitReached := errors.New("limite de résultats atteinte")
	err = walker.Walk(ctx, repo, *snapshot.Tree, walker.WalkVisitor{
		ProcessNode: func(_ restic.ID, location string, node *data.Node, err error) error {
			if err != nil || node == nil {
				return err
			}
			if node.Name == "." || node.Name == ".." || node.Name != path.Base(node.Name) ||
				(filepath.Separator != '/' && strings.ContainsRune(node.Name, filepath.Separator)) {
				return fmt.Errorf("Nom invalide dans l'instantané : %q", node.Name)
			}
			if err := vaulticValidateSnapshotPath(location); err != nil {
				return err
			}
			if !strings.Contains(strings.ToLower(location), query) {
				return nil
			}
			if len(matches) == limit {
				return limitReached
			}
			match := vaulticSnapshotNode{Name: node.Name, Path: location, Type: string(node.Type)}
			if node.Type == data.NodeTypeFile {
				size := node.Size
				match.Size = &size
			}
			matches = append(matches, match)
			return nil
		},
	})
	if errors.Is(err, limitReached) {
		return matches, true, nil
	}
	return matches, false, err
}

// Match exact selected paths and directory descendants, visiting only the
// ancestor directories needed to reach the selection.
func vaulticSelectionFilter(nodes []vaulticSnapshotNode) func(string, bool) (bool, bool) {
	exact := make(map[string]bool, len(nodes))
	ancestors := make(map[string]bool)
	for _, node := range nodes {
		exact[node.Path] = node.Type == "dir"
		for parent := path.Dir(node.Path); parent != "/" && parent != "."; parent = path.Dir(parent) {
			ancestors[parent] = true
		}
	}
	return func(item string, isDir bool) (bool, bool) {
		item = filepath.ToSlash(item)
		if directory, ok := exact[item]; ok {
			return true, isDir && directory
		}
		for parent := path.Dir(item); parent != "/" && parent != "."; parent = path.Dir(parent) {
			if exact[parent] {
				return true, isDir
			}
		}
		return false, isDir && ancestors[item]
	}
}

// Restores selected nodes through the original restic repository/restorer
// packages, with no subprocess, deletions or overwriting existing files.
func vaulticRestoreSelected(ctx context.Context, opts global.Options, id string, nodes []vaulticSnapshotNode, destination string) error {
	if !vaulticSnapshotID.MatchString(id) {
		return errors.New("Identifiant d'instantané invalide")
	}
	if len(nodes) == 0 {
		return errors.New("Sélectionnez un fichier ou un dossier")
	}
	for _, node := range nodes {
		if node.Path == "/" {
			return errors.New("Sélectionnez un fichier ou un dossier")
		}
		if err := vaulticValidateSnapshotPath(node.Path); err != nil {
			return err
		}
		if !vaulticRestorableNode(node) {
			return errors.New("Type d'élément non pris en charge")
		}
	}
	target, err := vaulticRestoreTarget(destination)
	if err != nil {
		return err
	}
	printer := restoreui.NewJSONProgress(opts.Term, opts.Verbosity)
	ctx, repo, unlock, err := openWithReadLock(ctx, opts, opts.NoLock, printer)
	if err != nil {
		return err
	}
	defer repo.Close()
	defer unlock()
	var filter data.SnapshotFilter
	snapshot, subfolder, err := filter.FindLatest(ctx, repo, repo, id)
	if err != nil {
		return err
	}
	if err := repo.LoadIndex(ctx, printer); err != nil {
		return err
	}
	snapshot.Tree, err = data.FindTreeDirectory(ctx, repo, snapshot.Tree, subfolder)
	if err != nil {
		return err
	}
	progress := restoreui.NewProgress(printer, ui.CalculateProgressInterval(!opts.Quiet, true, opts.Term.CanUpdateStatus()))
	r := restorer.NewRestorer(repo, snapshot, restorer.Options{
		Progress:                  progress,
		Overwrite:                 restorer.OverwriteNever,
		InheritWindowsPermissions: true,
		Delete:                    false,
	})
	r.SelectFilter = vaulticSelectionFilter(nodes)
	r.Error = func(location string, e error) error {
		printer.E("Erreur de restauration de %q : %v", location, e)
		return e
	}
	_, err = r.RestoreTo(ctx, target)
	progress.Finish()
	return err
}

func vaulticRestorableNode(node vaulticSnapshotNode) bool {
	return node.Type == "dir" || node.Type == "file" || node.Type == "symlink"
}
