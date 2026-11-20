package main

import (
	"encoding/json"
	"testing"
)

func TestVaulticMigrateDepots(t *testing.T) {
	settings := vaulticSettings{Repository: "local:depot", Sources: []string{"documents"}}
	settings.migrateDepots()
	if len(settings.Depots) != 1 || settings.Depots[0].Location != settings.Repository || len(settings.Sources) != 1 {
		t.Fatalf("legacy settings migration: %+v", settings)
	}
	settings.Depots = append(settings.Depots, vaulticDepot{Name: "Second", Location: "rest:https://example.com/archive"})
	settings.Repository = settings.Depots[1].Location
	settings.migrateDepots()
	if len(settings.Depots) != 2 || settings.Repository != settings.Depots[1].Location {
		t.Fatalf("active depot was not retained: %+v", settings)
	}
}

func TestVaulticRejectsDepotCredentials(t *testing.T) {
	for _, address := range []string{
		"rest:https://user:password@example.com/archive",
		"rest:https://example.com/archive?token=secret",
		"sftp://user:password@example.com/archive",
		"s3:user:password@example.com/archive",
	} {
		if vaulticValidateDepot(address) == nil {
			t.Fatalf("accepted an address containing credentials: %q", address)
		}
	}
}

func TestVaulticMigrateBackupProfiles(t *testing.T) {
	settings := vaulticSettings{Sources: []string{"documents"}}
	index := settings.migrateBackupProfiles()
	if index != 0 || len(settings.Profiles) != 1 || settings.Profiles[0].Sources[0] != "documents" {
		t.Fatalf("legacy sources were not retained: %+v", settings)
	}
	settings.Profiles = append(settings.Profiles, vaulticBackupProfile{Name: "Photos", Sources: []string{"images"}})
	settings.ActiveProfile = "Photos"
	if index := settings.migrateBackupProfiles(); index != 1 || settings.Profiles[index].Sources[0] != "images" {
		t.Fatalf("saved profile was not selected: %+v", settings)
	}
}

func TestVaulticProfileExclusionsSurviveJSONSerialization(t *testing.T) {
	input := vaulticSettings{
		Profiles: []vaulticBackupProfile{{
			Name:     "Documents",
			Sources:  []string{"documents"},
			Excludes: []string{"**/node_modules", "*.tmp"},
		}},
	}
	data, err := json.Marshal(input)
	if err != nil {
		t.Fatal(err)
	}
	var decoded vaulticSettings
	if err := json.Unmarshal(data, &decoded); err != nil {
		t.Fatal(err)
	}
	if len(decoded.Profiles) != 1 || len(decoded.Profiles[0].Excludes) != 2 ||
		decoded.Profiles[0].Excludes[0] != "**/node_modules" {
		t.Fatalf("règles perdues : %+v", decoded.Profiles)
	}
}
