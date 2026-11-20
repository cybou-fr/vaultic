package main

import (
	"reflect"
	"testing"
)

func TestVaulticRenameSavedDepotOnlyChangesDisplayName(t *testing.T) {
	input := []vaulticDepot{{Name: "Ancien", Location: "s3:bucket/restic"}, {Name: "Second", Location: "local:drive"}}
	renamed, err := vaulticRenameSavedDepot(input, "s3:bucket/restic", " Nouveau ")
	if err != nil {
		t.Fatal(err)
	}
	if renamed[0].Name != "Nouveau" || renamed[0].Location != input[0].Location || input[0].Name != "Ancien" {
		t.Fatalf("la modification a altéré la localisation ou l'original: %v / %v", renamed, input)
	}
	if _, err := vaulticRenameSavedDepot(input, input[0].Location, "SECOND"); err == nil {
		t.Fatal("nom dupliqué accepté")
	}
	if _, err := vaulticRenameSavedDepot(input, "absent", "Test"); err == nil {
		t.Fatal("dépôt absent accepté")
	}
}

func TestVaulticForgetSavedDepotDoesNotTouchOtherEntries(t *testing.T) {
	input := []vaulticDepot{{Name: "Un", Location: "repo-one"}, {Name: "Deux", Location: "repo-two"}}
	out, active, err := vaulticForgetSavedDepot(input, "repo-one", "repo-one")
	if err != nil {
		t.Fatal(err)
	}
	if active != "repo-two" || len(out) != 1 || out[0].Location != "repo-two" || len(input) != 2 {
		t.Fatalf("sélection incorrecte: %v, %q", out, active)
	}
	out, active, err = vaulticForgetSavedDepot(out, active, "repo-two")
	if err != nil || len(out) != 0 || active != "" {
		t.Fatalf("dernier dépôt: %v %q %v", out, active, err)
	}
}

func TestVaulticDuplicateAndDeleteProfiles(t *testing.T) {
	input := []vaulticBackupProfile{
		{Name: "Documents", Sources: []string{"docs"}, Excludes: []string{"*.tmp"}},
		{Name: "Photos", Sources: []string{"images"}},
	}
	copyProfiles, idx, err := vaulticDuplicateSavedProfile(input, 0, "Documents bis")
	if err != nil || idx != 2 {
		t.Fatalf("duplication: %v %d", err, idx)
	}
	if !reflect.DeepEqual(copyProfiles[idx].Sources, input[0].Sources) || !reflect.DeepEqual(copyProfiles[idx].Excludes, input[0].Excludes) {
		t.Fatal("la duplication a perdu des sources ou des exclusions")
	}
	copyProfiles[idx].Sources[0] = "other"
	copyProfiles[idx].Excludes[0] = "*.log"
	if input[0].Sources[0] != "docs" || input[0].Excludes[0] != "*.tmp" {
		t.Fatal("la copie partage des tranches avec l'original")
	}
	remaining, next, err := vaulticDeleteSavedProfile(copyProfiles, idx)
	if err != nil || next != 1 || len(remaining) != 2 {
		t.Fatalf("suppression: %d %v %v", next, remaining, err)
	}
	remaining, next, err = vaulticDeleteSavedProfile(remaining, 0)
	if err != nil || next != 0 || remaining[0].Name != "Photos" {
		t.Fatalf("profil après suppression: %d %v %v", next, remaining, err)
	}
	if _, _, err = vaulticDeleteSavedProfile(remaining, 0); err == nil {
		t.Fatal("le dernier profil a été supprimé")
	}
	if _, _, err = vaulticDuplicateSavedProfile(remaining, 0, "photos"); err == nil {
		t.Fatal("nom dupliqué accepté")
	}
}

func TestVaulticSuggestedCopyNameUniqueAndBounded(t *testing.T) {
	profiles := []vaulticBackupProfile{{Name: "Documents"}, {Name: "Copie de Documents"}}
	name := vaulticSuggestedCopyName(profiles, 0)
	if name != "Copie de Documents (2)" {
		t.Fatalf("suggestion: %q", name)
	}
	long := []vaulticBackupProfile{{Name: "Un nom de profil très très très très long"}}
	suggestion := vaulticSuggestedCopyName(long, 0)
	if _, err := vaulticValidateSavedName(suggestion); err != nil {
		t.Fatalf("suggestion trop longue: %q %v", suggestion, err)
	}
}
