// Package fswrite provides crash-atomic file writes that respect symlinks.
// Every production write of a user-visible file goes through Atomic so that a
// crash mid-write can never truncate the file, and a path the user has
// symlinked (for example an AGENTS.md linked into a dotfiles repository) keeps
// its link: the write replaces the symlink's final target, never the link
// itself.
package fswrite

import (
	"errors"
	"fmt"
	"os"
	"path/filepath"
)

// maxSymlinkHops bounds symlink-chain resolution, mirroring the kernel's ELOOP
// guard, so a link cycle fails cleanly instead of looping.
const maxSymlinkHops = 40

// Atomic writes data to path crash-atomically (temp file in the target
// directory + rename). Symlinks are resolved first — including a dangling
// final link, whose target is then created so the link starts working — and
// the rename replaces the resolved target. An existing target keeps its file
// mode; a new file is created with defaultMode.
func Atomic(path string, data []byte, defaultMode os.FileMode) error {
	target, err := resolveSymlinks(path)
	if err != nil {
		return fmt.Errorf("fswrite: resolve %s: %w", path, err)
	}

	mode := defaultMode
	if fi, statErr := os.Stat(target); statErr == nil {
		mode = fi.Mode().Perm()
	}

	dir := filepath.Dir(target)
	if mkErr := os.MkdirAll(dir, 0o755); mkErr != nil { //nolint:gosec // G301: parents of committed repo files, standard permissions by design
		return fmt.Errorf("fswrite: create dir for %s: %w", target, mkErr)
	}
	tmp, err := os.CreateTemp(dir, filepath.Base(target)+".tmp-*")
	if err != nil {
		return fmt.Errorf("fswrite: create temp for %s: %w", target, err)
	}
	tmpName := tmp.Name()
	defer func() { _ = os.Remove(tmpName) }() // no-op after a successful rename

	if chmodErr := tmp.Chmod(mode); chmodErr != nil {
		_ = tmp.Close()
		return fmt.Errorf("fswrite: chmod temp for %s: %w", target, chmodErr)
	}
	if _, writeErr := tmp.Write(data); writeErr != nil {
		_ = tmp.Close()
		return fmt.Errorf("fswrite: write temp for %s: %w", target, writeErr)
	}
	if closeErr := tmp.Close(); closeErr != nil {
		return fmt.Errorf("fswrite: close temp for %s: %w", target, closeErr)
	}
	if renameErr := os.Rename(tmpName, target); renameErr != nil {
		return fmt.Errorf("fswrite: replace %s: %w", target, renameErr)
	}
	return nil
}

// AtomicNew creates path with data crash-atomically, failing with a wrapped
// os.ErrExist if the (symlink-resolved) target already exists — the atomic
// equivalent of an O_EXCL create. The exclusivity comes from os.Link, which
// atomically links the fully-written temp file into place and fails if the
// name appears concurrently; a partial file can never be observed.
func AtomicNew(path string, data []byte, mode os.FileMode) error {
	target, err := resolveSymlinks(path)
	if err != nil {
		return fmt.Errorf("fswrite: resolve %s: %w", path, err)
	}
	if _, statErr := os.Lstat(target); statErr == nil {
		return fmt.Errorf("fswrite: create %s: %w", target, os.ErrExist)
	}

	dir := filepath.Dir(target)
	if mkErr := os.MkdirAll(dir, 0o755); mkErr != nil { //nolint:gosec // G301: parents of committed repo files, standard permissions by design
		return fmt.Errorf("fswrite: create dir for %s: %w", target, mkErr)
	}
	tmp, err := os.CreateTemp(dir, filepath.Base(target)+".tmp-*")
	if err != nil {
		return fmt.Errorf("fswrite: create temp for %s: %w", target, err)
	}
	tmpName := tmp.Name()
	defer func() { _ = os.Remove(tmpName) }() // also cleans up after a failed Link

	if chmodErr := tmp.Chmod(mode); chmodErr != nil {
		_ = tmp.Close()
		return fmt.Errorf("fswrite: chmod temp for %s: %w", target, chmodErr)
	}
	if _, writeErr := tmp.Write(data); writeErr != nil {
		_ = tmp.Close()
		return fmt.Errorf("fswrite: write temp for %s: %w", target, writeErr)
	}
	if closeErr := tmp.Close(); closeErr != nil {
		return fmt.Errorf("fswrite: close temp for %s: %w", target, closeErr)
	}
	if linkErr := os.Link(tmpName, target); linkErr != nil {
		if errors.Is(linkErr, os.ErrExist) {
			return fmt.Errorf("fswrite: create %s: %w", target, os.ErrExist)
		}
		return fmt.Errorf("fswrite: link %s: %w", target, linkErr)
	}
	return nil
}

// resolveSymlinks walks the symlink chain by hand so that, unlike
// filepath.EvalSymlinks, a dangling final link still resolves to the intended
// target location.
func resolveSymlinks(path string) (string, error) {
	resolved := path
	for range maxSymlinkHops {
		fi, err := os.Lstat(resolved)
		if errors.Is(err, os.ErrNotExist) {
			return resolved, nil // does not exist yet; create here
		}
		if err != nil {
			return "", err
		}
		if fi.Mode()&os.ModeSymlink == 0 {
			return resolved, nil
		}
		next, err := os.Readlink(resolved)
		if err != nil {
			return "", err
		}
		if !filepath.IsAbs(next) {
			next = filepath.Join(filepath.Dir(resolved), next)
		}
		resolved = next
	}
	return "", errors.New("too many levels of symbolic links")
}
