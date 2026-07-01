package scripts

import (
	"os"
	"regexp"
	"sort"
	"strconv"
	"strings"
)

type Frequency int

const (
	FreqOnce     Frequency = iota // run once ever
	FreqOnchange                  // run when content changes
	FreqAlways                    // run on every apply
	FreqManual                    // manual only
)

func (f Frequency) String() string {
	switch f {
	case FreqOnce:
		return "once"
	case FreqOnchange:
		return "onchange"
	case FreqAlways:
		return "always"
	case FreqManual:
		return "manual"
	default:
		return "unknown"
	}
}

type Timing int

const (
	TimingBefore Timing = iota // run before apply
	TimingAfter                // run after apply
)

func (t Timing) String() string {
	switch t {
	case TimingBefore:
		return "before"
	case TimingAfter:
		return "after"
	default:
		return "unknown"
	}
}

type Script struct {
	Path        string
	Name        string
	Frequency   Frequency
	Timing      Timing
	Template    bool
	Order       int
	SourceDir   string
	ContentHash string
}

func (s *Script) IsExecutable() bool {
	info, err := os.Stat(s.Path)
	if err != nil {
		return false
	}
	return info.Mode()&0111 != 0
}

// Script naming format: <order>-<name>.<ext>#<freq>#<timing>[#template]
// Examples:
//   01-setup.sh#once#before
//   02-cleanup.sh#always#after
//   03-render.sh#onchange#before#template
//   manual-task.sh (no attributes = manual)
var orderPattern = regexp.MustCompile(`^(\d+)-(.+)$`)

func ParseScriptName(filename string) (name string, freq Frequency, timing Timing, template bool, order int) {
	parts := strings.Split(filename, "#")
	nameWithOrder := parts[0]

	// Parse order prefix if present
	if matches := orderPattern.FindStringSubmatch(nameWithOrder); matches != nil {
		order, _ = strconv.Atoi(matches[1])
		name = matches[2]
	} else {
		name = nameWithOrder
	}

	// Default to manual
	freq = FreqManual
	timing = TimingBefore

	// Parse attributes
	for _, attr := range parts[1:] {
		switch strings.ToLower(attr) {
		case "once":
			freq = FreqOnce
		case "onchange":
			freq = FreqOnchange
		case "always":
			freq = FreqAlways
		case "before":
			timing = TimingBefore
		case "after":
			timing = TimingAfter
		case "template":
			template = true
		}
	}

	// If frequency is set but timing is missing, default to before
	// (timing is already TimingBefore by default, so nothing to do)

	return name, freq, timing, template, order
}

type Scripts []*Script

func (s Scripts) Len() int      { return len(s) }
func (s Scripts) Swap(i, j int) { s[i], s[j] = s[j], s[i] }
func (s Scripts) Less(i, j int) bool {
	if s[i].Order != s[j].Order {
		return s[i].Order < s[j].Order
	}
	return s[i].Name < s[j].Name
}

func (s Scripts) Sort() {
	sort.Sort(s)
}

func (s Scripts) ByTiming(t Timing) Scripts {
	var result Scripts
	for _, script := range s {
		if script.Timing == t {
			result = append(result, script)
		}
	}
	return result
}

func (s Scripts) ByFrequency(f Frequency) Scripts {
	var result Scripts
	for _, script := range s {
		if script.Frequency == f {
			result = append(result, script)
		}
	}
	return result
}

func (s Scripts) Automatic() Scripts {
	var result Scripts
	for _, script := range s {
		if script.Frequency != FreqManual {
			result = append(result, script)
		}
	}
	return result
}
