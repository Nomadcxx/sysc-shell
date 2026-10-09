package theming

import (
	"fmt"
	"os"
	"strings"
)

// valuedLine is one setting in a user's config whose key the shell owns and
// whose value it changes: a terminal's opacity. A directive owns one exact
// line, so a new value would read as a user edit; a valued line instead
// records the hash of the line it last wrote, which still tells its own
// rewrite apart from a user's edit.
type valuedLine struct {
	file         string // user config path
	key          string // matched with directiveKeyMatches; "name=" for name = value
	line         string // the exact line to write now
	section      string // ini/toml section the line lives under ("" = top level)
	returnConfig bool   // Lua assignment must precede a final `return config`
}

func valuedOwnershipKey(v valuedLine) string {
	return "valued:" + hash([]byte(v.file+"\x00"+v.key))
}

// ensureValuedLine writes v.line as the only line for v.key. The first time it
// takes the key, any user lines for it are replaced after a one-time backup:
// owning the setting is what the user asked for. Once it has written the key,
// a line it did not write is a user edit and is reported unless force.
// It never creates the file; the template's include directive does that.
func ensureValuedLine(v valuedLine, force bool) error {
	b, err := os.ReadFile(v.file)
	if os.IsNotExist(err) {
		return nil
	}
	if err != nil {
		return err
	}
	lines := strings.Split(string(b), "\n")
	if inlineTable(lines, v.section) {
		return fmt.Errorf("%w: %s defines [%s] inline", ErrUserModified, v.file, v.section)
	}
	written := stateHash(valuedOwnershipKey(v))
	want := strings.TrimSpace(v.line)
	ours, theirs := 0, 0
	keep := make([]string, 0, len(lines))
	inSection := sectionTracker(v.section)
	for _, ln := range lines {
		trim := strings.TrimSpace(ln)
		if !inSection(ln) || !directiveKeyMatches(trim, v.key) {
			keep = append(keep, ln)
			continue
		}
		if written != "" && hash([]byte(trim)) == written {
			ours++
		} else {
			theirs++
		}
	}
	if ours == 1 && theirs == 0 && written == hash([]byte(want)) {
		return nil
	}
	if theirs > 0 && written != "" && !force {
		return fmt.Errorf("%w: %s in %s", ErrUserModified, v.key, v.file)
	}
	if theirs > 0 {
		if err := backupUserFileOnce(v.file, b); err != nil {
			return err
		}
	}
	d := directive{file: v.file, line: v.line, section: v.section, returnConfig: v.returnConfig}
	content := directiveContent([]byte(strings.Join(keep, "\n")), keep, d)
	if err := writeDirectiveConfig(v.file, content); err != nil {
		return err
	}
	return rememberHash(valuedOwnershipKey(v), hash([]byte(want)))
}

// removeValuedLine deletes the line the shell last wrote for v.key and forgets
// it. A user's own line for the key is left alone.
func removeValuedLine(v valuedLine) error {
	written := stateHash(valuedOwnershipKey(v))
	if written == "" {
		return nil
	}
	b, err := os.ReadFile(v.file)
	if os.IsNotExist(err) {
		return rememberHash(valuedOwnershipKey(v), "")
	}
	if err != nil {
		return err
	}
	lines := strings.Split(string(b), "\n")
	keep := make([]string, 0, len(lines))
	inSection := sectionTracker(v.section)
	for _, ln := range lines {
		trim := strings.TrimSpace(ln)
		if inSection(ln) && directiveKeyMatches(trim, v.key) && hash([]byte(trim)) == written {
			continue
		}
		keep = append(keep, ln)
	}
	if len(keep) != len(lines) {
		if err := writeDirectiveConfig(v.file, []byte(strings.Join(keep, "\n"))); err != nil {
			return err
		}
	}
	return rememberHash(valuedOwnershipKey(v), "")
}

// sectionTracker returns a function fed every line in order that reports
// whether the line sits in section. An empty section is the whole file. Keys
// repeat across sections -- foot has alpha in [colors-dark] and
// [colors-light] -- and only the named one is the shell's.
func sectionTracker(section string) func(line string) bool {
	current := ""
	return func(line string) bool {
		if section == "" {
			return true
		}
		if name, ok := sectionHeader(line); ok {
			current = name
			return false
		}
		return current == section
	}
}

// inlineTable reports a TOML table written as a dotted key or an inline
// table before any header. Appending a [section] header beside either makes
// the config fail to load, so the shell refuses rather than write one.
func inlineTable(lines []string, section string) bool {
	if section == "" {
		return false
	}
	for _, ln := range lines {
		if _, ok := sectionHeader(ln); ok {
			return false
		}
		trim := strings.TrimSpace(ln)
		if strings.HasPrefix(trim, section+".") || directiveKeyMatches(trim, section+"=") {
			return true
		}
	}
	return false
}
