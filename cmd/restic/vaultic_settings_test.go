package main

import "testing"

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
