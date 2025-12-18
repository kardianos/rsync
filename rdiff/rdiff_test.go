package main

import (
	"bytes"
	"os"
	"path/filepath"
	"testing"

	"github.com/kardianos/rsync/proto"
)

func TestRdiffRoundTrip(t *testing.T) {
	dir := t.TempDir()

	basisPath := filepath.Join(dir, "basis")
	newPath := filepath.Join(dir, "new")
	sigPath := filepath.Join(dir, "signature")
	deltaPath := filepath.Join(dir, "delta")
	patchedPath := filepath.Join(dir, "patched")

	basisContent := []byte("The quick brown fox jumps over the lazy dog.")
	newContent := []byte("The quick brown fox jumps over the lazy dog. Extra content.")

	if err := os.WriteFile(basisPath, basisContent, 0644); err != nil {
		t.Fatalf("failed to write basis file: %v", err)
	}
	if err := os.WriteFile(newPath, newContent, 0644); err != nil {
		t.Fatalf("failed to write new file: %v", err)
	}

	// 1. Create Signature
	if err := signature(basisPath, sigPath, 1); err != nil {
		t.Fatalf("signature failed: %v", err)
	}
	// Verify signature created
	if _, err := os.Stat(sigPath); err != nil {
		t.Errorf("signature file not created: %v", err)
	}

	// 2. Create Delta
	// Testing with checkFile=true and CompNone first
	if err := delta(sigPath, newPath, deltaPath, true, proto.CompNone); err != nil {
		t.Fatalf("delta failed: %v", err)
	}
	// Verify delta created
	if _, err := os.Stat(deltaPath); err != nil {
		t.Errorf("delta file not created: %v", err)
	}

	// 3. Patch
	if err := patch(basisPath, deltaPath, patchedPath, true); err != nil {
		t.Fatalf("patch failed: %v", err)
	}
	// Verify patched file created
	if _, err := os.Stat(patchedPath); err != nil {
		t.Errorf("patched file not created: %v", err)
	}

	// 4. Compare
	patchedContent, err := os.ReadFile(patchedPath)
	if err != nil {
		t.Fatalf("failed to read patched file: %v", err)
	}
	if !bytes.Equal(patchedContent, newContent) {
		t.Errorf("patched content mismatch.\nGot: %q\nWant: %q", patchedContent, newContent)
	}

	// 5. Test valid command (test verb)
	if err := test(basisPath, basisPath); err != nil {
		t.Errorf("test/compare failed on identical files: %v", err)
	}
}

func TestRdiffCompression(t *testing.T) {
	dir := t.TempDir()

	basisPath := filepath.Join(dir, "basis")
	newPath := filepath.Join(dir, "new")
	sigPath := filepath.Join(dir, "signature")
	deltaPath := filepath.Join(dir, "delta")
	patchedPath := filepath.Join(dir, "patched")

	// Make content larger to ensure multiple blocks and compression kickoff.
	basisContent := bytes.Repeat([]byte("The quick brown fox jumps over the lazy dog.\n"), 100)
	newContent := append(basisContent, []byte("Extra content at the end.\n")...)

	if err := os.WriteFile(basisPath, basisContent, 0644); err != nil {
		t.Fatalf("failed to write basis file: %v", err)
	}
	if err := os.WriteFile(newPath, newContent, 0644); err != nil {
		t.Fatalf("failed to write new file: %v", err)
	}

	if err := signature(basisPath, sigPath, 1); err != nil {
		t.Fatalf("signature failed: %v", err)
	}

	// Use CompGZip
	if err := delta(sigPath, newPath, deltaPath, true, proto.CompGZip); err != nil {
		t.Fatalf("delta failed: %v", err)
	}

	if err := patch(basisPath, deltaPath, patchedPath, true); err != nil {
		t.Fatalf("patch failed: %v", err)
	}

	patchedContent, err := os.ReadFile(patchedPath)
	if err != nil {
		t.Fatalf("failed to read patched file: %v", err)
	}
	if !bytes.Equal(patchedContent, newContent) {
		t.Errorf("patched content mismatch with compression.\nGot length %d, Want length %d", len(patchedContent), len(newContent))
	}
}

func TestRdiffPatchNoCheck(t *testing.T) {
	dir := t.TempDir()

	basisPath := filepath.Join(dir, "basis")
	newPath := filepath.Join(dir, "new")
	sigPath := filepath.Join(dir, "signature")
	deltaPath := filepath.Join(dir, "delta")
	patchedPath := filepath.Join(dir, "patched")

	basisContent := []byte("Initial data.")
	newContent := []byte("Initial data. Modified.")

	if err := os.WriteFile(basisPath, basisContent, 0644); err != nil {
		t.Fatalf("failed to write basis file: %v", err)
	}
	if err := os.WriteFile(newPath, newContent, 0644); err != nil {
		t.Fatalf("failed to write new file: %v", err)
	}

	if err := signature(basisPath, sigPath, 1); err != nil {
		t.Fatalf("signature failed: %v", err)
	}

	if err := delta(sigPath, newPath, deltaPath, false, proto.CompNone); err != nil {
		t.Fatalf("delta failed: %v", err)
	}

	if err := patch(basisPath, deltaPath, patchedPath, false); err != nil {
		t.Fatalf("patch failed: %v", err)
	}

	patchedContent, err := os.ReadFile(patchedPath)
	if err != nil {
		t.Fatalf("failed to read patched file: %v", err)
	}
	if !bytes.Equal(patchedContent, newContent) {
		t.Errorf("patched content mismatch (no checksum).\nGot: %q\nWant: %q", patchedContent, newContent)
	}
}

func TestCompare(t *testing.T) {
	dir := t.TempDir()
	f1 := filepath.Join(dir, "f1")
	f2 := filepath.Join(dir, "f2")

	content := []byte("some content")
	os.WriteFile(f1, content, 0644)
	os.WriteFile(f2, content, 0644)

	if err := test(f1, f2); err != nil {
		t.Errorf("expected files to be same: %v", err)
	}

	os.WriteFile(f2, []byte("some content diff"), 0644)
	if err := test(f1, f2); err == nil {
		t.Error("expected error for different size files")
	}
	os.WriteFile(f2, []byte("some contenT"), 0644)
	if err := test(f1, f2); err == nil {
		t.Error("expected error for different content files")
	}
}
