package config

import (
	"os"
	"path/filepath"
	"testing"
)

func TestReadPEMFile_OK(t *testing.T) {
	t.Parallel()

	dir := t.TempDir()
	p := filepath.Join(dir, "key.pem")
	if err := os.WriteFile(p, []byte("-----BEGIN PUBLIC KEY-----\nabc\n-----END PUBLIC KEY-----\n"), 0o600); err != nil {
		t.Fatalf("write temp file: %v", err)
	}

	pemText, err := readPEMFile(p)
	if err != nil {
		t.Fatalf("readPEMFile: %v", err)
	}
	if pemText == "" {
		t.Fatalf("expected non-empty pemText")
	}
}

func TestReadPEMFile_Missing(t *testing.T) {
	t.Parallel()

	_, err := readPEMFile(filepath.Join(t.TempDir(), "missing.pem"))
	if err == nil {
		t.Fatalf("expected error")
	}
}

func TestReadPEMFile_Empty(t *testing.T) {
	t.Parallel()

	dir := t.TempDir()
	p := filepath.Join(dir, "empty.pem")
	if err := os.WriteFile(p, []byte("\n\n"), 0o600); err != nil {
		t.Fatalf("write temp file: %v", err)
	}

	_, err := readPEMFile(p)
	if err == nil {
		t.Fatalf("expected error")
	}
}

/*





























}	}		t.Fatalf("expected error")	if err == nil {	_, err := readPEMFile(p)	}		t.Fatalf("write temp file: %v", err)	if err := os.WriteFile(p, []byte("\n\n"), 0o600); err != nil {	p := filepath.Join(dir, "empty.pem")	dir := t.TempDir()	t.Parallel()func TestReadPEMFile_Empty(t *testing.T) {}	}		t.Fatalf("expected error")	if err == nil {	_, err := readPEMFile(filepath.Join(t.TempDir(), "missing.pem"))	t.Parallel()func TestReadPEMFile_Missing(t *testing.T) {}	}		t.Fatalf("expected non-empty pemText")	if pemText == "" {	}		t.Fatalf("readPEMFile: %v", err)
*/
