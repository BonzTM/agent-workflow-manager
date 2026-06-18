package main

import "testing"

func TestStaticFS_ReturnsFileSystem(t *testing.T) {
	fs, err := staticFS()
	if err != nil {
		t.Fatalf("staticFS() failed: %v", err)
	}
	if fs == nil {
		t.Fatal("staticFS() returned nil")
	}
}
