// Package qemu holds the UI-free logic for quickemu-tui: quickemu .conf files,
// the QEMU HMP monitor socket, qemu-img snapshots and the quickemu CLI.
package qemu

import (
	"regexp"
	"strings"
	"unicode"
)

var assignRe = regexp.MustCompile(`^\s*([A-Za-z_][A-Za-z0-9_]*)=(.*)$`)

// ParseConf reads the simple key="value" assignments from a quickemu .conf
// (which is a bash file). The shebang, comments and anything that isn't a plain
// assignment are ignored.
func ParseConf(text string) map[string]string {
	result := make(map[string]string)
	for _, line := range strings.Split(text, "\n") {
		m := assignRe.FindStringSubmatch(strings.TrimRight(line, "\r"))
		if m == nil {
			continue
		}
		result[m[1]] = UnquoteValue(m[2])
	}
	return result
}

// characters a backslash escapes inside bash double quotes
const dqEscapable = "$`\"\\\n"

// UnquoteValue turns the right-hand side of a bash assignment into its string
// value, following bash's rules for a single word: double quotes (only $ ` " \
// are escapable inside), single quotes (fully literal), bare backslash escapes
// and concatenation. It stops at unquoted whitespace or a leading #. No
// expansion happens: "$HOME" stays literally "$HOME". Bash arrays and
// unbalanced quotes are returned verbatim.
func UnquoteValue(raw string) string {
	raw = strings.TrimSpace(raw)
	if strings.HasPrefix(raw, "(") {
		return raw
	}
	r := []rune(raw)
	var out strings.Builder
	started := false
	for i := 0; i < len(r); {
		ch := r[i]
		if unicode.IsSpace(ch) || (ch == '#' && !started) {
			break
		}
		started = true
		switch {
		case ch == '\'':
			end := -1
			for j := i + 1; j < len(r); j++ {
				if r[j] == '\'' {
					end = j
					break
				}
			}
			if end < 0 {
				return raw
			}
			out.WriteString(string(r[i+1 : end]))
			i = end + 1
		case ch == '"':
			i++
			for i < len(r) && r[i] != '"' {
				if r[i] == '\\' && i+1 < len(r) && strings.ContainsRune(dqEscapable, r[i+1]) {
					out.WriteRune(r[i+1])
					i += 2
				} else {
					out.WriteRune(r[i])
					i++
				}
			}
			if i >= len(r) {
				return raw
			}
			i++
		case ch == '\\' && i+1 < len(r):
			out.WriteRune(r[i+1])
			i += 2
		default:
			out.WriteRune(ch)
			i++
		}
	}
	return out.String()
}
