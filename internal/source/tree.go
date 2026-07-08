package source

import (
	"os"
)

type Entry struct {
	SourcePath string
	TargetPath string
	RelPath    string
	Name       string
	Attrs      Attrs
	IsDir      bool
	Mode       os.FileMode

	// Generated entries have no source file - content is provided directly
	Generated        bool
	GeneratedContent string
}

type Tree struct {
	Entries  []*Entry
	Conflicts []Conflict
}

type Conflict struct {
	TargetPath string
	Sources    []string
}

func (t *Tree) AddEntry(e *Entry) {
	t.Entries = append(t.Entries, e)
}

func (t *Tree) CheckConflicts() {
	targets := make(map[string][]string)
	for _, e := range t.Entries {
		if !e.IsDir {
			targets[e.TargetPath] = append(targets[e.TargetPath], e.SourcePath)
		}
	}

	for target, sources := range targets {
		if len(sources) > 1 {
			t.Conflicts = append(t.Conflicts, Conflict{
				TargetPath: target,
				Sources:    sources,
			})
		}
	}
}

func (t *Tree) HasConflicts() bool {
	return len(t.Conflicts) > 0
}

func (t *Tree) FilterByProfile(profile string) *Tree {
	filtered := &Tree{}
	for _, e := range t.Entries {
		if e.Attrs.Profile == "" || e.Attrs.Profile == profile {
			filtered.Entries = append(filtered.Entries, e)
		}
	}
	return filtered
}

func (t *Tree) Files() []*Entry {
	var files []*Entry
	for _, e := range t.Entries {
		if !e.IsDir {
			files = append(files, e)
		}
	}
	return files
}

func (t *Tree) Dirs() []*Entry {
	var dirs []*Entry
	for _, e := range t.Entries {
		if e.IsDir {
			dirs = append(dirs, e)
		}
	}
	return dirs
}
