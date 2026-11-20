package main

import (
	"strings"
	"testing"
)

func TestVaulticCloudWizardBuildsValidLocationsWithoutSecrets(t *testing.T) {
	tests := []struct {
		form    vaulticCloudFormData
		address string
	}{
		{vaulticCloudFormData{Scheme: "s3", Name: "Archives", Password: "repo-pass", Endpoint: "https://s3.amazonaws.com", Bucket: "my-bucket", Prefix: "vaultic", AccessID: "access-id", Secret: "s3-secret", Region: "eu-west-3"}, "s3:s3.amazonaws.com/my-bucket/vaultic"},
		{vaulticCloudFormData{Scheme: "b2", Name: "B2", Password: "repo-pass", Bucket: "samplebucket", Prefix: "vaultic", AccessID: "account-id", Secret: "b2-secret"}, "b2:samplebucket:vaultic"},
		{vaulticCloudFormData{Scheme: "azure", Name: "Azure", Password: "repo-pass", Bucket: "container", Prefix: "data", Account: "example", Secret: "azure-secret", AzureSAS: true}, "azure:container:data"},
	}
	for _, tc := range tests {
		result, err := vaulticBuildCloudSetup(tc.form)
		if err != nil {
			t.Fatalf("%s: %v", tc.form.Scheme, err)
		}
		if result.Address != tc.address {
			t.Fatalf("want %q got %q", tc.address, result.Address)
		}
		if strings.Contains(result.Address, tc.form.Secret) || strings.Contains(result.Address, tc.form.Password) {
			t.Fatal("credentials exposed in repository address")
		}
		if result.Auth.Secret != tc.form.Secret {
			t.Fatal("cloud secret lost")
		}
	}
}

func TestVaulticCloudWizardRejectsInvalidAndIncompleteInputs(t *testing.T) {
	tests := []vaulticCloudFormData{
		{Scheme: "s3", Name: "A", Password: "x", Endpoint: "http://unsafe.example", Bucket: "bucket"},
		{Scheme: "s3", Name: "A", Password: "x", Endpoint: "host/path", Bucket: "bucket"},
		{Scheme: "s3", Name: "A", Password: "x", Endpoint: "host", Bucket: "bad/bucket"},
		{Scheme: "s3", Name: "A", Password: "x", Endpoint: "host", Bucket: "bucket", Prefix: "../escape"},
		{Scheme: "s3", Name: "A", Password: "x", Endpoint: "host", Bucket: "bucket", AccessID: "partial"},
		{Scheme: "b2", Name: "B", Password: "x", Bucket: "short", Secret: "partial"},
		{Scheme: "azure", Name: "B", Password: "x", Bucket: "container", Secret: "partial"},
		{Scheme: "azure", Name: "B", Password: "", Bucket: "container"},
		{Scheme: "xyz", Name: "B", Password: "x", Bucket: "container"},
	}
	for _, tc := range tests {
		if _, err := vaulticBuildCloudSetup(tc); err == nil {
			t.Errorf("invalid cloud setup accepted: %+v", tc)
		}
	}
}
