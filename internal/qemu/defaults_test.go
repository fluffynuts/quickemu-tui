package qemu

import (
	"bufio"
	"fmt"
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

func TestSetConfValueReplacesOrAppends(t *testing.T) {
	for _, tc := range []struct{ in, line, want string }{
		{"a=\"1\"\nram=\"4G\"\n", `ram="8G"`, "a=\"1\"\nram=\"8G\"\n"},
		{"ram=\"4G\"\nram=\"2G\"\n", `ram="8G"`, "ram=\"8G\"\n"},
		{"# ram=\"4G\"\na=1", `ram="8G"`, "# ram=\"4G\"\na=1\nram=\"8G\"\n"},
		{"", `ram="8G"`, "ram=\"8G\"\n"},
	} {
		if got := SetConfValue(tc.in, tc.line); got != tc.want {
			t.Errorf("SetConfValue(%q, %q) = %q, want %q", tc.in, tc.line, got, tc.want)
		}
	}
}

func TestRAMChoicesAndAutoTiers(t *testing.T) {
	for _, tc := range []struct {
		gib  int64
		want []int
	}{
		{32, []int{4, 8, 12, 16}},
		{31, []int{4, 8, 12, 16}}, // "roughly" half: 15.5 is close enough to 16
		{28, []int{4, 8, 12}},     // 16 would be 57%
		{64, []int{4, 8, 12, 16, 20, 24, 28, 32}},
		{8, []int{4}},
		{6, nil},
	} {
		got := RAMChoices(tc.gib << 30)
		if fmt.Sprint(got) != fmt.Sprint(tc.want) {
			t.Errorf("RAMChoices(%dGiB) = %v, want %v", tc.gib, got, tc.want)
		}
	}
	for have, want := range map[int]int{64: 16, 32: 16, 31: 8, 16: 8, 8: 4, 4: 2, 3: 1, 1: 1} {
		if got := AutoCoreTiers[AutoTierFor(AutoCoreTiers, have)].Value; got != want {
			t.Errorf("auto cores for %d host CPUs = %d, want %d", have, got, want)
		}
	}
	for have, want := range map[int]int{128: 32, 64: 16, 32: 8, 16: 8, 15: 4, 8: 4, 7: 2} {
		if got := AutoRAMTiers[AutoTierFor(AutoRAMTiers, have)].Value; got != want {
			t.Errorf("auto RAM for %dGB host = %dG, want %dG", have, got, want)
		}
	}
}

func TestParseMemTotal(t *testing.T) {
	sc := bufio.NewScanner(strings.NewReader("MemFree:  100 kB\nMemTotal:       32768000 kB\n"))
	got, err := parseMemTotal(sc)
	if err != nil || got != 32768000*1024 {
		t.Fatalf("got %d, %v", got, err)
	}
}
