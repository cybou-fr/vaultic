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
	{"local", vaulticL("Local — disque, USB ou NAS monté"), vaulticL("local:/chemin/vers/backup"), vaulticL("Dossier local, disque externe ou partage réseau monté par le système."), vaulticL("Aucun compte cloud nécessaire. Vérifiez les droits d'accès et l'espace libre."), false},
	{"sftp", vaulticL("SFTP — serveur SSH"), vaulticL("sftp:utilisateur@serveur:/backup"), vaulticL("Stockage distant accessible par le protocole SFTP."), vaulticL("Client SSH installé et connexion SSH par clé déjà configurée."), true},
	{"rest", vaulticL("REST — serveur restic"), vaulticL("rest:https://serveur.example/backup"), vaulticL("Serveur implémentant le protocole REST de restic."), vaulticL("Serveur REST disponible. Identifiants éventuels via RESTIC_REST_USERNAME et RESTIC_REST_PASSWORD."), false},
	{"s3", vaulticL("S3 — AWS, MinIO, fournisseurs compatibles"), vaulticL("s3:s3.amazonaws.com/mon-bucket/vaultic"), vaulticL("Stockage objet S3, y compris de nombreux fournisseurs compatibles."), vaulticL("Credentials AWS ou paramètres S3 configurés dans l'environnement. Coûts et compatibilité selon le fournisseur."), false},
	{"b2", vaulticL("Backblaze B2"), vaulticL("b2:mon-bucket:vaultic"), vaulticL("Stockage objet Backblaze B2 via son API native."), vaulticL("B2_ACCOUNT_ID et B2_ACCOUNT_KEY configurés dans l'environnement."), false},
	{"azure", vaulticL("Azure Blob Storage"), vaulticL("azure:mon-container:vaultic"), vaulticL("Conteneur Azure Blob Storage."), vaulticL("AZURE_ACCOUNT_NAME avec clé, SAS ou une autre méthode d'authentification Azure configurée."), false},
	{"gs", vaulticL("Google Cloud Storage"), vaulticL("gs:mon-bucket:vaultic"), vaulticL("Bucket de stockage Google Cloud."), vaulticL("Identité Google Cloud configurée (ADC / compte de service et permissions bucket)."), false},
	{"swift", vaulticL("OpenStack Swift"), vaulticL("swift:mon-container:/vaultic"), vaulticL("Stockage objet OpenStack Swift."), vaulticL("Identifiants Keystone v3 (mot de passe ou Application Credential) saisis dans Vaultic et gardés en mémoire, ou variables OS_* du système."), false},
	{"rclone", vaulticL("rclone — autres fournisseurs"), vaulticL("rclone:remote:vaultic"), vaulticL("Passerelle vers les remotes pris en charge par rclone (selon configuration)."), vaulticL("Programme rclone installé et remote configuré séparément."), true},
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
	text := item.Description + vaulticT("\nExemple : ") + item.Example + "\n" + item.Requirements
	if item.External {
		text += vaulticT("\nProgramme externe requis pour cette connexion (pas pour le moteur restic lui-même).")
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
