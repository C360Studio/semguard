package main

import (
	"io"
	"os"
	"path/filepath"
	"testing"
)

// spec: openspec/specs/corpus/spec.md#Requirement: Frozen case file pinned by hash
func TestRunExitCodes(t *testing.T) {
	empty := t.TempDir()
	corrupt := t.TempDir()
	if err := os.WriteFile(filepath.Join(corrupt, "corpus.lock.json"), []byte(`{"version":1,"files":[{"path":"cases.json","size":1,"sha256":"00"}]}`), 0o600); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(corrupt, "cases.json"), []byte("x"), 0o600); err != nil {
		t.Fatal(err)
	}
	nodrafts := t.TempDir()
	if err := os.WriteFile(filepath.Join(nodrafts, "tasks.json"), []byte(`{"version":1,"tasks":[]}`), 0o600); err != nil {
		t.Fatal(err)
	}
	tests := []struct {
		name string
		args []string
		want int
	}{
		{"no args", nil, 2},
		{"unknown command", []string{"frob"}, 2},
		{"bad flag", []string{"validate", "-nope"}, 2},
		{"validate missing lock is IO", []string{"validate", "-dir", empty}, 2},
		{"validate lock mismatch is validation", []string{"validate", "-dir", corrupt}, 1},
		{"build with missing drafts is validation", []string{"build", "-dir", nodrafts}, 1},
		{"build missing tasks is IO", []string{"build", "-dir", empty}, 2},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			if got := run(tt.args, io.Discard, io.Discard); got != tt.want {
				t.Fatalf("exit %d want %d", got, tt.want)
			}
		})
	}
}
