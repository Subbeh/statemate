package source

import (
	"io/fs"
	"os"
	"path/filepath"
	"strconv"
	"strings"

	gitignore "github.com/sabhiram/go-gitignore"
	"github.com/subbeh/statemate/internal/config"
)

type Scanner struct {
	targetBase       string
	repoRoot         string
	dirConfigs       map[string]*config.DirConfig
	ignoreFiles      map[string]*gitignore.GitIgnore
	templateRenderer config.TemplateRenderer
}

func NewScanner(targetBase, repoRoot string) *Scanner {
	return NewScannerWithRenderer(targetBase, repoRoot, nil)
}

func NewScannerWithRenderer(targetBase, repoRoot string, renderer config.TemplateRenderer) *Scanner {
	s := &Scanner{
		targetBase:       targetBase,
		repoRoot:         repoRoot,
		dirConfigs:       make(map[string]*config.DirConfig),
		ignoreFiles:      make(map[string]*gitignore.GitIgnore),
		templateRenderer: renderer,
	}
	if repoRoot != "" {
		s.loadIgnoreFile(repoRoot, repoRoot)
	}
	return s
}

func (s *Scanner) Scan(sources []string) (*Tree, error) {
	tree := &Tree{}

	for _, source := range sources {
		if err := s.scanSource(source, tree); err != nil {
			return nil, err
		}
	}

	tree.CheckConflicts()
	return tree, nil
}

func (s *Scanner) scanSource(sourceDir string, tree *Tree) error {
	dirCfg, _ := config.LoadDirConfigRaw(sourceDir, s.templateRenderer)
	if dirCfg != nil {
		s.dirConfigs[sourceDir] = dirCfg
	}

	return filepath.WalkDir(sourceDir, func(path string, d fs.DirEntry, err error) error {
		if err != nil {
			return err
		}

		relToSource, err := filepath.Rel(sourceDir, path)
		if err != nil {
			return err
		}

		if relToSource == "." {
			s.loadIgnoreFile(sourceDir, sourceDir)
			return nil
		}

		if s.shouldSkip(d.Name()) {
			if d.IsDir() {
				return filepath.SkipDir
			}
			return nil
		}

		if d.IsDir() {
			s.loadIgnoreFile(sourceDir, path)
		}

		if s.isIgnored(sourceDir, relToSource, d.IsDir()) {
			if d.IsDir() {
				return filepath.SkipDir
			}
			return nil
		}

		info, err := d.Info()
		if err != nil {
			return err
		}

		entry := s.buildEntry(sourceDir, path, relToSource, info, dirCfg)
		tree.AddEntry(entry)

		return nil
	})
}

func (s *Scanner) buildEntry(sourceDir, fullPath, relPath string, info os.FileInfo, dirCfg *config.DirConfig) *Entry {
	name, attrs := ParseAttrs(filepath.Base(fullPath))

	_, sourceDirAttrs := ParseAttrs(filepath.Base(sourceDir))
	parentAttrs := s.getParentAttrs(filepath.Dir(relPath))
	parentAttrs.Merge(sourceDirAttrs)
	attrs.Merge(parentAttrs)

	if dirCfg != nil {
		if dirCfg.Profile != "" && attrs.Profile == "" {
			attrs.Profile = dirCfg.Profile
		}
		if dirCfg.Owner != "" && attrs.Owner == "" {
			attrs.Owner = dirCfg.Owner
		}
		if dirCfg.Group != "" && attrs.Group == "" {
			attrs.Group = dirCfg.Group
		}
		if dirCfg.Perm != "" && attrs.Perm == 0 {
			if p, err := strconv.ParseUint(dirCfg.Perm, 8, 32); err == nil {
				attrs.Perm = uint32(p)
			}
		}
	}

	targetPath := s.resolveTarget(relPath, name, dirCfg)

	return &Entry{
		SourcePath: fullPath,
		TargetPath: targetPath,
		RelPath:    relPath,
		Name:       name,
		Attrs:      attrs,
		IsDir:      info.IsDir(),
		Mode:       info.Mode(),
	}
}

func (s *Scanner) resolveTarget(relPath, name string, dirCfg *config.DirConfig) string {
	parts := strings.Split(relPath, string(filepath.Separator))
	cleanParts := make([]string, 0, len(parts))

	for _, p := range parts {
		baseName, _ := ParseAttrs(p)
		cleanParts = append(cleanParts, baseName)
	}

	if len(cleanParts) > 0 {
		cleanParts[len(cleanParts)-1] = name
	}

	targetBase := s.targetBase
	if dirCfg != nil {
		if dirCfg.TargetBase != "" {
			targetBase = expandHome(dirCfg.TargetBase)
		} else if len(cleanParts) > 0 {
			firstDir := cleanParts[0]
			if override, ok := dirCfg.Targets[firstDir]; ok {
				targetBase = override
				cleanParts = cleanParts[1:]
			}
		}
	}

	return filepath.Join(targetBase, filepath.Join(cleanParts...))
}

func expandHome(path string) string {
	if path == "~" {
		home, _ := os.UserHomeDir()
		return home
	}
	if strings.HasPrefix(path, "~/") {
		home, _ := os.UserHomeDir()
		return filepath.Join(home, path[2:])
	}
	return path
}

func (s *Scanner) getParentAttrs(relDir string) Attrs {
	var attrs Attrs
	parts := strings.Split(relDir, string(filepath.Separator))
	for _, p := range parts {
		_, partAttrs := ParseAttrs(p)
		if partAttrs.Profile != "" {
			attrs.Profile = partAttrs.Profile
		}
		if partAttrs.Perm != 0 {
			attrs.Perm = partAttrs.Perm
		}
		if partAttrs.Owner != "" {
			attrs.Owner = partAttrs.Owner
		}
		if partAttrs.Group != "" {
			attrs.Group = partAttrs.Group
		}
	}
	return attrs
}

func (s *Scanner) shouldSkip(name string) bool {
	switch name {
	case ".git", ".mate.yaml", ".mate.yml", ".mate.toml", ".matescripts", ".mateignore":
		return true
	}
	return false
}

func (s *Scanner) loadIgnoreFile(_, dir string) {
	ignorePath := filepath.Join(dir, ".mateignore")
	if _, err := os.Stat(ignorePath); err != nil {
		return
	}

	gi, err := gitignore.CompileIgnoreFile(ignorePath)
	if err != nil {
		return
	}

	s.ignoreFiles[dir] = gi
}

func (s *Scanner) isIgnored(sourceDir, relPath string, isDir bool) bool {
	fullPath := filepath.Join(sourceDir, relPath)

	for dir, gi := range s.ignoreFiles {
		if !strings.HasPrefix(fullPath+"/", dir+"/") && dir != s.repoRoot {
			continue
		}

		var relToIgnoreDir string
		var err error
		if dir == s.repoRoot && s.repoRoot != "" {
			relToIgnoreDir, err = filepath.Rel(s.repoRoot, fullPath)
		} else {
			relToIgnoreDir, err = filepath.Rel(dir, fullPath)
		}
		if err != nil {
			continue
		}

		checkPath := relToIgnoreDir
		if isDir {
			checkPath = relToIgnoreDir + "/"
		}

		if gi.MatchesPath(checkPath) {
			return true
		}
	}

	return false
}

func (s *Scanner) DirConfig(sourceDir string) *config.DirConfig {
	return s.dirConfigs[sourceDir]
}
