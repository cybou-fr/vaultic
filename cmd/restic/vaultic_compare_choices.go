package main

// vaulticComparisonChoices lists the snapshots that can serve as the "before"
// side of a comparison with target: the drop-down labels, the label → snapshot
// ID map and the label to preselect (the suggested base, else the first one).
// snapshots must contain at least one snapshot other than target.
func vaulticComparisonChoices(snapshots []vaulticSnapshot, target vaulticSnapshot) (options []string, ids map[string]string, initial string) {
	ids = make(map[string]string, len(snapshots))
	suggestedID := vaulticSuggestedComparisonBase(snapshots, target)
	for _, sn := range snapshots {
		if sn.ID == target.ID {
			continue
		}
		label := sn.Time.Local().Format("02/01/2006 15:04:05") + "  ·  " + sn.ShortID + "  ·  " + sn.Hostname + "  ·  " + sn.ID
		options = append(options, label)
		ids[label] = sn.ID
		if sn.ID == suggestedID {
			initial = label
		}
	}
	if initial == "" && len(options) > 0 {
		initial = options[0]
	}
	return options, ids, initial
}
