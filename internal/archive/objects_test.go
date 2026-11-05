package archive

import (
	"os"
	"os/exec"
	"path/filepath"
	"testing"
)

func TestAbruptExitBeforeAndAfterDurablePublication(t *testing.T) {
	if dir := os.Getenv("CIVORA_ARCHIVE_CRASH_DIR"); dir != "" {
		s := Store{Directory: dir, Hook: func(stage string) {
			if stage == os.Getenv("CIVORA_ARCHIVE_CRASH_STAGE") {
				os.Exit(73)
			}
		}}
		_, _ = s.Put([]byte("public encrypted development object"))
		os.Exit(74)
	}
	for _, stage := range []string{"partial_write", "after_sync", "after_rename"} {
		t.Run(stage, func(t *testing.T) {
			dir := t.TempDir()
			cmd := exec.Command(os.Args[0], "-test.run=^TestAbruptExitBeforeAndAfterDurablePublication$")
			cmd.Env = append(os.Environ(), "CIVORA_ARCHIVE_CRASH_DIR="+dir, "CIVORA_ARCHIVE_CRASH_STAGE="+stage)
			if e := cmd.Run(); e == nil {
				t.Fatal("child did not die")
			}
			s := Store{Directory: dir}
			data := []byte("public encrypted development object")
			path, _ := s.Path(Digest(data))
			if stage != "after_rename" {
				if _, e := os.Stat(path); !os.IsNotExist(e) {
					t.Fatal("partial object published", e)
				}
			}
			actual, e := s.Put(data)
			if e != nil {
				t.Fatal(e)
			}
			if _, e = s.Get(actual); e != nil {
				t.Fatal(e)
			}
			if e = os.WriteFile(path, []byte("corrupted"), 0600); e != nil {
				t.Fatal(e)
			}
			if _, e = s.Get(actual); e == nil {
				t.Fatal("corruption accepted")
			}
			if _, e = s.Put(data); e == nil {
				t.Fatal("corruption silently overwritten")
			}
			if _, e = s.Path(filepath.Join("..", "escape")); e == nil {
				t.Fatal("path traversal")
			}
		})
	}
}
