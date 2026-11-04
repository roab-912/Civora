package store

import (
	"bufio"
	"bytes"
	"civora/internal/codec"
	"civora/internal/core"
	"civora/internal/testutil"
	"encoding/json"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"strconv"
	"strings"
	"testing"
	"time"
)

func TestCrashHelper(t *testing.T) {
	if os.Getenv("CIVORA_TEST_CRASH_HELPER") != "1" {
		return
	}
	f := testutil.Load(t, os.Getenv("CIVORA_TEST_FIXTURE"))
	s, e := Open(os.Getenv("CIVORA_TEST_STORE"), codec.Must(f.Genesis), 2)
	if e != nil {
		t.Fatal(e)
	}
	index, e := strconv.Atoi(os.Getenv("CIVORA_TEST_BLOCK_INDEX"))
	if e != nil {
		t.Fatal(e)
	}
	s.hook = func(point string) {
		if point == os.Getenv("CIVORA_TEST_KILL_POINT") {
			fmt.Fprintln(os.Stdout, "READY")
			var b [1]byte
			_, _ = os.Stdin.Read(b[:])
			os.Exit(91)
		}
	}
	if e = s.Commit(codec.Must(f.Blocks[index])); e != nil {
		t.Fatal(e)
	}
	t.Fatal("crash hook not reached")
}

func TestAbruptProcessDeathAndReplay(t *testing.T) {
	for _, fixture := range []string{"accounts", "activation"} {
		for _, point := range []string{"partial_write", "after_write", "after_sync", "after_rename", "after_directory_sync"} {
			t.Run(fixture+"/"+point, func(t *testing.T) {
				f := testutil.Load(t, fixture)
				dir := t.TempDir()
				s, e := Open(dir, codec.Must(f.Genesis), 2)
				if e != nil {
					t.Fatal(e)
				}
				// Stop during rotation, or during the first block of a new epoch.
				index := 1
				if fixture == "activation" {
					index = 2
				}
				for _, b := range f.Blocks[:index] {
					if e = s.Commit(codec.Must(b)); e != nil {
						t.Fatal(e)
					}
				}
				cmd := exec.Command(os.Args[0], "-test.run=^TestCrashHelper$")
				cmd.Env = append(os.Environ(), "CIVORA_TEST_CRASH_HELPER=1", "CIVORA_TEST_FIXTURE="+fixture, "CIVORA_TEST_STORE="+dir, "CIVORA_TEST_BLOCK_INDEX="+strconv.Itoa(index), "CIVORA_TEST_KILL_POINT="+point)
				out, e := cmd.StdoutPipe()
				if e != nil {
					t.Fatal(e)
				}
				in, e := cmd.StdinPipe()
				if e != nil {
					t.Fatal(e)
				}
				defer in.Close()
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
						t.Fatalf("helper exited: %s", stderr.String())
					}
				case <-time.After(20 * time.Second):
					_ = cmd.Process.Kill()
					_ = cmd.Wait()
					t.Fatal("crash helper timeout")
				}
				if e = cmd.Process.Kill(); e != nil {
					t.Fatal(e)
				}
				if e = cmd.Wait(); e == nil {
					t.Fatal("helper was not killed")
				}
				s, e = Open(dir, codec.Must(f.Genesis), 2)
				if e != nil {
					t.Fatal(e)
				}
				expected := index
				if point == "after_rename" || point == "after_directory_sync" {
					expected++
				}
				if s.Height() != strconv.Itoa(expected) {
					t.Fatalf("partial/double commit: got %s want %d", s.Height(), expected)
				}
				for _, b := range f.Blocks[expected:] {
					if e = s.Commit(codec.Must(b)); e != nil {
						t.Fatal(e)
					}
				}
				fresh, e := Open(t.TempDir(), codec.Must(f.Genesis), 2)
				if e != nil {
					t.Fatal(e)
				}
				for _, b := range f.Blocks {
					if e = fresh.Commit(codec.Must(b)); e != nil {
						t.Fatal(e)
					}
				}
				a, e := s.Export()
				if e != nil {
					t.Fatal(e)
				}
				b, e := fresh.Export()
				if e != nil {
					t.Fatal(e)
				}
				if len(a) != len(b) {
					t.Fatal("record count")
				}
				for i := range a {
					if !bytes.Equal(a[i], b[i]) {
						t.Fatalf("recovered record %d differs", i)
					}
				}
				if s.AppHash() != f.ExpectedRoots[len(f.ExpectedRoots)-1] {
					t.Fatal("recovery changed root")
				}
				for _, block := range f.Blocks {
					for _, tx := range block.Transactions {
						status, height := s.Status(core.TxID(tx.Body))
						if status != "finalized" || height != block.Height {
							t.Fatal("lost finality marker")
						}
					}
				}
			})
		}
	}
}
func TestRejectCorruptCommitAndForeignGenesis(t *testing.T) {
	f := testutil.Load(t, "accounts")
	dir := t.TempDir()
	s, e := Open(dir, codec.Must(f.Genesis), 1)
	if e != nil {
		t.Fatal(e)
	}
	if e = s.Commit(codec.Must(f.Blocks[0])); e != nil {
		t.Fatal(e)
	}
	var g core.Config
	_ = json.Unmarshal(codec.Must(f.Genesis), &g)
	g.Registry.Modules[1].RetirementPolicy = "different_policy"
	if _, e = Open(dir, codec.Must(g), 1); e == nil {
		t.Fatal("accepted foreign genesis")
	}
	path := filepath.Join(dir, commitName("1"))
	raw, e := os.ReadFile(path)
	if e != nil {
		t.Fatal(e)
	}
	corrupt := strings.Replace(string(raw), f.ExpectedRoots[0], strings.Repeat("0", 64), 1)
	if e = os.WriteFile(path, []byte(corrupt), 0600); e != nil {
		t.Fatal(e)
	}
	if _, e = Open(dir, codec.Must(f.Genesis), 1); e == nil {
		t.Fatal("accepted corrupt state")
	}
}
