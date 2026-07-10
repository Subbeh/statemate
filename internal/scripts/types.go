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
	FreqDaily                     // run at most once per day
	FreqWeekly                    // run at most once per week
	FreqMonthly                   // run at most once per month
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
	case FreqDaily:
		return "daily"
	case FreqWeekly:
		return "weekly"
	case FreqMonthly:
		return "monthly"
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
	Profile     string
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

// Script naming format: <order>-<name>.<ext>#<freq>#<timing>[#template][#profile:<name>]
// Examples:
//   01-setup.sh#once#before
//   02-cleanup.sh#always#after
//   03-render.sh#onchange#before#template
//   04-init.sh#once#before#profile:arch
//   manual-task.sh (no attributes = manual)
var orderPattern = regexp.MustCompile(`^(\d+)-(.+)$`)

func ParseScriptName(filename string) (name string, freq Frequency, timing Timing, template bool, profile string, order int) {
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
		lower := strings.ToLower(attr)
		switch {
		case lower == "once":
			freq = FreqOnce
		case lower == "onchange":
			freq = FreqOnchange
		case lower == "always":
			freq = FreqAlways
		case lower == "daily":
			freq = FreqDaily
		case lower == "weekly":
			freq = FreqWeekly
		case lower == "monthly":
			freq = FreqMonthly
		case lower == "before":
			timing = TimingBefore
		case lower == "after":
			timing = TimingAfter
		case lower == "template":
			template = true
		case strings.HasPrefix(lower, "profile:"):
			profile = strings.TrimPrefix(attr, "profile:")
		}
	}

	return name, freq, timing, template, profile, order
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

func (s Scripts) ByProfile(profileName string) Scripts {
	var result Scripts
	for _, script := range s {
		if script.Profile == "" || script.Profile == profileName {
			result = append(result, script)
		}
	}
	return result
}
