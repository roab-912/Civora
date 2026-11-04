//go:build civora_sim

package store

import (
	"bufio"
	"bytes"
	"civora/internal/codec"
	"civora/internal/simulation"
	"civora/internal/testutil"
	"fmt"
	"os"
	"os/exec"
	"strconv"
	"testing"
	"time"
)

func governanceFixture(t *testing.T) simulation.Fixture {
	t.Helper()
	var f simulation.Fixture
	if e := codec.Decode(testutil.Read(t, "v0.2/governance"), "fixture", &f); e != nil {
		t.Fatal(e)
	}
	return f
}
func TestGovernanceCrashHelper(t *testing.T) {
	if os.Getenv("CIVORA_GOV_CRASH_HELPER") != "1" {
		return
	}
	f := governanceFixture(t)
	s, e := OpenWithFactory(os.Getenv("CIVORA_TEST_STORE"), codec.Must(f.Genesis), 1, simulation.New)
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
	t.Fatal("hook not reached")
}
func TestGovernanceAbruptDeathAtBallotAndEffect(t *testing.T) {
	f := governanceFixture(t)
	genesis := codec.Must(f.Genesis)
	gold, e := OpenWithFactory(t.TempDir(), genesis, 1, simulation.New)
	if e != nil {
		t.Fatal(e)
	}
	for _, b := range f.Blocks {
		if e = gold.Commit(codec.Must(b)); e != nil {
			t.Fatal(e)
		}
	}
	expected, e := gold.Export()
	if e != nil {
		t.Fatal(e)
	}
	ballotIndex, effectIndex := -1, -1
	for i, b := range f.Blocks {
		if ballotIndex < 0 {
			for _, tx := range b.Transactions {
				if tx.Body.Type == "vote.cast" {
					ballotIndex = i
					break
				}
			}
		}
		if effectIndex < 0 {
			for _, ev := range f.ExpectedEvents[i] {
				if ev.Type == "proposal.applied" {
					effectIndex = i
					break
				}
			}
		}
	}
	if ballotIndex < 0 || effectIndex < 0 {
		t.Fatal("fixture has no ballot/effect")
	}
	for _, index := range []int{ballotIndex, effectIndex} {
		for _, point := range []string{"partial_write", "after_write", "after_sync", "after_rename", "after_directory_sync"} {
			t.Run(fmt.Sprint(index, "/", point), func(t *testing.T) {
				dir := t.TempDir()
				s, e := OpenWithFactory(dir, genesis, 1, simulation.New)
				if e != nil {
					t.Fatal(e)
				}
				for _, b := range f.Blocks[:index] {
					if e = s.Commit(codec.Must(b)); e != nil {
						t.Fatal(e)
					}
				}
				cmd := exec.Command(os.Args[0], "-test.run=^TestGovernanceCrashHelper$")
				cmd.Env = append(os.Environ(), "CIVORA_GOV_CRASH_HELPER=1", "CIVORA_TEST_STORE="+dir, "CIVORA_TEST_BLOCK_INDEX="+strconv.Itoa(index), "CIVORA_TEST_KILL_POINT="+point)
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
						t.Fatal("helper exited", stderr.String())
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
					t.Fatal("helper was not killed")
				}
				s, e = OpenWithFactory(dir, genesis, 1, simulation.New)
				if e != nil {
					t.Fatal(e)
				}
				start := index
				if point == "after_rename" || point == "after_directory_sync" {
					start++
				}
				if s.Height() != strconv.Itoa(start) {
					t.Fatal("partial or double effect")
				}
				for _, b := range f.Blocks[start:] {
					if e = s.Commit(codec.Must(b)); e != nil {
						t.Fatal(e)
					}
				}
				actual, e := s.Export()
				if e != nil {
					t.Fatal(e)
				}
				if len(actual) != len(expected) {
					t.Fatal("record count")
				}
				for i := range actual {
					if !bytes.Equal(actual[i], expected[i]) {
						t.Fatal("recovery differs at block", i+1)
					}
				}
			})
		}
	}
}
