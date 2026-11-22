package main

import (
	"go/ast"
	"go/parser"
	"go/token"
	"os"
	"path/filepath"
	"regexp"
	"sort"
	"strconv"
	"strings"
	"testing"
)

// literalKeys returns every message passed to vaulticT in the application's
// own sources (constant concatenations are folded), with the file that uses it.
func literalKeys(t *testing.T) map[string]string {
	t.Helper()
	files, err := filepath.Glob("vaultic_*.go")
	if err != nil {
		t.Fatal(err)
	}
	keys := map[string]string{}
	for _, file := range files {
		if strings.HasSuffix(file, "_test.go") || file == "vaultic_i18n.go" || file == "vaultic_i18n_en.go" {
			continue
		}
		f, err := parser.ParseFile(token.NewFileSet(), file, nil, 0)
		if err != nil {
			t.Fatal(err)
		}
		ast.Inspect(f, func(n ast.Node) bool {
			call, ok := n.(*ast.CallExpr)
			if !ok {
				return true
			}
			if id, ok := call.Fun.(*ast.Ident); !ok || (id.Name != "vaulticT" && id.Name != "vaulticL") || len(call.Args) != 1 {
				return true
			}
			if key, ok := foldString(call.Args[0]); ok {
				keys[key] = file
			} else if file != "vaultic_i18n_tables.go" && file != "vaultic_storage_setup_ui.go" { // these translate the values of their tables
				t.Errorf("%s: vaulticT needs a string literal (or a concatenation of literals)", file)
			}
			return true
		})
	}
	return keys
}

func foldString(e ast.Expr) (string, bool) {
	switch v := e.(type) {
	case *ast.BasicLit:
		if v.Kind != token.STRING {
			return "", false
		}
		s, err := strconv.Unquote(v.Value)
		return s, err == nil
	case *ast.BinaryExpr:
		if v.Op != token.ADD {
			return "", false
		}
		l, ok1 := foldString(v.X)
		r, ok2 := foldString(v.Y)
		return l + r, ok1 && ok2
	case *ast.ParenExpr:
		return foldString(v.X)
	}
	return "", false
}

var verbRe = regexp.MustCompile(`%[+#\- 0-9.]*[a-zA-Z%]`)

// Messages that read the same in both languages.
var sameInEnglish = map[string]bool{"Vaultic": true}

func TestVaulticEnglishCatalogCoversEveryMessage(t *testing.T) {
	keys := literalKeys(t)
	var missing []string
	for key := range keys {
		if sameInEnglish[key] {
			continue
		}
		if vaulticEnglish[key] == "" {
			missing = append(missing, key)
		}
	}
	sort.Strings(missing)
	if dump := os.Getenv("VAULTIC_I18N_DUMP"); dump != "" {
		if err := os.WriteFile(dump, []byte(strings.Join(missing, "\n---\n")), 0o644); err != nil {
			t.Fatal(err)
		}
	}
	if len(missing) > 0 {
		show := missing
		if len(show) > 15 {
			show = show[:15]
		}
		t.Fatalf("%d message(s) have no English translation, for example:\n%s\n(set VAULTIC_I18N_DUMP=<file> to list them all)", len(missing), strings.Join(show, "\n"))
	}
}

func TestVaulticEnglishCatalogHasNoStaleOrBrokenEntries(t *testing.T) {
	keys := literalKeys(t)
	for french, english := range vaulticEnglish {
		if _, used := keys[french]; !used {
			t.Errorf("catalog entry is not used by any message: %q", french)
		}
		if strings.TrimSpace(english) == "" {
			t.Errorf("empty translation for %q", french)
			continue
		}
		fv, ev := verbRe.FindAllString(french, -1), verbRe.FindAllString(english, -1)
		sort.Strings(fv)
		sort.Strings(ev)
		if strings.Join(fv, "|") != strings.Join(ev, "|") {
			t.Errorf("format verbs differ between French and English:\n  %q\n  %q", french, english)
		}
		if strings.Count(french, "\n") != strings.Count(english, "\n") {
			t.Errorf("line breaks differ between French and English:\n  %q\n  %q", french, english)
		}
	}
}

func TestVaulticLanguageSelectionAndFallback(t *testing.T) {
	defer vaulticSetLanguage(vaulticLangFR)
	cases := []struct{ setting, locale, want string }{
		{"auto", "fr-FR", "fr"}, {"auto", "fr_CA", "fr"}, {"auto", "en-US", "en"}, {"", "de-DE", "en"},
		{"fr", "en-US", "fr"}, {"en", "fr-FR", "en"}, {"EN", "", "en"}, {"klingon", "fr", "fr"},
	}
	for _, c := range cases {
		if got := vaulticResolveLanguage(c.setting, c.locale); got != c.want {
			t.Errorf("setting %q locale %q: got %s want %s", c.setting, c.locale, got, c.want)
		}
	}
	vaulticSetLanguage(vaulticLangEN)
	if vaulticLanguage() != "en" {
		t.Fatal("English not selected")
	}
	if got := vaulticT("texte français sans traduction inventé pour le test"); got != "texte français sans traduction inventé pour le test" {
		t.Fatalf("a missing translation must fall back to the French text: %q", got)
	}
	vaulticSetLanguage("zz")
	if vaulticLanguage() != "fr" {
		t.Fatal("an unknown language falls back to French")
	}
	if got := vaulticT("Sauvegarder maintenant"); got != "Sauvegarder maintenant" {
		t.Fatalf("French must be returned untouched: %q", got)
	}
}
