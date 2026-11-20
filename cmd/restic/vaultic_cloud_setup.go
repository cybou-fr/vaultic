package main

import (
	"errors"
	"fmt"
	"strings"
)

// The wizard builds a repository location from public, non-secret fields.
// Authentication tokens are not part of the URI and are never serialized.
type vaulticCloudSetup struct {
	Name, Address, Password string
	Auth                    vaulticCloudAuth
}
type vaulticCloudFormData struct {
	Scheme, Name, Password, Endpoint, Bucket, Prefix, Region string
	Account, AccessID, Secret                                string
	AzureSAS                                                 bool
}

func vaulticCloudField(value string) (string, error) {
	v := strings.TrimSpace(value)
	if len(v) > 256 || strings.ContainsAny(v, "\x00\r\n?#") {
		return "", errors.New("Utilisez un champ sans paramètres URL ni caractères de contrôle.")
	}
	return v, nil
}

func vaulticBuildCloudSetup(form vaulticCloudFormData) (vaulticCloudSetup, error) {
	var result vaulticCloudSetup
	name, err := vaulticValidateSavedName(form.Name)
	if err != nil {
		return result, err
	}
	if form.Password == "" {
		return result, errors.New("Saisissez le mot de passe du dépôt restic.")
	}
	bucket, err := vaulticCloudField(form.Bucket)
	if err != nil {
		return result, err
	}
	if bucket == "" || strings.ContainsAny(bucket, ":/@\\ ") {
		return result, errors.New("Indiquez un nom de bucket/conteneur valide, sans barre oblique.")
	}
	prefix, err := vaulticCloudField(form.Prefix)
	if err != nil {
		return result, err
	}
	prefix = strings.Trim(prefix, "/")
	for _, segment := range strings.Split(prefix, "/") {
		if segment == "." || segment == ".." || segment == "" && prefix != "" {
			return result, errors.New("Chemin du dépôt invalide : évitez les segments . et ...")
		}
	}
	credentials := vaulticCloudAuth{Scheme: form.Scheme, AccessID: strings.TrimSpace(form.AccessID), Secret: form.Secret, AccountName: strings.TrimSpace(form.Account), Region: strings.TrimSpace(form.Region), AzureSAS: form.AzureSAS}
	if err := credentials.validate(); err != nil {
		return result, err
	}
	switch form.Scheme {
	case "s3":
		endpoint, err := vaulticCloudEndpoint(form.Endpoint)
		if err != nil {
			return result, err
		}
		result.Address = "s3:" + endpoint + "/" + bucket
		if prefix != "" {
			result.Address += "/" + prefix
		}
	case "b2":
		result.Address = "b2:" + bucket + ":" + prefix
	case "azure":
		result.Address = "azure:" + bucket + ":" + prefix
	default:
		return result, fmt.Errorf("L'assistant de connexion %q n'est pas encore disponible.", form.Scheme)
	}
	if err := vaulticValidateDepot(result.Address); err != nil {
		return vaulticCloudSetup{}, err
	}
	result.Name = name
	result.Password = form.Password
	result.Auth = credentials
	return result, nil
}

// vaulticCloudEndpoint validates an S3 endpoint host (optionally host:port).
func vaulticCloudEndpoint(raw string) (string, error) {
	endpoint, err := vaulticCloudField(raw)
	if err != nil {
		return "", err
	}
	endpoint = strings.TrimPrefix(endpoint, "https://")
	if endpoint == "" || strings.HasPrefix(endpoint, "-") || strings.ContainsAny(endpoint, "/ @\\") || strings.Contains(endpoint, "://") {
		return "", errors.New("Indiquez le nom de l'endpoint S3 (ex. s3.amazonaws.com ou minio.exemple.fr:9000).")
	}
	return endpoint, nil
}
