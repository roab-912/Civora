package eligibility

import (
	"bufio"
	"bytes"
	"civora/internal/codec"
	"fmt"
	"os"
	"os/exec"
	"testing"
	"time"
)

func crashChanges() []Change { return []Change{{"update", "0", "0"}, {"append", "3", "99"}} }
func TestEligibilityCrashHelper(t *testing.T) {
	if os.Getenv("CIVORA_TREE_CRASH_HELPER") != "1" {
		return
	}
	dir := os.Getenv("CIVORA_TREE_DIRECTORY")
	m, e := BaseMetadata(dir)
	if e != nil {
		t.Fatal(e)
	}
	s, e := OpenStore(dir, m)
	if e != nil {
		t.Fatal(e)
	}
	defer s.Close()
	s.hook = func(point string) {
		if point == os.Getenv("CIVORA_TREE_KILL_POINT") {
			fmt.Fprintln(os.Stdout, "READY")
			var b [1]byte
			_, _ = os.Stdin.Read(b[:])
			os.Exit(91)
		}
	}
	if _, e = s.Commit(2, crashChanges()); e != nil {
		t.Fatal(e)
	}
	t.Fatal("hook not reached")
}
func TestVersionedTreeProcessDeath(t *testing.T) {
	for _, point := range []string{"after_nodes", "before_cursor", "after_commit"} {
		t.Run(point, func(t *testing.T) {
			v := loadVector(t)
			tree, _ := New(16)
			initial := tree.Metadata(v.NetworkID, "1", "crash-test", "semaphore-v4.14.5-depth-16", "0")
			dir := t.TempDir()
			s, e := OpenStore(dir, initial)
			if e != nil {
				t.Fatal(e)
			}
			first := []Change{{"append", "0", "1"}, {"append", "1", "2"}, {"append", "2", "3"}}
			if _, e = s.Commit(1, first); e != nil {
				t.Fatal(e)
			}
			if e = s.Close(); e != nil {
				t.Fatal(e)
			}
			cmd := exec.Command(os.Args[0], "-test.run=^TestEligibilityCrashHelper$")
			cmd.Env = append(os.Environ(), "CIVORA_TREE_CRASH_HELPER=1", "CIVORA_TREE_DIRECTORY="+dir, "CIVORA_TREE_KILL_POINT="+point)
			out, e := cmd.StdoutPipe()
			if e != nil {
				t.Fatal(e)
			}
			input, e := cmd.StdinPipe()
			if e != nil {
				t.Fatal(e)
			}
			defer input.Close()
			var stderr bytes.Buffer
			cmd.Stderr = &stderr
			if e = cmd.Start(); e != nil {
				t.Fatal(e)
			}
			defer cmd.Process.Kill()
			ready := make(chan bool, 1)
			go func() {
				scanner := bufio.NewScanner(out)
				for scanner.Scan() {
					if scanner.Text() == "READY" {
						ready <- true
						return
					}
				}
				ready <- false
			}()
			select {
			case ok := <-ready:
				if !ok {
					_ = cmd.Wait()
					t.Fatal(stderr.String())
				}
			case <-time.After(20 * time.Second):
				_ = cmd.Process.Kill()
				_ = cmd.Wait()
				t.Fatal("helper timeout")
			}
			if e = cmd.Process.Kill(); e != nil {
				t.Fatal(e)
			}
			if e = cmd.Wait(); e == nil {
				t.Fatal("helper not killed")
			}
			s, e = OpenStore(dir, initial)
			if e != nil {
				t.Fatal(e)
			}
			defer s.Close()
			m, e := s.Latest()
			if e != nil {
				t.Fatal(e)
			}
			expected := "1"
			if point == "after_commit" {
				expected = "2"
			}
			if m.Height != expected {
				t.Fatalf("partial transaction: got %s want %s", m.Height, expected)
			}
			if _, e = s.Path(1, 0); e != nil {
				t.Fatal("historical path lost", e)
			}
			if expected == "1" {
				if _, e = s.Commit(2, crashChanges()); e != nil {
					t.Fatal(e)
				}
			}
			applyChanges(t, tree, first)
			applyChanges(t, tree, crashChanges())
			want := tree.Metadata(v.NetworkID, "1", "crash-test", "semaphore-v4.14.5-depth-16", "2")
			m, e = s.Latest()
			if e != nil || !bytes.Equal(codec.Must(m), codec.Must(want)) {
				t.Fatal("recovery differs", e)
			}
			if _, e = s.Path(2, 0); e == nil {
				t.Fatal("retired leaf survived")
			}
			if _, e = s.Path(2, 3); e != nil {
				t.Fatal(e)
			}
		})
	}
}
