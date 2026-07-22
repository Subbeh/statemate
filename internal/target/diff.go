package target

import (
	"bytes"
	"fmt"
	"os"
	"os/exec"
	"strings"

	"github.com/fatih/color"
	"github.com/subbeh/statemate/internal/encrypt"
	"github.com/subbeh/statemate/internal/source"
	"github.com/subbeh/statemate/internal/state"
	"github.com/subbeh/statemate/internal/template"
)

type Change struct {
	Entry   *source.Entry
	Status  ChangeStatus
	OldHash string
	NewHash string
}

func desiredMode(entry *source.Entry) os.FileMode {
	if entry.Attrs.Perm != 0 {
		return os.FileMode(entry.Attrs.Perm)
	}
	return entry.Mode.Perm()
}

func permMismatch(entry *source.Entry, info os.FileInfo) bool {
	if entry.Attrs.Perm == 0 {
		return false
	}
	return info.Mode().Perm() != desiredMode(entry)
}

type ComputeOpts struct {
	TmplCtx *template.Context
	Enc     *encrypt.AgeEncryptor
}

func ComputeChanges(tree *source.Tree, db *state.DB, opts ...ComputeOpts) ([]*Change, error) {
	var o ComputeOpts
	if len(opts) > 0 {
		o = opts[0]
	}

	var changes []*Change

	for _, entry := range tree.Files() {
		change, err := computeChange(entry, db, &o)
		if err != nil {
			return nil, err
		}
		if change.Status != StatusUnchanged && change.Status != StatusStateOnly {
			changes = append(changes, change)
		}
	}

	return changes, nil
}

func computeChange(entry *source.Entry, db *state.DB, opts *ComputeOpts) (*Change, error) {
	change := &Change{Entry: entry}

	var sourceHash string
	var err error
	if entry.Generated {
		sourceHash = state.HashBytes([]byte(entry.GeneratedContent))
	} else {
		sourceHash, err = state.HashFile(entry.SourcePath)
		if err != nil {
			return nil, err
		}
	}
	change.NewHash = sourceHash

	renderedHash := sourceHash
	if !entry.Generated && (entry.Attrs.Encrypted || entry.Attrs.Template) {
		if h, err := getRenderedHash(entry, opts); err == nil {
			renderedHash = h
		}
	}

	existing, err := db.GetFile(entry.TargetPath)
	if err != nil {
		return nil, err
	}

	if existing == nil {
		info, err := os.Lstat(entry.TargetPath)
		if os.IsNotExist(err) {
			change.Status = StatusNew
		} else if err != nil {
			return nil, err
		} else {
			// If target is a symlink but source is not a symlink type, treat as conflict
			targetIsSymlink := info.Mode()&os.ModeSymlink != 0
			if targetIsSymlink && !entry.Attrs.Symlink {
				change.Status = StatusConflict
				return change, nil
			}

			targetHash, err := state.HashFile(entry.TargetPath)
			if err != nil {
				return nil, err
			}
			if targetHash != renderedHash {
				change.Status = StatusConflict
				change.OldHash = targetHash
			} else if permMismatch(entry, info) {
				change.Status = StatusModified
			} else {
				change.Status = StatusStateOnly
			}
		}
		return change, nil
	}

	change.OldHash = existing.AppliedHash

	info, err := os.Lstat(entry.TargetPath)
	targetExists := err == nil
	if err != nil && !os.IsNotExist(err) {
		return nil, err
	}

	if !targetExists {
		change.Status = StatusModified
		return change, nil
	}

	// If target is a symlink but source is not a symlink type, treat as modified
	targetIsSymlink := info.Mode()&os.ModeSymlink != 0
	if targetIsSymlink && !entry.Attrs.Symlink {
		change.Status = StatusModified
		return change, nil
	}

	targetHash, err := state.HashFile(entry.TargetPath)
	if err != nil {
		return nil, err
	}

	if existing.SourceHash == sourceHash {
		if targetHash == existing.AppliedHash {
			if renderedHash != targetHash {
				change.Status = StatusModified
			} else if permMismatch(entry, info) {
				change.Status = StatusModified
			} else {
				change.Status = StatusUnchanged
			}
		} else {
			change.Status = StatusConflict
		}
		return change, nil
	}

	if targetHash != existing.AppliedHash {
		change.Status = StatusConflict
	} else {
		change.Status = StatusModified
	}

	return change, nil
}

func showDiff(sourcePath, targetPath string) error {
	return ShowDiffWithTool(sourcePath, targetPath, "")
}

func ShowDiffWithTool(sourcePath, targetPath, diffTool string) error {
	if diffTool != "" {
		cmd := exec.Command(diffTool, targetPath, sourcePath)
		cmd.Stdout = os.Stdout
		cmd.Stderr = os.Stderr
		cmd.Stdin = os.Stdin
		_ = cmd.Run()
		return nil
	}

	cmd := exec.Command("diff", "-u", targetPath, sourcePath)
	var out bytes.Buffer
	cmd.Stdout = &out
	cmd.Stderr = &out

	_ = cmd.Run()

	diff := out.String()
	if diff == "" {
		fmt.Println("  (no differences)")
	} else {
		fmt.Println(ColorizeDiff(diff))
	}
	return nil
}

func ColorizeDiff(diff string) string {
	if diff == "" {
		return ""
	}

	red := color.New(color.FgRed).SprintFunc()
	green := color.New(color.FgGreen).SprintFunc()
	cyan := color.New(color.FgCyan).SprintFunc()

	var result strings.Builder
	for _, line := range strings.Split(diff, "\n") {
		switch {
		case strings.HasPrefix(line, "+++") || strings.HasPrefix(line, "---"):
			result.WriteString(cyan(line))
		case strings.HasPrefix(line, "@@"):
			result.WriteString(cyan(line))
		case strings.HasPrefix(line, "+"):
			result.WriteString(green(line))
		case strings.HasPrefix(line, "-"):
			result.WriteString(red(line))
		default:
			result.WriteString(line)
		}
		result.WriteString("\n")
	}

	return strings.TrimSuffix(result.String(), "\n")
}

func GenerateDiff(sourcePath, targetPath string) (string, error) {
	return GenerateDiffWithTool(sourcePath, targetPath, "")
}

func GenerateDiffWithTool(sourcePath, targetPath, diffTool string) (string, error) {
	if _, err := os.Stat(targetPath); os.IsNotExist(err) {
		content, err := os.ReadFile(sourcePath)
		if err != nil {
			return "", err
		}
		return fmt.Sprintf("+++ %s (new file)\n%s", targetPath, content), nil
	}

	tool := "diff"
	args := []string{"-u", targetPath, sourcePath}
	if diffTool != "" {
		tool = diffTool
		args = []string{targetPath, sourcePath}
	}

	cmd := exec.Command(tool, args...)
	var out bytes.Buffer
	cmd.Stdout = &out
	cmd.Stderr = &out

	_ = cmd.Run()

	return out.String(), nil
}

func IsBinaryFile(path string) bool {
	f, err := os.Open(path)
	if err != nil {
		return false
	}
	defer func() { _ = f.Close() }()

	buf := make([]byte, 512)
	n, err := f.Read(buf)
	if err != nil {
		return false
	}

	for i := 0; i < n; i++ {
		if buf[i] == 0 {
			return true
		}
	}
	return false
}

func getRenderedHash(entry *source.Entry, opts *ComputeOpts) (string, error) {
	content, err := os.ReadFile(entry.SourcePath)
	if err != nil {
		return "", err
	}

	if entry.Attrs.Encrypted && opts.Enc != nil {
		content, err = opts.Enc.Decrypt(content)
		if err != nil {
			return "", err
		}
	}

	if entry.Attrs.Template && opts.TmplCtx != nil {
		content, err = template.Render(content, opts.TmplCtx)
		if err != nil {
			return "", err
		}
	}

	return state.HashBytes(content), nil
}
