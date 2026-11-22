package main

import (
	"crypto/sha256"
	"encoding/hex"
	"errors"
	"fmt"
	"path/filepath"
	"runtime"
	"sort"
	"strings"
	"time"

	"github.com/restic/restic/internal/data"
)

// Retention decides which old snapshots of a profile may be deleted. It never
// runs by itself: nothing here is called by the scheduler or after a backup.
// Deleting needs a preview of the exact snapshots, an explicit confirmation,
// and removes only those snapshots.

const (
	vaulticRetentionMax        = 1000
	vaulticRetentionPreviewTTL = 30 * time.Minute
)

// vaulticRetention is the policy of one profile, stored in vaultic.json. It
// holds counts only.
type vaulticRetention struct {
	KeepLast    int `json:"keep_last,omitempty"`
	KeepDaily   int `json:"keep_daily,omitempty"`
	KeepWeekly  int `json:"keep_weekly,omitempty"`
	KeepMonthly int `json:"keep_monthly,omitempty"`
	KeepYearly  int `json:"keep_yearly,omitempty"`
}

// vaulticRecommendedRetention keeps the last 3 snapshots, one a day for a
// week, one a week for a month and one a month for a year.
func vaulticRecommendedRetention() vaulticRetention {
	return vaulticRetention{KeepLast: 3, KeepDaily: 7, KeepWeekly: 4, KeepMonthly: 12}
}

// vaulticCloneRetention returns an independent copy (nil stays nil).
func vaulticCloneRetention(r *vaulticRetention) *vaulticRetention {
	if r == nil {
		return nil
	}
	clone := *r
	return &clone
}

func (r vaulticRetention) Validate() error {
	for _, n := range []int{r.KeepLast, r.KeepDaily, r.KeepWeekly, r.KeepMonthly, r.KeepYearly} {
		if n < 0 || n > vaulticRetentionMax {
			return fmt.Errorf(vaulticT("Chaque valeur doit être comprise entre 0 et %d."), vaulticRetentionMax)
		}
	}
	if r.KeepLast+r.KeepDaily+r.KeepWeekly+r.KeepMonthly+r.KeepYearly == 0 {
		return errors.New(vaulticT("Indiquez au moins une règle de conservation : sans règle, tout serait supprimé."))
	}
	return nil
}

// Effective is the policy handed to restic. The newest snapshot is always kept,
// whatever the rules say.
func (r vaulticRetention) Effective() data.ExpirePolicy {
	return data.ExpirePolicy{
		Last: max(r.KeepLast, 1), Daily: r.KeepDaily, Weekly: r.KeepWeekly, Monthly: r.KeepMonthly, Yearly: r.KeepYearly,
	}
}

// Describe writes the policy in French.
func (r vaulticRetention) Describe() string {
	var parts []string
	add := func(n int, singular, plural string) {
		if n > 0 {
			if n == 1 {
				parts = append(parts, "1 "+singular)
			} else {
				parts = append(parts, fmt.Sprintf(vaulticT("%d %s"), n, plural))
			}
		}
	}
	add(max(r.KeepLast, 1), vaulticT("dernier instantané"), vaulticT("derniers instantanés"))
	add(r.KeepDaily, vaulticT("par jour"), vaulticT("jours (un par jour)"))
	add(r.KeepWeekly, vaulticT("par semaine"), vaulticT("semaines (un par semaine)"))
	add(r.KeepMonthly, vaulticT("par mois"), vaulticT("mois (un par mois)"))
	add(r.KeepYearly, "par an", vaulticT("ans (un par an)"))
	return vaulticT("Conserver : ") + strings.Join(parts, ", ")
}

// vaulticRetentionItem is one snapshot in a preview.
type vaulticRetentionItem struct {
	ID      string
	ShortID string
	Time    time.Time
	Reasons []string // why it is kept; empty for snapshots to remove
}

// vaulticRetentionPreview is exactly what applying the policy would do. It is
// the only thing the deletion step accepts.
type vaulticRetentionPreview struct {
	Profile     string
	Repository  string
	Policy      vaulticRetention
	Keep        []vaulticRetentionItem
	Remove      []vaulticRetentionItem
	Others      int // snapshots of other profiles, hosts or sources: never touched
	CreatedAt   time.Time
	Fingerprint string
}

func vaulticPathsEqual(a, b string) bool {
	a, b = filepath.Clean(a), filepath.Clean(b)
	if runtime.GOOS == "windows" {
		return strings.EqualFold(a, b)
	}
	return a == b
}

func vaulticSamePathSet(a, b []string) bool {
	if len(a) != len(b) {
		return false
	}
	used := make([]bool, len(b))
	for _, x := range a {
		found := false
		for i, y := range b {
			if !used[i] && vaulticPathsEqual(x, y) {
				used[i], found = true, true
				break
			}
		}
		if !found {
			return false
		}
	}
	return true
}

// vaulticBelongsToProfile reports whether a snapshot was made by this profile:
// same host and exactly the profile's sources. Anything else is not the
// policy's business and is never listed for removal.
func vaulticBelongsToProfile(sn vaulticSnapshot, host string, sources []string) bool {
	return strings.EqualFold(sn.Hostname, host) && vaulticSamePathSet(sn.Paths, sources)
}

func vaulticRetentionFingerprint(profile, repository string, policy vaulticRetention, remove []vaulticRetentionItem) string {
	ids := make([]string, len(remove))
	for i, item := range remove {
		ids[i] = item.ID
	}
	sort.Strings(ids)
	h := sha256.New()
	fmt.Fprintf(h, "%s\x00%s\x00%+v\x00%s", profile, repository, policy, strings.Join(ids, ","))
	return hex.EncodeToString(h.Sum(nil))[:24]
}

// vaulticEvaluateRetention applies the policy to the profile's snapshots.
func vaulticEvaluateRetention(all []vaulticSnapshot, host string, sources []string, policy vaulticRetention) (keep, remove []vaulticRetentionItem, others int, err error) {
	if err := policy.Validate(); err != nil {
		return nil, nil, 0, err
	}
	byPointer := map[*data.Snapshot]vaulticSnapshot{}
	var group data.Snapshots
	for _, sn := range all {
		if !vaulticBelongsToProfile(sn, host, sources) {
			others++
			continue
		}
		made, err := data.NewSnapshot(sn.Paths, nil, sn.Hostname, sn.Time)
		if err != nil {
			return nil, nil, 0, err
		}
		byPointer[made] = sn
		group = append(group, made)
	}
	kept, removed, reasons := data.ApplyPolicy(group, policy.Effective())
	for i, made := range kept {
		sn := byPointer[made]
		item := vaulticRetentionItem{ID: sn.ID, ShortID: sn.ShortID, Time: sn.Time}
		if i < len(reasons) {
			item.Reasons = reasons[i].Matches
		}
		keep = append(keep, item)
	}
	for _, made := range removed {
		sn := byPointer[made]
		remove = append(remove, vaulticRetentionItem{ID: sn.ID, ShortID: sn.ShortID, Time: sn.Time})
	}
	// The newest snapshot of a group is never removed, whatever happened above.
	if len(keep) == 0 && len(group) > 0 {
		return nil, nil, 0, errors.New(vaulticT("la politique supprimerait tous les instantanés : refusé"))
	}
	return keep, remove, others, nil
}

// vaulticPreviewRetention builds the preview the user must look at before
// anything can be deleted.
func vaulticPreviewRetention(all []vaulticSnapshot, profile, repository, host string, sources []string, policy vaulticRetention, now time.Time) (vaulticRetentionPreview, error) {
	keep, remove, others, err := vaulticEvaluateRetention(all, host, sources, policy)
	if err != nil {
		return vaulticRetentionPreview{}, err
	}
	return vaulticRetentionPreview{
		Profile: profile, Repository: repository, Policy: policy, Keep: keep, Remove: remove, Others: others, CreatedAt: now,
		Fingerprint: vaulticRetentionFingerprint(profile, repository, policy, remove),
	}, nil
}

// vaulticConfirmRetention re-checks a preview against the repository's current
// snapshots just before deleting. It returns the IDs that may be removed, which
// are always a subset of what the preview showed.
func vaulticConfirmRetention(p vaulticRetentionPreview, current []vaulticSnapshot, host string, sources []string, now time.Time) ([]string, error) {
	if p.Fingerprint != vaulticRetentionFingerprint(p.Profile, p.Repository, p.Policy, p.Remove) {
		return nil, errors.New(vaulticT("L'aperçu a été modifié : recommencez l'aperçu."))
	}
	if now.Sub(p.CreatedAt) > vaulticRetentionPreviewTTL || now.Before(p.CreatedAt) {
		return nil, errors.New(vaulticT("L'aperçu est trop ancien : recommencez l'aperçu avant de supprimer."))
	}
	_, remove, _, err := vaulticEvaluateRetention(current, host, sources, p.Policy)
	if err != nil {
		return nil, err
	}
	now2 := map[string]bool{}
	for _, item := range remove {
		now2[item.ID] = true
	}
	present := map[string]bool{}
	for _, sn := range current {
		present[sn.ID] = true
	}
	var ids []string
	for _, item := range p.Remove {
		if !present[item.ID] {
			continue // already gone
		}
		if !now2[item.ID] {
			return nil, errors.New(vaulticT("Le dépôt a changé depuis l'aperçu : un instantané prévu pour suppression doit désormais être conservé. Recommencez l'aperçu."))
		}
		if !vaulticFullSnapshotID.MatchString(item.ID) {
			return nil, errors.New(vaulticT("Identifiant d'instantané invalide."))
		}
		ids = append(ids, item.ID)
	}
	if len(ids) == 0 {
		return nil, errors.New(vaulticT("Il n'y a plus rien à supprimer."))
	}
	return ids, nil
}

// vaulticRetentionLines formats a preview for display.
func vaulticRetentionLines(items []vaulticRetentionItem, withReasons bool) string {
	var lines []string
	for _, item := range items {
		line := item.Time.Local().Format("02/01/2006 15:04") + " · " + item.ShortID
		if withReasons && len(item.Reasons) > 0 {
			line += " · " + strings.Join(item.Reasons, ", ")
		}
		lines = append(lines, line)
	}
	return strings.Join(lines, "\n")
}
