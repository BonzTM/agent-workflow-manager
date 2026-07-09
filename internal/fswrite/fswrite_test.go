package fswrite

import (
	"errors"
	"os"
	"path/filepath"
	"testing"
)

func TestAtomicWritesPlainFile(t *testing.T) {
	t.Parallel()
	path := filepath.Join(t.TempDir(), "out.txt")
	if err := Atomic(path, []byte("hello"), 0o644); err != nil {
		t.Fatalf("Atomic: %v", err)
	}
	data, err := os.ReadFile(path)
	if err != nil {
		t.Fatal(err)
	}
	if string(data) != "hello" {
		t.Fatalf("content = %q", data)
	}
	fi, err := os.Stat(path)
	if err != nil {
		t.Fatal(err)
	}
	if fi.Mode().Perm() != 0o644 {
		t.Fatalf("new file mode = %v, want 0644", fi.Mode().Perm())
	}
}

func TestAtomicWritesThroughSymlink(t *testing.T) {
	t.Parallel()
	dir := t.TempDir()
	target := filepath.Join(dir, "real", "file.md")
	if err := os.MkdirAll(filepath.Dir(target), 0o750); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(target, []byte("original"), 0o600); err != nil {
		t.Fatal(err)
	}
	link := filepath.Join(dir, "link.md")
	if err := os.Symlink(target, link); err != nil {
		t.Fatal(err)
	}

	if err := Atomic(link, []byte("updated"), 0o644); err != nil {
		t.Fatalf("Atomic: %v", err)
	}

	fi, err := os.Lstat(link)
	if err != nil {
		t.Fatal(err)
	}
	if fi.Mode()&os.ModeSymlink == 0 {
		t.Fatal("symlink was orphaned into a regular file")
	}
	data, err := os.ReadFile(target)
	if err != nil {
		t.Fatal(err)
	}
	if string(data) != "updated" {
		t.Fatalf("target content = %q, want updated", data)
	}
	tfi, err := os.Stat(target)
	if err != nil {
		t.Fatal(err)
	}
	if tfi.Mode().Perm() != 0o600 {
		t.Fatalf("existing target mode = %v, want 0600 preserved over the 0644 default", tfi.Mode().Perm())
	}
}

func TestAtomicCreatesDanglingSymlinkTarget(t *testing.T) {
	t.Parallel()
	dir := t.TempDir()
	target := filepath.Join(dir, "dotfiles", "file.md")
	link := filepath.Join(dir, "link.md")
	if err := os.Symlink(target, link); err != nil {
		t.Fatal(err)
	}

	if err := Atomic(link, []byte("created"), 0o644); err != nil {
		t.Fatalf("Atomic: %v", err)
	}
	fi, err := os.Lstat(link)
	if err != nil {
		t.Fatal(err)
	}
	if fi.Mode()&os.ModeSymlink == 0 {
		t.Fatal("dangling symlink was replaced instead of populated")
	}
	data, err := os.ReadFile(target)
	if err != nil {
		t.Fatal(err)
	}
	if string(data) != "created" {
		t.Fatalf("target content = %q, want created", data)
	}
}

func TestAtomicRejectsSymlinkLoops(t *testing.T) {
	t.Parallel()
	dir := t.TempDir()
	a := filepath.Join(dir, "a")
	b := filepath.Join(dir, "b")
	if err := os.Symlink(a, b); err != nil {
		t.Fatal(err)
	}
	if err := os.Symlink(b, a); err != nil {
		t.Fatal(err)
	}
	if err := Atomic(a, []byte("x"), 0o644); err == nil {
		t.Fatal("expected an error for a symlink loop")
	}
}

func TestAtomicNewCreatesAndRefusesExisting(t *testing.T) {
	t.Parallel()
	path := filepath.Join(t.TempDir(), "new.txt")
	if err := AtomicNew(path, []byte("first"), 0o644); err != nil {
		t.Fatalf("AtomicNew: %v", err)
	}
	data, err := os.ReadFile(path)
	if err != nil || string(data) != "first" {
		t.Fatalf("content = %q, err %v", data, err)
	}
	err = AtomicNew(path, []byte("second"), 0o644)
	if !errors.Is(err, os.ErrExist) {
		t.Fatalf("err = %v, want errors.Is os.ErrExist", err)
	}
	data, err = os.ReadFile(path)
	if err != nil || string(data) != "first" {
		t.Fatalf("existing content was overwritten: %q, %v", data, err)
	}
}

func TestAtomicNewThroughDanglingSymlink(t *testing.T) {
	t.Parallel()
	dir := t.TempDir()
	target := filepath.Join(dir, "dotfiles", "new.md")
	link := filepath.Join(dir, "link.md")
	if err := os.Symlink(target, link); err != nil {
		t.Fatal(err)
	}
	if err := AtomicNew(link, []byte("via link"), 0o644); err != nil {
		t.Fatalf("AtomicNew: %v", err)
	}
	fi, err := os.Lstat(link)
	if err != nil || fi.Mode()&os.ModeSymlink == 0 {
		t.Fatalf("link state: %v %v", fi, err)
	}
	data, err := os.ReadFile(target)
	if err != nil || string(data) != "via link" {
		t.Fatalf("target = %q, err %v", data, err)
	}
}
