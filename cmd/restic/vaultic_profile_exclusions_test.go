package main

import (
	"strings"
	"testing"
)

func TestVaulticProfileExclusionEditor(t *testing.T) {
	rules, err := vaulticParseExclusions("# Commentaire\n **/node_modules \n\n*.tmp\r\n**/node_modules\n")
	if err != nil {
		t.Fatal(err)
	}
	if len(rules) != 2 || rules[0] != "**/node_modules" || rules[1] != "*.tmp" {
		t.Fatalf("règles obtenues : %#v", rules)
	}
}
func TestVaulticRejectsInvalidExclusions(t *testing.T) {
	for _, invalid := range []string{"[", "a\x00b", strings.Repeat("a", vaulticMaxExclusionBytes+1)} {
		if _, err := vaulticParseExclusions(invalid); err == nil {
			t.Fatalf("motif invalide accepté : %.20q", invalid)
		}
	}
}
func TestVaulticProfileExclusionRuleLimit(t *testing.T) {
	var lines []string
	for i := 0; i <= vaulticMaxExclusionRules; i++ {
		lines = append(lines, "cache_"+string(rune(0x1000+i)))
	}
	if _, err := vaulticParseExclusions(strings.Join(lines, "\n")); err == nil {
		t.Fatal("nombre de règles non limité")
	}
}
