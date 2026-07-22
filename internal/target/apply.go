package target

import (
	"fmt"
	"os"
	"path/filepath"
	"strings"

	"github.com/subbeh/statemate/internal/encrypt"
	"github.com/subbeh/statemate/internal/source"
	"github.com/subbeh/statemate/internal/state"
	"github.com/subbeh/statemate/internal/template"
	"golang.org/x/term"
)

type Applier struct {
	db       *state.DB
	tmplCtx  *template.Context
	enc      *encrypt.AgeEncryptor
	dryRun   bool
	force    bool
	verbose  int
}

type ApplyResult struct {
	Applied  int
	Skipped  int
	Imported int
	Errors   []error
	DryRun   bool
}


type ChangeStatus int

const (
	StatusUnchanged ChangeStatus = iota
	StatusNew
	StatusModified
	StatusConflict
	StatusStateOnly // content matches but state DB needs update
)

func (s ChangeStatus) String() string {
	switch s {
	case StatusUnchanged:
		return "unchanged"
	case StatusNew:
		return "new"
	case StatusModified:
		return "modified"
	case StatusConflict:
		return "conflict"
	case StatusStateOnly:
		return "state-only"
	default:
		return "unknown"
	}
}

func NewApplier(db *state.DB, tmplCtx *template.Context, enc *encrypt.AgeEncryptor, dryRun, force bool, verbose int) *Applier {
	return &Applier{
		db:       db,
		tmplCtx:  tmplCtx,
		enc:      enc,
		dryRun:   dryRun,
		force:    force,
		verbose:  verbose,
	}
}

func (a *Applier) Apply(tree *source.Tree) (*ApplyResult, error) {
	result := &ApplyResult{DryRun: a.dryRun}

	for _, dir := range tree.Dirs() {
		if a.dryRun {
			continue
		}
		if err := os.MkdirAll(dir.TargetPath, 0755); err != nil {
			return nil, fmt.Errorf("creating directory %s: %w", dir.TargetPath, err)
		}
	}

	for _, entry := range tree.Files() {
		change, err := computeChange(entry, a.db, &ComputeOpts{TmplCtx: a.tmplCtx, Enc: a.enc})
		if err != nil {
			return nil, fmt.Errorf("checking %s: %w", entry.SourcePath, err)
		}

		switch change.Status {
		case StatusUnchanged:
			result.Skipped++
			continue

		case StatusStateOnly:
			// Content matches target, just need to update state DB
			if !a.dryRun {
				if err := a.recordState(entry, change.NewHash); err != nil {
					return nil, fmt.Errorf("recording state for %s: %w", entry.SourcePath, err)
				}
			}
			result.Skipped++
			continue

		case StatusConflict:
			if !a.force {
				action, err := a.promptConflict(change)
				if err != nil {
					return nil, err
				}
				switch action {
				case "skip":
					result.Skipped++
					continue
				case "abort":
					return nil, fmt.Errorf("aborted by user")
				case "import":
					if a.dryRun {
						fmt.Printf("Would import %s -> %s\n", entry.TargetPath, entry.SourcePath)
					} else {
						if err := a.importFile(entry); err != nil {
							return nil, fmt.Errorf("importing %s: %w", entry.TargetPath, err)
						}
						fmt.Printf("Imported %s -> %s\n", entry.TargetPath, entry.SourcePath)
					}
					result.Imported++
					continue
				}
			}
		}

		if a.dryRun {
			a.printChange(change)
			result.Applied++
			continue
		}

		if err := a.applyFile(entry, change.NewHash); err != nil {
			return nil, fmt.Errorf("applying %s: %w", entry.SourcePath, err)
		}
		result.Applied++
	}

	return result, nil
}


func (a *Applier) applyFile(entry *source.Entry, sourceHash string) error {
	if entry.Attrs.Symlink {
		if err := createSymlink(entry); err != nil {
			return fmt.Errorf("creating symlink: %w", err)
		}
		return a.db.SaveFile(&state.FileEntry{
			SourcePath:  entry.SourcePath,
			TargetPath:  entry.TargetPath,
			SourceHash:  sourceHash,
			AppliedHash: sourceHash,
			Mode:        0777,
		})
	}

	useSudo := needsSudo(entry.TargetPath)

	if useSudo {
		if err := sudoMkdir(filepath.Dir(entry.TargetPath), 0755); err != nil {
			return err
		}
	} else {
		if err := os.MkdirAll(filepath.Dir(entry.TargetPath), 0755); err != nil {
			return err
		}
	}

	mode := entry.Mode.Perm()
	if entry.Attrs.Perm != 0 {
		mode = os.FileMode(entry.Attrs.Perm)
	}

	var content []byte
	var err error

	if entry.Generated {
		content = []byte(entry.GeneratedContent)
	} else {
		content, err = os.ReadFile(entry.SourcePath)
		if err != nil {
			return err
		}

		if entry.Attrs.Encrypted && a.enc != nil {
			content, err = a.enc.Decrypt(content)
			if err != nil {
				return fmt.Errorf("decrypting: %w", err)
			}
		}

		if entry.Attrs.Template && a.tmplCtx != nil {
			content, err = template.Render(content, a.tmplCtx)
			if err != nil {
				return fmt.Errorf("rendering template: %w", err)
			}
		}
	}

	// Remove existing symlink if target should be a regular file
	if info, err := os.Lstat(entry.TargetPath); err == nil {
		if info.Mode()&os.ModeSymlink != 0 && !entry.Attrs.Symlink {
			if useSudo {
				if err := sudoRemove(entry.TargetPath); err != nil {
					return fmt.Errorf("removing symlink: %w", err)
				}
			} else {
				if err := os.Remove(entry.TargetPath); err != nil {
					return fmt.Errorf("removing symlink: %w", err)
				}
			}
		}
	}

	if useSudo {
		if err := sudoWriteFile(entry.TargetPath, content, mode); err != nil {
			return err
		}
	} else {
		if err := os.WriteFile(entry.TargetPath, content, mode); err != nil {
			return err
		}
		if err := os.Chmod(entry.TargetPath, mode); err != nil {
			return err
		}
	}

	if entry.Attrs.Owner != "" || entry.Attrs.Group != "" {
		if useSudo {
			if err := sudoChown(entry.TargetPath, entry.Attrs.Owner, entry.Attrs.Group); err != nil {
				return fmt.Errorf("setting ownership: %w", err)
			}
		} else {
			if err := chownFile(entry.TargetPath, entry.Attrs.Owner, entry.Attrs.Group); err != nil {
				return fmt.Errorf("setting ownership: %w", err)
			}
		}
	}

	targetHash, err := state.HashFile(entry.TargetPath)
	if err != nil {
		return err
	}

	return a.db.SaveFile(&state.FileEntry{
		SourcePath:  entry.SourcePath,
		TargetPath:  entry.TargetPath,
		SourceHash:  sourceHash,
		AppliedHash: targetHash,
		Mode:        mode,
	})
}

func (a *Applier) promptConflict(change *Change) (string, error) {
	fmt.Printf("\nConflict: %s\n", change.Entry.TargetPath)
	fmt.Printf("  Target has been modified since last apply\n")

	canImport := !change.Entry.Attrs.Template
	var prompt string
	if canImport {
		prompt = "  [o]verwrite, [i]mport, [s]kip, [d]iff, [a]bort: "
	} else {
		prompt = "  [o]verwrite, [s]kip, [d]iff, [a]bort: "
	}
	fmt.Print(prompt)

	for {
		char, err := readSingleChar()
		if err != nil {
			return "", err
		}

		input := strings.ToLower(string(char))
		fmt.Println(input)

		switch input {
		case "o":
			return "overwrite", nil
		case "i":
			if canImport {
				return "import", nil
			}
			fmt.Print("\n" + prompt)
		case "s":
			return "skip", nil
		case "a":
			return "abort", nil
		case "d":
			if err := a.showConflictDiff(change.Entry); err != nil {
				fmt.Printf("Error showing diff: %v\n", err)
			}
			fmt.Print(prompt)
		default:
			fmt.Print("\n" + prompt)
		}
	}
}

func readSingleChar() (byte, error) {
	fd := int(os.Stdin.Fd())

	if !term.IsTerminal(fd) {
		b := make([]byte, 1)
		_, err := os.Stdin.Read(b)
		if err != nil {
			return 0, err
		}
		return b[0], nil
	}

	oldState, err := term.MakeRaw(fd)
	if err != nil {
		return 0, err
	}
	defer func() { _ = term.Restore(fd, oldState) }()

	b := make([]byte, 1)
	_, err = os.Stdin.Read(b)
	if err != nil {
		return 0, err
	}

	return b[0], nil
}

func (a *Applier) showConflictDiff(entry *source.Entry) error {
	if entry.Attrs.Encrypted || entry.Attrs.Template {
		content, err := os.ReadFile(entry.SourcePath)
		if err != nil {
			return err
		}

		if entry.Attrs.Encrypted {
			if a.enc == nil || !a.enc.CanDecrypt() {
				return fmt.Errorf("cannot show diff: file is encrypted and no decryption key available")
			}
			content, err = a.enc.Decrypt(content)
			if err != nil {
				return fmt.Errorf("decrypting source: %w", err)
			}
		}

		if entry.Attrs.Template && a.tmplCtx != nil {
			content, err = template.Render(content, a.tmplCtx)
			if err != nil {
				return fmt.Errorf("rendering template: %w", err)
			}
		}

		tmpFile, err := os.CreateTemp("", "mate-diff-*")
		if err != nil {
			return err
		}
		defer func() { _ = os.Remove(tmpFile.Name()) }()

		if _, err := tmpFile.Write(content); err != nil {
			_ = tmpFile.Close()
			return err
		}
		_ = tmpFile.Close()

		return showDiff(tmpFile.Name(), entry.TargetPath)
	}

	return showDiff(entry.SourcePath, entry.TargetPath)
}

func (a *Applier) importFile(entry *source.Entry) error {
	content, err := os.ReadFile(entry.TargetPath)
	if err != nil {
		return fmt.Errorf("reading target: %w", err)
	}

	if entry.Attrs.Encrypted && a.enc != nil {
		content, err = a.enc.Encrypt(content)
		if err != nil {
			return fmt.Errorf("encrypting: %w", err)
		}
	}

	if err := os.WriteFile(entry.SourcePath, content, entry.Mode.Perm()); err != nil {
		return fmt.Errorf("writing source: %w", err)
	}

	sourceHash, err := state.HashFile(entry.SourcePath)
	if err != nil {
		return err
	}

	targetHash, err := state.HashFile(entry.TargetPath)
	if err != nil {
		return err
	}

	return a.db.SaveFile(&state.FileEntry{
		SourcePath:  entry.SourcePath,
		TargetPath:  entry.TargetPath,
		SourceHash:  sourceHash,
		AppliedHash: targetHash,
		Mode:        entry.Mode.Perm(),
	})
}


func (a *Applier) recordState(entry *source.Entry, sourceHash string) error {
	targetHash, err := state.HashFile(entry.TargetPath)
	if err != nil {
		return err
	}

	mode := entry.Mode.Perm()
	if entry.Attrs.Perm != 0 {
		mode = os.FileMode(entry.Attrs.Perm)
	}

	return a.db.SaveFile(&state.FileEntry{
		SourcePath:  entry.SourcePath,
		TargetPath:  entry.TargetPath,
		SourceHash:  sourceHash,
		AppliedHash: targetHash,
		Mode:        mode,
	})
}

func (a *Applier) printChange(change *Change) {
	var prefix string
	switch change.Status {
	case StatusNew:
		prefix = "+"
	case StatusModified:
		prefix = "~"
	case StatusConflict:
		prefix = "!"
	}
	fmt.Printf("%s %s\n", prefix, change.Entry.TargetPath)
}
