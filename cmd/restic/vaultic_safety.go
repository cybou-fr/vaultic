package main

import (
	"context"
	"errors"
	"fmt"
	"net/url"
	"os"
	"path"
	"path/filepath"
	"regexp"
	"runtime"
	"sort"
	"strings"

	"github.com/restic/restic/internal/data"
	"github.com/restic/restic/internal/restic"
	"github.com/restic/restic/internal/walker"
)

func vaulticCheckRestoreTree(ctx context.Context, repo restic.BlobLoader, tree *restic.ID, selected func(string, bool) (bool, bool)) error {
	if tree == nil {
		return errors.New("Cet instantané n’a pas d’arborescence")
	}
	names := make(map[string]map[string]bool)
	return walker.Walk(ctx, repo, *tree, walker.WalkVisitor{
		ProcessNode: func(_ restic.ID, location string, node *data.Node, err error) error {
			if err != nil || node == nil {
				return err
			}
			isDir := node.Type == data.NodeTypeDir
			include, descend := selected(location, isDir)
			if !include && !descend {
				if isDir {
					return walker.ErrSkipNode
				}
				return nil
			}
			if !vaulticSafeRestoreName(node.Name) {
				return fmt.Errorf("Nom incompatible avec une restauration sûre : %q", node.Name)
			}
			if !isDir && node.Type != data.NodeTypeFile {
				return fmt.Errorf("Seuls les fichiers ordinaires et les dossiers peuvent être restaurés : %q", location)
			}
			parent := path.Dir(location)
			key := node.Name
			if runtime.GOOS == "windows" {
				key = strings.ToUpper(key)
			}
			if names[parent] == nil {
				names[parent] = make(map[string]bool)
			}
			if names[parent][key] {
				return fmt.Errorf("Deux éléments utilisent le même nom de destination : %q", location)
			}
			names[parent][key] = true
			return nil
		},
		LeaveDir: func(directory string) error {
			delete(names, directory)
			return nil
		},
	})
}

func vaulticSafeRestoreName(name string) bool {
	if name == "." || name == ".." || name != path.Base(name) || !filepath.IsLocal(name) || strings.ContainsRune(name, 0) {
		return false
	}
	return runtime.GOOS != "windows" ||
		(name == strings.TrimRight(name, ". ") && !strings.ContainsAny(name, `<>:"/\|?*`) &&
			!strings.ContainsFunc(name, func(r rune) bool { return r < 32 }))
}

var vaulticLogURL = regexp.MustCompile(`(?i)https?://[^\s"'<>\x60]+`)

func vaulticRedactOperation(password string) func(string) string {
	secrets := []string{password}
	for _, name := range []string{
		"RESTIC_PASSWORD", "RESTIC_REST_PASSWORD", "AWS_SECRET_ACCESS_KEY", "AWS_SESSION_TOKEN",
		"AZURE_ACCOUNT_KEY", "AZURE_CLIENT_SECRET", "B2_ACCOUNT_KEY", "GOOGLE_OAUTH_ACCESS_TOKEN",
		"OS_PASSWORD", "OS_AUTH_TOKEN", "OS_APPLICATION_CREDENTIAL_SECRET", "ST_AUTH_TOKEN",
	} {
		secrets = append(secrets, os.Getenv(name))
	}
	return vaulticRedactor(secrets)
}

func vaulticRedactor(secrets []string) func(string) string {
	secrets = append([]string(nil), secrets...)
	sort.SliceStable(secrets, func(i, j int) bool { return len(secrets[i]) > len(secrets[j]) })
	var replacements []string
	for _, secret := range secrets {
		if secret != "" {
			replacements = append(replacements, secret, "[masqué]")
		}
	}
	redactor := strings.NewReplacer(replacements...)
	return func(message string) string {
		return vaulticLogURL.ReplaceAllStringFunc(redactor.Replace(message), func(raw string) string {
			parsed, err := url.Parse(raw)
			if err != nil {
				return "[adresse masquée]"
			}
			parsed.User = nil
			parsed.RawQuery, parsed.Fragment, parsed.RawFragment = "", "", ""
			parsed.ForceQuery = false
			return parsed.String()
		})
	}
}
