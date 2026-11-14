// Package inputconfig preserves device settings when writing the shell's KDL include.
package inputconfig

import (
	"errors"
	"fmt"
	"io"
	"os"
	"path/filepath"
	"strconv"
	"strings"
	"sync"
	"syscall"

	"github.com/Nomadcxx/sysc-shell/internal/config"
	"github.com/sblinch/kdl-go"
)

const (
	managedFile = "sysc-shell-input.kdl"
	includeLine = `include optional=true "sysc-shell-input.kdl"`
	maxBytes    = 4 << 20
)

// Section is a device's child nodes, in file order. Unknown settings survive rendering.
type Section []string

// ReadSections follows top-level includes in place; each device's last section wins.
func ReadSections(configPath string) (touchpad, mouse Section, err error) {
	stack := make(map[string]bool)
	budget := 16 << 20
	files := 0
	var walk func(string, int, bool) error
	walk = func(path string, depth int, optional bool) error {
		path, err := filepath.Abs(path)
		if err != nil {
			return err
		}
		if depth > 32 || files >= 256 || budget <= 0 {
			return fmt.Errorf("input config: include limit at %s", path)
		}
		if stack[path] {
			return fmt.Errorf("input config: recursive include %s", path)
		}
		data, err := readFile(path)
		if optional && errors.Is(err, os.ErrNotExist) {
			return nil
		}
		if err != nil {
			return fmt.Errorf("input config: %w", err)
		}
		files++
		budget -= len(data)
		if budget < 0 {
			return fmt.Errorf("input config: includes exceed byte limit")
		}
		doc, err := kdl.Parse(strings.NewReader(string(data)))
		if err != nil {
			return fmt.Errorf("%s: %w", path, err)
		}
		stack[path] = true
		defer delete(stack, path)
		for _, node := range doc.Nodes {
			switch node.Name.ValueString() {
			case "include":
				if len(node.Arguments) != 1 || len(node.Children) != 0 {
					return fmt.Errorf("%s: invalid include", path)
				}
				name, ok := node.Arguments[0].Value.(string)
				if !ok {
					return fmt.Errorf("%s: include path must be a string", path)
				}
				optional := false
				for key, value := range node.Properties {
					flag, ok := value.Value.(bool)
					if key != "optional" || !ok {
						return fmt.Errorf("%s: invalid include property %s", path, key)
					}
					optional = flag
				}
				if name == "~" || strings.HasPrefix(name, "~/") {
					home, err := os.UserHomeDir()
					if err != nil {
						return err
					}
					name = filepath.Join(home, strings.TrimPrefix(strings.TrimPrefix(name, "~"), "/"))
				}
				if !filepath.IsAbs(name) {
					name = filepath.Join(filepath.Dir(path), name)
				}
				if err := walk(name, depth+1, optional); err != nil {
					return err
				}
			case "input":
				for _, device := range node.Children {
					name := device.Name.ValueString()
					if name != "touchpad" && name != "mouse" {
						continue
					}
					if len(device.Arguments) != 0 || device.Properties.Len() != 0 {
						return fmt.Errorf("%s: invalid %s section", path, name)
					}
					var section Section
					for _, child := range device.Children {
						section = append(section, child.String())
					}
					if name == "touchpad" {
						touchpad = section
					} else {
						mouse = section
					}
				}
			}
		}
		return nil
	}
	err = walk(configPath, 0, false)
	return
}

func readFile(path string) ([]byte, error) {
	f, err := os.OpenFile(path, os.O_RDONLY|syscall.O_NONBLOCK, 0)
	if err != nil {
		return nil, err
	}
	defer f.Close()
	st, err := f.Stat()
	if err != nil {
		return nil, err
	}
	if !st.Mode().IsRegular() || st.Size() > maxBytes {
		return nil, fmt.Errorf("%s: expected regular file under %d bytes", path, maxBytes)
	}
	b, err := io.ReadAll(io.LimitReader(f, maxBytes+1))
	if len(b) > maxBytes {
		return nil, fmt.Errorf("%s: file too large", path)
	}
	return b, err
}

// Render overlays only explicitly set fields, retaining all other device nodes.
func Render(user Section, set config.Pointer, device string) string {
	lines := append(Section(nil), user...)
	replace := func(name, value string, present bool) {
		if !present {
			return
		}
		kept := lines[:0]
		for _, line := range lines {
			doc, err := kdl.Parse(strings.NewReader(line))
			if err == nil && len(doc.Nodes) == 1 && doc.Nodes[0].Name.ValueString() == name {
				continue
			}
			kept = append(kept, line)
		}
		lines = kept
		if value != "" {
			lines = append(lines, value)
		}
	}
	for _, flag := range []struct {
		name  string
		value *bool
	}{
		{"tap", set.Tap}, {"natural-scroll", set.NaturalScroll}, {"left-handed", set.LeftHanded},
		{"dwt", set.DWT}, {"disabled-on-external-mouse", set.DisabledOnExternalMouse},
	} {
		value := ""
		if flag.value != nil && *flag.value {
			value = flag.name
		}
		replace(flag.name, value, flag.value != nil)
	}
	for _, number := range []struct {
		name  string
		value *float64
	}{{"accel-speed", set.AccelSpeed}, {"scroll-factor", set.ScrollFactor}} {
		if number.value != nil {
			replace(number.name, number.name+" "+strconv.FormatFloat(*number.value, 'f', -1, 64), true)
		}
	}
	for _, text := range []struct {
		name  string
		value *string
	}{{"accel-profile", set.AccelProfile}, {"scroll-method", set.ScrollMethod}, {"click-method", set.ClickMethod}} {
		if text.value != nil {
			replace(text.name, text.name+" "+strconv.Quote(*text.value), true)
		}
	}
	return device + " {\n    " + strings.Join(lines, "\n    ") + "\n}"
}

// Effective decodes the controls from the winning sections; unshown nodes remain opaque.
func Effective(touchpad, mouse Section) (config.Input, error) {
	var in config.Input
	for _, dev := range []struct {
		section Section
		out     *config.Pointer
	}{{touchpad, &in.Touchpad}, {mouse, &in.Mouse}} {
		p := dev.out
		off1, off2, off3, off4, off5 := false, false, false, false, false
		zero, one := 0.0, 1.0
		p.Tap, p.NaturalScroll, p.LeftHanded, p.DWT, p.DisabledOnExternalMouse = &off1, &off2, &off3, &off4, &off5
		p.AccelSpeed, p.ScrollFactor = &zero, &one
		if p == &in.Mouse {
			p.Tap, p.DWT, p.DisabledOnExternalMouse = nil, nil, nil
		}
		seen := make(map[string]bool)
		for _, line := range dev.section {
			doc, err := kdl.Parse(strings.NewReader(line))
			if err != nil {
				return in, err
			}
			if len(doc.Nodes) != 1 {
				return in, fmt.Errorf("input config: expected one setting per node")
			}
			n := doc.Nodes[0]
			name := n.Name.ValueString()
			if seen[name] {
				return in, fmt.Errorf("input config: duplicate setting %s", name)
			}
			seen[name] = true
			var flag **bool
			var number **float64
			var text **string
			switch name {
			case "tap":
				flag = &p.Tap
			case "natural-scroll":
				flag = &p.NaturalScroll
			case "left-handed":
				flag = &p.LeftHanded
			case "dwt":
				flag = &p.DWT
			case "disabled-on-external-mouse":
				flag = &p.DisabledOnExternalMouse
			case "accel-speed":
				number = &p.AccelSpeed
			case "scroll-factor":
				number = &p.ScrollFactor
			case "accel-profile":
				text = &p.AccelProfile
			case "scroll-method":
				text = &p.ScrollMethod
			case "click-method":
				text = &p.ClickMethod
			default:
				continue
			}
			if (n.Properties.Len() != 0 && name != "scroll-factor") || len(n.Children) != 0 {
				return in, fmt.Errorf("input config: invalid %s", name)
			}
			if name == "scroll-factor" {
				for key, value := range n.Properties {
					if key != "horizontal" && key != "vertical" {
						return in, fmt.Errorf("input config: invalid scroll-factor property %s", key)
					}
					switch value.ResolvedValue().(type) {
					case string, bool, nil:
						return in, fmt.Errorf("input config: scroll-factor %s must be a number", key)
					}
					v, err := strconv.ParseFloat(fmt.Sprint(value.ResolvedValue()), 64)
					if err != nil || v < -100 || v > 100 {
						return in, fmt.Errorf("input config: scroll-factor %s must be in -100..100", key)
					}
				}
				if len(n.Arguments) == 0 {
					continue
				}
			}
			if flag != nil {
				if len(n.Arguments) != 0 {
					return in, fmt.Errorf("input config: %s takes no arguments", name)
				}
				on := true
				*flag = &on
			} else {
				if len(n.Arguments) != 1 {
					return in, fmt.Errorf("input config: %s needs one argument", name)
				}
				if number != nil {
					switch n.Arguments[0].ResolvedValue().(type) {
					case string, bool, nil:
						return in, fmt.Errorf("input config: %s must be a number", name)
					}
					v, err := strconv.ParseFloat(fmt.Sprint(n.Arguments[0].ResolvedValue()), 64)
					if err != nil {
						return in, fmt.Errorf("input config: invalid %s: %w", name, err)
					}
					*number = &v
				} else {
					v, ok := n.Arguments[0].Value.(string)
					if !ok {
						return in, fmt.Errorf("input config: %s must be a string", name)
					}
					*text = &v
				}
			}
		}
	}
	return in, config.ValidateInput(in)
}

// SupportedVersion requires released Niri 26.04 for optional includes.
func SupportedVersion(version string) bool {
	version = strings.TrimSpace(strings.TrimPrefix(strings.TrimSpace(version), "niri "))
	fields := strings.Fields(version)
	if len(fields) == 0 {
		return false
	}
	parts := strings.Split(fields[0], ".")
	if len(parts) < 2 || len(parts) > 3 {
		return false
	}
	var nums [3]int
	for i, s := range parts {
		n, err := strconv.Atoi(s)
		if err != nil || n < 0 {
			return false
		}
		nums[i] = n
	}
	return nums[0] > 26 || nums[0] == 26 && nums[1] >= 4
}

// HasTouchpad reads udev's cached properties without opening input devices.
func HasTouchpad(root string) bool {
	entries, err := os.ReadDir(root)
	if err != nil {
		return false
	}
	for _, entry := range entries {
		if entry.IsDir() {
			continue
		}
		data, err := readFile(filepath.Join(root, entry.Name()))
		if err != nil {
			continue
		}
		for _, line := range strings.Split(string(data), "\n") {
			if line == "E:ID_INPUT_TOUCHPAD=1" {
				return true
			}
		}
	}
	return false
}

var writeMu sync.Mutex

// Write validates before touching either file. An untouched device gets no section.
func Write(configDir string, in config.Input, touchpad, mouse Section) error {
	writeMu.Lock()
	defer writeMu.Unlock()
	if in.IsZero() {
		return nil
	}
	if err := config.ValidateInput(in); err != nil {
		return err
	}
	path := filepath.Join(configDir, "config.kdl")
	sidecar := filepath.Join(configDir, managedFile)
	if info, err := os.Lstat(sidecar); err == nil && !info.Mode().IsRegular() {
		return fmt.Errorf("input config: managed include must be a regular file: %s", sidecar)
	} else if err != nil && !errors.Is(err, os.ErrNotExist) {
		return err
	}
	// Re-read before writing so an edit made while the page was open survives.
	var err error
	touchpad, mouse, err = ReadSections(path)
	if err != nil {
		return err
	}
	if _, err := Effective(touchpad, mouse); err != nil {
		return err
	}
	data, err := readFile(path)
	if err != nil {
		return err
	}
	updated, err := includeLast(string(data))
	if err != nil {
		return err
	}
	var sections []string
	if !in.Touchpad.IsZero() {
		sections = append(sections, Render(touchpad, in.Touchpad, "touchpad"))
	}
	if !in.Mouse.IsZero() {
		sections = append(sections, Render(mouse, in.Mouse, "mouse"))
	}
	body := "// generated by sysc-shell; edit input settings in Settings\ninput {\n    " + strings.ReplaceAll(strings.Join(sections, "\n"), "\n", "\n    ") + "\n}\n"
	if _, err := kdl.Parse(strings.NewReader(body)); err != nil {
		return err
	}
	if err := atomicWrite(sidecar, []byte(body)); err != nil {
		return err
	}
	if updated == string(data) {
		return nil
	}
	return atomicWrite(path, []byte(updated))
}

// includeLast only removes whole, top-level managed lines. Other syntax is
// refused so that editing settings cannot reformat or delete the user's KDL.
func includeLast(src string) (string, error) {
	doc, err := kdl.Parse(strings.NewReader(src))
	if err != nil {
		return "", err
	}
	count := 0
	for _, n := range doc.Nodes {
		if n.Name.ValueString() == "include" && len(n.Arguments) == 1 && n.Arguments[0].ValueString() == managedFile {
			count++
		}
	}
	lines := strings.SplitAfter(src, "\n")
	var out strings.Builder
	removed := 0
	for i, line := range lines {
		trim := strings.TrimSpace(line)
		if trim == includeLine || trim == `include "sysc-shell-input.kdl"` {
			// ponytail: parse a prefix per managed line (O(n*m)); m is normally
			// one. Use parser source spans if we ever manage many directives.
			prefix, err := kdl.Parse(strings.NewReader(strings.Join(lines[:i], "") + "\nsysc-input-boundary\n"))
			if err == nil && len(prefix.Nodes) > 0 && prefix.Nodes[len(prefix.Nodes)-1].Name.ValueString() == "sysc-input-boundary" {
				removed++
				continue
			}
		}
		out.WriteString(line)
	}
	if removed != count {
		return "", fmt.Errorf("input config: put the managed include on its own line before editing input settings (found %d, removable %d)", count, removed)
	}
	result := out.String()
	if result != "" && !strings.HasSuffix(result, "\n") {
		result += "\n"
	}
	return result + includeLine + "\n", nil
}

func atomicWrite(path string, data []byte) error {
	// Follow dotfile symlinks instead of replacing them with regular files.
	if _, err := os.Lstat(path); err == nil {
		resolved, err := filepath.EvalSymlinks(path)
		if err != nil {
			return err
		}
		path = resolved
	} else if !errors.Is(err, os.ErrNotExist) {
		return err
	}
	mode := os.FileMode(0600)
	if info, err := os.Stat(path); err == nil {
		mode = info.Mode().Perm()
	} else if !errors.Is(err, os.ErrNotExist) {
		return err
	}
	f, err := os.CreateTemp(filepath.Dir(path), ".sysc-input-*")
	if err != nil {
		return err
	}
	defer os.Remove(f.Name())
	defer f.Close()
	if err = f.Chmod(mode); err != nil {
		return err
	}
	if _, err = f.Write(data); err != nil {
		return err
	}
	if err = f.Sync(); err != nil {
		return err
	}
	if err = f.Close(); err != nil {
		return err
	}
	return os.Rename(f.Name(), path)
}
