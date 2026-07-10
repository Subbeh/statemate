package scripts

import (
	"os"
	"path/filepath"
	"testing"
)

func TestParseScriptName(t *testing.T) {
	tests := []struct {
		filename     string
		wantName     string
		wantFreq     Frequency
		wantTiming   Timing
		wantTemplate bool
		wantProfile  string
		wantOrder    int
	}{
		{"01-setup.sh#once#before", "setup.sh", FreqOnce, TimingBefore, false, "", 1},
		{"02-cleanup.sh#always#after", "cleanup.sh", FreqAlways, TimingAfter, false, "", 2},
		{"03-render.sh#onchange#before#template", "render.sh", FreqOnchange, TimingBefore, true, "", 3},
		{"10-install.sh#once#after", "install.sh", FreqOnce, TimingAfter, false, "", 10},
		{"05-prep.sh#always#before", "prep.sh", FreqAlways, TimingBefore, false, "", 5},
		{"manual-task.sh", "manual-task.sh", FreqManual, TimingBefore, false, "", 0},
		{"script", "script", FreqManual, TimingBefore, false, "", 0},
		{"100-long-name-here.zsh#onchange#after", "long-name-here.zsh", FreqOnchange, TimingAfter, false, "", 100},
		// Frequency only (timing defaults to before)
		{"01-setup.sh#once", "setup.sh", FreqOnce, TimingBefore, false, "", 1},
		{"01-setup.sh#always", "setup.sh", FreqAlways, TimingBefore, false, "", 1},
		// Profile attribute
		{"00-init.sh#once#before#profile:arch", "init.sh", FreqOnce, TimingBefore, false, "arch", 0},
		{"01-setup.sh#always#after#profile:work", "setup.sh", FreqAlways, TimingAfter, false, "work", 1},
	}

	for _, tc := range tests {
		t.Run(tc.filename, func(t *testing.T) {
			name, freq, timing, template, profile, order := ParseScriptName(tc.filename)
			if name != tc.wantName {
				t.Errorf("name = %v, want %v", name, tc.wantName)
			}
			if freq != tc.wantFreq {
				t.Errorf("frequency = %v, want %v", freq, tc.wantFreq)
			}
			if timing != tc.wantTiming {
				t.Errorf("timing = %v, want %v", timing, tc.wantTiming)
			}
			if template != tc.wantTemplate {
				t.Errorf("template = %v, want %v", template, tc.wantTemplate)
			}
			if profile != tc.wantProfile {
				t.Errorf("profile = %v, want %v", profile, tc.wantProfile)
			}
			if order != tc.wantOrder {
				t.Errorf("order = %v, want %v", order, tc.wantOrder)
			}
		})
	}
}

func TestFrequencyString(t *testing.T) {
	tests := []struct {
		freq Frequency
		want string
	}{
		{FreqOnce, "once"},
		{FreqOnchange, "onchange"},
		{FreqAlways, "always"},
		{FreqManual, "manual"},
	}

	for _, tc := range tests {
		if got := tc.freq.String(); got != tc.want {
			t.Errorf("%v.String() = %v, want %v", tc.freq, got, tc.want)
		}
	}
}

func TestTimingString(t *testing.T) {
	tests := []struct {
		timing Timing
		want   string
	}{
		{TimingBefore, "before"},
		{TimingAfter, "after"},
	}

	for _, tc := range tests {
		if got := tc.timing.String(); got != tc.want {
			t.Errorf("%v.String() = %v, want %v", tc.timing, got, tc.want)
		}
	}
}

func TestScriptsSort(t *testing.T) {
	scripts := Scripts{
		{Name: "z", Order: 10},
		{Name: "a", Order: 20},
		{Name: "b", Order: 10},
	}

	scripts.Sort()

	if scripts[0].Name != "b" || scripts[0].Order != 10 {
		t.Errorf("first should be b/10, got %s/%d", scripts[0].Name, scripts[0].Order)
	}
	if scripts[1].Name != "z" || scripts[1].Order != 10 {
		t.Errorf("second should be z/10, got %s/%d", scripts[1].Name, scripts[1].Order)
	}
	if scripts[2].Name != "a" || scripts[2].Order != 20 {
		t.Errorf("third should be a/20, got %s/%d", scripts[2].Name, scripts[2].Order)
	}
}

func TestScriptsByTiming(t *testing.T) {
	scripts := Scripts{
		{Name: "a", Timing: TimingBefore},
		{Name: "b", Timing: TimingAfter},
		{Name: "c", Timing: TimingBefore},
		{Name: "d", Timing: TimingAfter},
	}

	before := scripts.ByTiming(TimingBefore)
	if len(before) != 2 {
		t.Errorf("expected 2 before scripts, got %d", len(before))
	}

	after := scripts.ByTiming(TimingAfter)
	if len(after) != 2 {
		t.Errorf("expected 2 after scripts, got %d", len(after))
	}
}

func TestScriptsByFrequency(t *testing.T) {
	scripts := Scripts{
		{Name: "a", Frequency: FreqOnce},
		{Name: "b", Frequency: FreqAlways},
		{Name: "c", Frequency: FreqOnce},
		{Name: "d", Frequency: FreqManual},
	}

	once := scripts.ByFrequency(FreqOnce)
	if len(once) != 2 {
		t.Errorf("expected 2 once scripts, got %d", len(once))
	}

	always := scripts.ByFrequency(FreqAlways)
	if len(always) != 1 {
		t.Errorf("expected 1 always script, got %d", len(always))
	}
}

func TestScriptsAutomatic(t *testing.T) {
	scripts := Scripts{
		{Name: "a", Frequency: FreqOnce},
		{Name: "b", Frequency: FreqAlways},
		{Name: "c", Frequency: FreqManual},
		{Name: "d", Frequency: FreqOnchange},
	}

	auto := scripts.Automatic()
	if len(auto) != 3 {
		t.Errorf("expected 3 automatic scripts, got %d", len(auto))
	}

	for _, s := range auto {
		if s.Frequency == FreqManual {
			t.Errorf("automatic scripts should not include manual: %s", s.Name)
		}
	}
}

func TestDiscoverer(t *testing.T) {
	tmpDir := t.TempDir()

	scriptsDir := filepath.Join(tmpDir, ".matescripts")
	if err := os.MkdirAll(scriptsDir, 0755); err != nil {
		t.Fatal(err)
	}

	scriptPath := filepath.Join(scriptsDir, "01-test.sh#once#before")
	if err := os.WriteFile(scriptPath, []byte("#!/bin/bash\necho test"), 0755); err != nil {
		t.Fatal(err)
	}

	sourceDir := filepath.Join(tmpDir, "mysource")
	if err := os.MkdirAll(sourceDir, 0755); err != nil {
		t.Fatal(err)
	}

	sourceScriptsDir := filepath.Join(sourceDir, ".matescripts")
	if err := os.MkdirAll(sourceScriptsDir, 0755); err != nil {
		t.Fatal(err)
	}

	sourceScriptPath := filepath.Join(sourceScriptsDir, "02-cleanup.sh#always#after")
	if err := os.WriteFile(sourceScriptPath, []byte("#!/bin/bash\necho cleanup"), 0755); err != nil {
		t.Fatal(err)
	}

	discoverer := NewDiscoverer(tmpDir, []string{sourceDir})
	scripts, err := discoverer.Discover()
	if err != nil {
		t.Fatalf("Discover() error: %v", err)
	}

	if len(scripts) != 2 {
		t.Errorf("expected 2 scripts, got %d", len(scripts))
	}

	if scripts[0].Order != 1 {
		t.Errorf("expected first script order 1, got %d", scripts[0].Order)
	}
	if scripts[0].Frequency != FreqOnce {
		t.Errorf("expected first script frequency once, got %v", scripts[0].Frequency)
	}
	if scripts[1].Order != 2 {
		t.Errorf("expected second script order 2, got %d", scripts[1].Order)
	}
	if scripts[1].Timing != TimingAfter {
		t.Errorf("expected second script timing after, got %v", scripts[1].Timing)
	}
}

func TestScriptIsExecutable(t *testing.T) {
	tmpDir := t.TempDir()

	execPath := filepath.Join(tmpDir, "executable.sh")
	if err := os.WriteFile(execPath, []byte("#!/bin/bash"), 0755); err != nil {
		t.Fatal(err)
	}

	nonExecPath := filepath.Join(tmpDir, "nonexec.sh")
	if err := os.WriteFile(nonExecPath, []byte("#!/bin/bash"), 0644); err != nil {
		t.Fatal(err)
	}

	execScript := &Script{Path: execPath}
	if !execScript.IsExecutable() {
		t.Error("expected executable script to be executable")
	}

	nonExecScript := &Script{Path: nonExecPath}
	if nonExecScript.IsExecutable() {
		t.Error("expected non-executable script to not be executable")
	}
}
