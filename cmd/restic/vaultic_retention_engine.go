package main

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"regexp"
	"sort"

	"github.com/restic/restic/internal/global"
)

// The only places where Vaultic deletes snapshots or frees repository space.
// A static test checks that nothing else in the application calls them.

// vaulticFullSnapshotID is a complete snapshot ID: deletions never use short
// IDs, prefixes or names like "latest".
var vaulticFullSnapshotID = regexp.MustCompile(`^[a-f0-9]{64}$`)

// vaulticListSnapshots lists every snapshot of the repository, newest first.
func vaulticListSnapshots(ctx context.Context, g global.Options) ([]vaulticSnapshot, error) {
	var out bytes.Buffer
	term, ok := g.Term.(*vaulticTerminal)
	if !ok {
		return nil, errors.New("Terminal Vaultic requis pour lister les instantanés")
	}
	original := term.output
	term.output = &out
	defer func() { term.output = original }()
	if err := runSnapshots(ctx, SnapshotOptions{}, g, nil, term); err != nil {
		return nil, err
	}
	var list []vaulticSnapshot
	if err := json.Unmarshal(out.Bytes(), &list); err != nil {
		return nil, fmt.Errorf("Décodage des instantanés : %w", err)
	}
	sort.SliceStable(list, func(i, j int) bool { return list[i].Time.After(list[j].Time) })
	return list, nil
}

// vaulticForgetSnapshots removes exactly the given snapshots (complete IDs).
// It frees no space by itself: unreferenced data stays until a prune.
func vaulticForgetSnapshots(ctx context.Context, g global.Options, ids []string) error {
	if len(ids) == 0 {
		return errors.New("aucun instantané à supprimer")
	}
	for _, id := range ids {
		if !vaulticFullSnapshotID.MatchString(id) {
			return fmt.Errorf("identifiant d'instantané invalide : %q", id)
		}
	}
	return runForget(ctx, ForgetOptions{}, PruneOptions{MaxUnused: "5%"}, g, g.Term, ids)
}

// vaulticPruneRepository frees the space of data no snapshot uses any more.
func vaulticPruneRepository(ctx context.Context, g global.Options) error {
	return runPrune(ctx, PruneOptions{MaxUnused: "5%"}, g, g.Term)
}
