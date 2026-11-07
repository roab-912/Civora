// civrecovery is an independent off-chain verifier. No consensus connection or
// private recovery key is needed. It never approves an activation or imports state.
package main

import (
	"civora/internal/codec"
	"civora/internal/recovery"
	"crypto/sha3"
	"encoding/hex"
	"encoding/json"
	"flag"
	"fmt"
	"io"
	"os"
	"time"
)

func read(path string) ([]byte, error) {
	f, e := os.Open(path)
	if e != nil {
		return nil, e
	}
	defer f.Close()
	b, e := io.ReadAll(io.LimitReader(f, 2*1024*1024+1))
	if len(b) > 2*1024*1024 {
		return nil, fmt.Errorf("input exceeds 2 MiB")
	}
	return b, e
}
func run(args []string) error {
	if len(args) == 0 {
		return fmt.Errorf("usage: civrecovery verify|apply|status|hash-archive [flags]")
	}
	fs := flag.NewFlagSet(args[0], flag.ContinueOnError)
	rootPath := fs.String("root", "", "independently installed recovery root")
	pin := fs.String("root-pin", "", "trusted SHA3-384 of the exact canonical root bytes")
	bundlePath := fs.String("bundle", "", "canonical recovery bundle")
	storePath := fs.String("store", "", "private durable verifier database")
	archive := fs.String("archive", "", "archived bytes committed by recovery.publish (no automatic import)")
	test := fs.Bool("allow-test-keys", false, "explicitly allow development_TEST_ONLY roots")
	nowArg := fs.Uint64("now-ms", 0, "TEST_ONLY exercise clock")
	if e := fs.Parse(args[1:]); e != nil {
		return e
	}
	if fs.NArg() != 0 {
		return fmt.Errorf("unexpected arguments")
	}
	if args[0] == "hash-archive" {
		f, e := os.Open(*archive)
		if e != nil {
			return e
		}
		defer f.Close()
		h := sha3.New384()
		n, e := io.Copy(h, f)
		if e != nil {
			return e
		}
		return json.NewEncoder(os.Stdout).Encode(map[string]string{"sha3_384": hex.EncodeToString(h.Sum(nil)), "size": fmt.Sprint(n)})
	}
	if args[0] != "verify" && args[0] != "apply" && args[0] != "status" {
		return fmt.Errorf("unknown command")
	}
	now := uint64(time.Now().UnixMilli())
	if *nowArg != 0 {
		if !*test {
			return fmt.Errorf("exercise clock requires --allow-test-keys")
		}
		now = *nowArg
	}
	raw, e := read(*rootPath)
	if e != nil {
		return e
	}
	r, e := recovery.DecodeRoot(raw, *pin, now, *test)
	if e != nil {
		return e
	}
	var b recovery.Bundle
	if args[0] != "status" {
		raw, e = read(*bundlePath)
		if e != nil {
			return e
		}
		if e = codec.Decode(raw, "recovery_bundle", &b); e != nil {
			return e
		}
	}
	if *archive != "" {
		if b.Order.Type != "recovery.publish" {
			return fmt.Errorf("archive checking requires recovery.publish")
		}
		f, e := os.Open(*archive)
		if e != nil {
			return e
		}
		h := sha3.New384()
		_, e = io.Copy(h, f)
		f.Close()
		if e != nil {
			return e
		}
		if hex.EncodeToString(h.Sum(nil)) != b.Order.Reference {
			return fmt.Errorf("ERR_RECOVERY_ARCHIVE_HASH")
		}
	}
	var state any
	if args[0] == "verify" {
		if e = recovery.Verify(r, b, now, 0, *test); e != nil {
			return e
		}
		state = map[string]any{"verified": true, "read_only": true, "persistent_replay_check": false, "activation_authorized": false, "order": b.Order}
	} else {
		if *storePath == "" {
			return fmt.Errorf("--store required")
		}
		s, e := recovery.Open(*storePath, r, now, *test)
		if e != nil {
			return e
		}
		defer s.Close()
		if args[0] == "apply" {
			if e = s.Apply(b, now); e != nil {
				return e
			}
		}
		state, e = s.Snapshot()
		if e != nil {
			return e
		}
	}
	return json.NewEncoder(os.Stdout).Encode(state)
}
func main() {
	if e := run(os.Args[1:]); e != nil {
		fmt.Fprintln(os.Stderr, e)
		os.Exit(1)
	}
}
