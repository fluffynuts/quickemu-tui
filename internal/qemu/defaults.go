package qemu

import (
	"fmt"
	"os"
	"strings"
)

// DefaultKey returns the variable a default-option line assigns, e.g. "gl" for
// `gl="off"`. Blank lines and comments have no key.
func DefaultKey(line string) (string, bool) {
	m := assignRe.FindStringSubmatch(strings.TrimSpace(line))
	if m == nil {
		return "", false
	}
	return m[1], true
}

// ValidateDefaults checks lines the user typed as default VM options. quickemu
// sources its .conf as bash, so each line must be a plain assignment that
// can't run anything: no command separators, redirections, substitutions or
// unbalanced quotes. Blank lines and #comments are fine; a key may appear once.
func ValidateDefaults(lines []string) error {
	seen := map[string]int{}
	for i, raw := range lines {
		line := strings.TrimSpace(raw)
		if line == "" || strings.HasPrefix(line, "#") {
			continue
		}
		m := assignRe.FindStringSubmatch(line)
		if m == nil {
			return fmt.Errorf("line %d: %q isn't an assignment like key=\"value\"", i+1, truncateForError(line))
		}
		if err := checkSafeValue(m[2]); err != nil {
			return fmt.Errorf("line %d (%s): %w", i+1, m[1], err)
		}
		if first, dup := seen[m[1]]; dup {
			return fmt.Errorf("line %d: %s is already set on line %d", i+1, m[1], first)
		}
		seen[m[1]] = i + 1
	}
	return nil
}

func truncateForError(s string) string {
	if r := []rune(s); len(r) > 40 {
		return string(r[:39]) + "…"
	}
	return s
}

// checkSafeValue accepts the right-hand side of an assignment only if bash
// would treat it as plain words (or an array of them): no unquoted ; & | < >
// ` ( ) or $(, and no backticks or $( even inside double quotes.
func checkSafeValue(rhs string) error {
	s := strings.TrimSpace(rhs)
	if strings.HasPrefix(s, "(") {
		if !strings.HasSuffix(s, ")") {
			return fmt.Errorf("unbalanced parentheses")
		}
		s = s[1 : len(s)-1]
	}
	r := []rune(s)
	var quote rune // 0, '\'' or '"'
	for i := 0; i < len(r); i++ {
		c := r[i]
		switch quote {
		case '\'':
			if c == '\'' {
				quote = 0
			}
		case '"':
			switch {
			case c == '\\':
				i++
			case c == '"':
				quote = 0
			case c == '`', c == '$' && i+1 < len(r) && r[i+1] == '(':
				return fmt.Errorf("command substitution isn't allowed")
			}
		default:
			switch {
			case c == '\\':
				i++
			case c == '\'' || c == '"':
				quote = c
			case c == '#' && (i == 0 || r[i-1] == ' ' || r[i-1] == '\t'):
				return nil // trailing comment
			case strings.ContainsRune(";&|<>`()", c), c == '$' && i+1 < len(r) && r[i+1] == '(':
				return fmt.Errorf("%q isn't allowed outside quotes", string(c))
			}
		}
	}
	if quote != 0 {
		return fmt.Errorf("unbalanced %c quote", quote)
	}
	return nil
}

// MergeDefaults appends the defaults whose key the conf text doesn't already
// assign, and returns the new text and the lines it added. Keys the conf does
// set (to anything, including "on" where the default says "off") are left
// alone, as are commented-out assignments' meaning: those count as unset.
func MergeDefaults(confText string, defaults []string) (string, []string) {
	have := ParseConf(confText)
	var added []string
	for _, d := range defaults {
		key, ok := DefaultKey(d)
		if !ok {
			continue // blank or comment
		}
		if _, set := have[key]; set {
			continue
		}
		have[key] = "" // a later default for the same key mustn't double up
		added = append(added, strings.TrimSpace(d))
	}
	if len(added) == 0 {
		return confText, nil
	}
	out := confText
	if out != "" && !strings.HasSuffix(out, "\n") {
		out += "\n"
	}
	return out + strings.Join(added, "\n") + "\n", added
}

// ApplyDefaults merges defaults into the conf file at path, keeping its mode
// (quickemu confs are executable). It returns the lines added.
func ApplyDefaults(path string, defaults []string) ([]string, error) {
	if len(defaults) == 0 {
		return nil, nil
	}
	st, err := os.Stat(path)
	if err != nil {
		return nil, err
	}
	data, err := os.ReadFile(path)
	if err != nil {
		return nil, err
	}
	merged, added := MergeDefaults(string(data), defaults)
	if len(added) == 0 {
		return nil, nil
	}
	if err := os.WriteFile(path, []byte(merged), st.Mode().Perm()); err != nil {
		return nil, err
	}
	return added, nil
}
