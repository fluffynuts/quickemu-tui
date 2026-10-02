package qemu

import (
	"os"
	"path/filepath"
	"reflect"
	"strings"
	"testing"
)

func TestMergeAddsOnlyMissingKeys(t *testing.T) {
	conf := "#!/usr/bin/quickemu --vm\nguest_os=\"linux\"\ngl=\"on\"\n#cpu_cores=\"2\"\n"
	defaults := []string{`gl="off"`, `cpu_cores="4"`, `ram="4G"`, "", "# note"}
	got, added := MergeDefaults(conf, defaults)
	if want := []string{`cpu_cores="4"`, `ram="4G"`}; !reflect.DeepEqual(added, want) {
		t.Fatalf("added = %v, want %v", added, want)
	}
	if !strings.Contains(got, `gl="on"`) || strings.Contains(got, `gl="off"`) {
		t.Errorf("an explicit gl=on was overridden:\n%s", got)
	}
	if !strings.HasSuffix(got, "cpu_cores=\"4\"\nram=\"4G\"\n") {
		t.Errorf("unexpected tail:\n%s", got)
	}
	if again, added := MergeDefaults(got, defaults); again != got || added != nil {
		t.Errorf("merge isn't idempotent: added %v", added)
	}
}

func TestMergeHandlesMissingTrailingNewlineAndDuplicates(t *testing.T) {
	got, added := MergeDefaults(`guest_os="linux"`, []string{`gl="off"`, `gl="on"`})
	if got != "guest_os=\"linux\"\ngl=\"off\"\n" || len(added) != 1 {
		t.Fatalf("got %q added %v", got, added)
	}
}

func TestApplyDefaultsKeepsFileMode(t *testing.T) {
	p := filepath.Join(t.TempDir(), "vm.conf")
	if err := os.WriteFile(p, []byte("guest_os=\"linux\"\n"), 0o744); err != nil {
		t.Fatal(err)
	}
	added, err := ApplyDefaults(p, []string{`gl="off"`})
	if err != nil || len(added) != 1 {
		t.Fatalf("added=%v err=%v", added, err)
	}
	st, _ := os.Stat(p)
	if st.Mode().Perm() != 0o744 {
		t.Errorf("mode = %v, want 0744", st.Mode().Perm())
	}
	if added, _ := ApplyDefaults(p, []string{`gl="off"`}); added != nil {
		t.Errorf("second apply added %v", added)
	}
	if _, err := ApplyDefaults(filepath.Join(t.TempDir(), "missing.conf"), []string{`a=1`}); err == nil {
		t.Error("expected an error for a missing conf")
	}
}

func TestValidateDefaults(t *testing.T) {
	good := [][]string{
		{`gl="off"`}, {`ram=4G`}, {`# comment`, ``, `cpu_cores="4"`},
		{`port_forwards=("8123:8123" "8888:80")`}, {`display='sdl'  # trailing note`},
		{`path="a;b|c&d"`}, // metacharacters are fine inside quotes
	}
	for _, g := range good {
		if err := ValidateDefaults(g); err != nil {
			t.Errorf("%q rejected: %v", g, err)
		}
	}
	bad := map[string][]string{
		"not an assignment": {`just words`},
		"command separator": {`gl="off"; rm -rf ~`},
		"background":        {`gl=off & sleep 9`},
		"pipe":              {`gl=off | cat`},
		"substitution":      {`gl=$(whoami)`},
		"quoted subst":      {`gl="$(whoami)"`},
		"backtick":          {"gl=`id`"},
		"redirect":          {`gl=off > /etc/x`},
		"unbalanced quote":  {`gl="off`},
		"duplicate key":     {`gl="off"`, `gl="on"`},
	}
	for name, lines := range bad {
		if err := ValidateDefaults(lines); err == nil {
			t.Errorf("%s: %q accepted", name, lines)
		}
	}
}
