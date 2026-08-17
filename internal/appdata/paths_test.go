package appdata

import (
	"os"
	"path/filepath"
	"testing"
)

func TestResolveAndAtomicWriteProtectsAppData(t *testing.T) {
	root := filepath.Join(t.TempDir(), "cvpp")
	paths, err := Resolve(root)
	if err != nil {
		t.Fatal(err)
	}
	if err := paths.Ensure(); err != nil {
		t.Fatal(err)
	}
	if err := AtomicWrite(paths.Credentials, []byte("secret\n"), 0o600); err != nil {
		t.Fatal(err)
	}
	info, err := os.Stat(paths.Credentials)
	if err != nil {
		t.Fatal(err)
	}
	if info.Mode().Perm() != 0o600 {
		t.Fatalf("mode = %o, want 600", info.Mode().Perm())
	}
}
