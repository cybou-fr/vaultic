package main

import (
	"strings"

	"github.com/restic/restic/internal/backend/all"
	"github.com/restic/restic/internal/backend/location"
)

// This catalog mirrors internal/backend/all/all.go for the pinned restic engine.
// It describes available backends, not an assertion that remote credentials are
// present or that a given server is reachable from this computer.
type vaulticBackendInfo struct {
	Scheme, Label, Example, Description, Requirements string
	External                                          bool
}

var vaulticBackendCatalog = []vaulticBackendInfo{
	{"local", "Local — disque, USB ou NAS monté", "local:/chemin/vers/backup", "Dossier local, disque externe ou partage réseau monté par le système.", "Aucun compte cloud nécessaire. Vérifiez les droits d'accès et l'espace libre.", false},
	{"sftp", "SFTP — serveur SSH", "sftp:utilisateur@serveur:/backup", "Stockage distant accessible par le protocole SFTP.", "Client SSH installé et connexion SSH par clé déjà configurée.", true},
	{"rest", "REST — serveur restic", "rest:https://serveur.example/backup", "Serveur implémentant le protocole REST de restic.", "Serveur REST disponible. Identifiants éventuels via RESTIC_REST_USERNAME et RESTIC_REST_PASSWORD.", false},
	{"s3", "S3 — AWS, MinIO, fournisseurs compatibles", "s3:s3.amazonaws.com/mon-bucket/vaultic", "Stockage objet S3, y compris de nombreux fournisseurs compatibles.", "Credentials AWS ou paramètres S3 configurés dans l'environnement. Coûts et compatibilité selon le fournisseur.", false},
	{"b2", "Backblaze B2", "b2:mon-bucket:vaultic", "Stockage objet Backblaze B2 via son API native.", "B2_ACCOUNT_ID et B2_ACCOUNT_KEY configurés dans l'environnement.", false},
	{"azure", "Azure Blob Storage", "azure:mon-container:vaultic", "Conteneur Azure Blob Storage.", "AZURE_ACCOUNT_NAME avec clé, SAS ou une autre méthode d'authentification Azure configurée.", false},
	{"gs", "Google Cloud Storage", "gs:mon-bucket:vaultic", "Bucket de stockage Google Cloud.", "Identité Google Cloud configurée (ADC / compte de service et permissions bucket).", false},
	{"swift", "OpenStack Swift", "swift:mon-container:/vaultic", "Stockage objet OpenStack Swift.", "Identifiants Keystone v3 (mot de passe ou Application Credential) saisis dans Vaultic et gardés en mémoire, ou variables OS_* du système.", false},
	{"rclone", "rclone — autres fournisseurs", "rclone:remote:vaultic", "Passerelle vers les remotes pris en charge par rclone (selon configuration).", "Programme rclone installé et remote configuré séparément.", true},
}

func vaulticBackendLabels() []string {
	labels := make([]string, 0, len(vaulticBackendCatalog))
	for _, item := range vaulticBackendCatalog {
		labels = append(labels, item.Label)
	}
	return labels
}

func vaulticBackendByLabel(label string) (vaulticBackendInfo, bool) {
	for _, item := range vaulticBackendCatalog {
		if item.Label == label {
			return item, true
		}
	}
	return vaulticBackendInfo{}, false
}

func vaulticBackendByScheme(scheme string) (vaulticBackendInfo, bool) {
	for _, item := range vaulticBackendCatalog {
		if item.Scheme == scheme {
			return item, true
		}
	}
	return vaulticBackendInfo{}, false
}

func vaulticBackendDescription(item vaulticBackendInfo) string {
	text := item.Description + "\nExemple : " + item.Example + "\n" + item.Requirements
	if item.External {
		text += "\nProgramme externe requis pour cette connexion (pas pour le moteur restic lui-même)."
	}
	return text
}

func vaulticBackendForAddress(address string) (vaulticBackendInfo, bool) {
	p := strings.TrimSpace(address)
	if p == "" {
		return vaulticBackendByScheme("local")
	}
	loc, err := vaulticBackendLocation(p)
	if err != nil {
		return vaulticBackendInfo{}, false
	}
	return vaulticBackendByScheme(loc)
}

func vaulticBackendLocation(address string) (string, error) {
	parsed, err := location.Parse(all.Backends(), address)
	if err != nil {
		return "", err
	}
	return parsed.Scheme, nil
}
