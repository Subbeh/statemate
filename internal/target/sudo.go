package target

import (
	"fmt"
	"os"
	"os/exec"
	"os/user"
	"path/filepath"
	"strconv"
	"syscall"
)

func needsSudo(path string) bool {
	dir := path
	for dir != "/" {
		// Use Lstat to not follow symlinks - we care about the path itself
		info, err := os.Lstat(dir)
		if err == nil {
			// If it's a symlink, check the parent directory
			if info.Mode()&os.ModeSymlink != 0 {
				dir = filepath.Dir(dir)
				continue
			}
			return !isWritableByCurrentUser(info)
		}
		dir = filepath.Dir(dir)
		if dir == "" {
			dir = "/"
		}
	}
	return true
}

func isWritableByCurrentUser(info os.FileInfo) bool {
	mode := info.Mode().Perm()

	// Check world-writable
	if mode&0002 != 0 {
		return true
	}

	// Get current user
	currentUser, err := user.Current()
	if err != nil {
		return false
	}

	// Check if we're root
	if currentUser.Uid == "0" {
		return true
	}

	// Get file's uid/gid (Unix-specific)
	stat, ok := info.Sys().(*syscall.Stat_t)
	if !ok {
		return false
	}

	// Check owner
	if fmt.Sprintf("%d", stat.Uid) == currentUser.Uid {
		return mode&0200 != 0
	}

	// Check group
	if fmt.Sprintf("%d", stat.Gid) == currentUser.Gid {
		return mode&0020 != 0
	}

	// Check supplementary groups
	gids, err := currentUser.GroupIds()
	if err == nil {
		fileGid := fmt.Sprintf("%d", stat.Gid)
		for _, gid := range gids {
			if gid == fileGid {
				return mode&0020 != 0
			}
		}
	}

	return false
}

func chownFile(path, owner, group string) error {
	uid := -1
	gid := -1

	if owner != "" {
		u, err := user.Lookup(owner)
		if err != nil {
			return fmt.Errorf("looking up user %q: %w", owner, err)
		}
		uid, _ = strconv.Atoi(u.Uid)
	}

	if group != "" {
		g, err := user.LookupGroup(group)
		if err != nil {
			return fmt.Errorf("looking up group %q: %w", group, err)
		}
		gid, _ = strconv.Atoi(g.Gid)
	}

	if uid == -1 && gid == -1 {
		return nil
	}

	return os.Chown(path, uid, gid)
}

func sudoChown(path, owner, group string) error {
	args := []string{"chown"}
	if group != "" {
		args = append(args, owner+":"+group)
	} else {
		args = append(args, owner)
	}
	args = append(args, path)

	cmd := exec.Command("sudo", args...)
	cmd.Stdin = os.Stdin
	cmd.Stdout = os.Stdout
	cmd.Stderr = os.Stderr
	return cmd.Run()
}

func sudoWriteFile(path string, content []byte, mode os.FileMode) error {
	tmpFile, err := os.CreateTemp("", "statemate-*")
	if err != nil {
		return err
	}
	tmpPath := tmpFile.Name()
	defer os.Remove(tmpPath)

	if _, err := tmpFile.Write(content); err != nil {
		tmpFile.Close()
		return err
	}
	tmpFile.Close()

	cmd := exec.Command("sudo", "cp", tmpPath, path)
	cmd.Stdin = os.Stdin
	cmd.Stdout = os.Stdout
	cmd.Stderr = os.Stderr
	if err := cmd.Run(); err != nil {
		return err
	}

	cmd = exec.Command("sudo", "chmod", fmt.Sprintf("%o", mode), path)
	cmd.Stdin = os.Stdin
	cmd.Stdout = os.Stdout
	cmd.Stderr = os.Stderr
	return cmd.Run()
}

func sudoMkdir(path string, mode os.FileMode) error {
	cmd := exec.Command("sudo", "mkdir", "-p", path)
	cmd.Stdin = os.Stdin
	cmd.Stdout = os.Stdout
	cmd.Stderr = os.Stderr
	if err := cmd.Run(); err != nil {
		return err
	}

	cmd = exec.Command("sudo", "chmod", fmt.Sprintf("%o", mode), path)
	cmd.Stdin = os.Stdin
	cmd.Stdout = os.Stdout
	cmd.Stderr = os.Stderr
	return cmd.Run()
}

func sudoRemove(path string) error {
	cmd := exec.Command("sudo", "rm", path)
	cmd.Stdin = os.Stdin
	cmd.Stdout = os.Stdout
	cmd.Stderr = os.Stderr
	return cmd.Run()
}
