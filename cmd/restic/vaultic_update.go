package main

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net/http"
	"strconv"
	"strings"
	"time"
)

// Version of the application. The build sets them with -ldflags; a plain
// "go build" or "go run" reports "dev".
var (
	vaulticVersion = "dev"
	vaulticCommit  = ""
)

// vaulticAuthor is shown in the About section.
const vaulticAuthor = "Stanislav SAVELIEV"

// vaulticVersionLine is the text shown in the About section.
func vaulticVersionLine() string {
	line := vaulticVersion
	if vaulticCommit != "" {
		line += " (" + vaulticCommit + ")"
	}
	return line
}

const (
	vaulticReleasesAPI  = "https://api.github.com/repos/cybou-fr/vaultic/releases/latest"
	vaulticReleasesPage = "https://github.com/cybou-fr/vaultic/releases"
	vaulticUpdateLimit  = 1 << 20 // the answer is read up to 1 MiB
)

// vaulticParseVersion reads "v1.2.3", "1.2.3" or "1.2.3-rc1" as numbers; a
// pre-release suffix is ignored for ordering but reported by ok=true.
func vaulticParseVersion(text string) (parts [3]int, ok bool) {
	text = strings.TrimPrefix(strings.TrimSpace(text), "v")
	if i := strings.IndexAny(text, "-+ "); i >= 0 {
		text = text[:i]
	}
	fields := strings.Split(text, ".")
	if len(fields) == 0 || len(fields) > 3 || text == "" {
		return parts, false
	}
	for i, f := range fields {
		n, err := strconv.Atoi(f)
		if err != nil || n < 0 {
			return parts, false
		}
		parts[i] = n
	}
	return parts, true
}

// vaulticNewerVersion reports whether candidate is a higher version than
// current. An unparsable current version ("dev") never counts as outdated.
func vaulticNewerVersion(candidate, current string) bool {
	c, ok1 := vaulticParseVersion(candidate)
	cur, ok2 := vaulticParseVersion(current)
	if !ok1 || !ok2 {
		return false
	}
	for i := range c {
		if c[i] != cur[i] {
			return c[i] > cur[i]
		}
	}
	// 0.6.0 is newer than 0.6.0-beta: the final release follows its pre-releases.
	return vaulticIsPrerelease(current) && !vaulticIsPrerelease(candidate)
}

// vaulticIsPrerelease reports whether a version carries a "-beta"/"-rc1" suffix.
func vaulticIsPrerelease(text string) bool {
	text = strings.TrimPrefix(strings.TrimSpace(text), "v")
	return strings.Contains(text, "-")
}

// vaulticUpdateResult is the answer of the manual update check.
type vaulticUpdateResult struct {
	Latest    string
	URL       string
	Available bool
	None      bool // no release has been published yet
}

// vaulticCheckForUpdate asks the releases API once. It sends no data about
// the user: an anonymous request with a fixed user agent. It never downloads
// or installs anything.
func vaulticCheckForUpdate(ctx context.Context, client *http.Client, api, current string) (vaulticUpdateResult, error) {
	ctx, cancel := context.WithTimeout(ctx, 10*time.Second)
	defer cancel()
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, api, nil)
	if err != nil {
		return vaulticUpdateResult{}, err
	}
	req.Header.Set("Accept", "application/vnd.github+json")
	req.Header.Set("User-Agent", "Vaultic-update-check")
	resp, err := client.Do(req)
	if err != nil {
		return vaulticUpdateResult{}, fmt.Errorf(vaulticT("Impossible de joindre le serveur de versions : %w"), err)
	}
	defer resp.Body.Close()
	switch {
	case resp.StatusCode == http.StatusNotFound:
		return vaulticUpdateResult{None: true, URL: vaulticReleasesPage}, nil
	case resp.StatusCode == http.StatusForbidden || resp.StatusCode == http.StatusTooManyRequests:
		return vaulticUpdateResult{}, errors.New(vaulticT("Le serveur de versions limite les demandes. Réessayez plus tard."))
	case resp.StatusCode != http.StatusOK:
		return vaulticUpdateResult{}, fmt.Errorf(vaulticT("Réponse inattendue du serveur de versions (%d)."), resp.StatusCode)
	}
	var release struct {
		Tag  string `json:"tag_name"`
		URL  string `json:"html_url"`
		Pre  bool   `json:"prerelease"`
		Draf bool   `json:"draft"`
	}
	if err := json.NewDecoder(io.LimitReader(resp.Body, vaulticUpdateLimit)).Decode(&release); err != nil {
		return vaulticUpdateResult{}, errors.New(vaulticT("Réponse illisible du serveur de versions."))
	}
	if release.Tag == "" || release.Draf {
		return vaulticUpdateResult{None: true, URL: vaulticReleasesPage}, nil
	}
	url := release.URL
	if !strings.HasPrefix(url, "https://github.com/") {
		url = vaulticReleasesPage // never send the user to an address the answer chose freely
	}
	return vaulticUpdateResult{Latest: release.Tag, URL: url, Available: vaulticNewerVersion(release.Tag, current)}, nil
}
