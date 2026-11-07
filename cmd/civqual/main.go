// civqual verifies every receipt in a canonical JSONL measurement stream.
// The frozen context hash must be authenticated independently of that stream.
package main

import (
	"civora/internal/codec"
	"civora/internal/partition"
	"civora/internal/qualification"
	"civora/internal/recovery"
	"flag"
	"fmt"
	"io"
	"os"
	"strings"
)

func run() error {
	context := flag.String("context", "", "canonical independently authenticated frozen context")
	pin := flag.String("context-pin", "", "SHA3-384 pin obtained independently")
	samples := flag.String("samples", "", "canonical JSONL receipt samples")
	start := flag.Uint64("start-ms", 0, "measurement window start in the client clock")
	end := flag.Uint64("end-ms", 0, "exclusive measurement window end in the same clock")
	output := flag.String("output", "", "new immutable UTF-8 report file (otherwise stdout)")
	flag.Parse()
	if flag.NArg() != 0 {
		return fmt.Errorf("unexpected arguments")
	}
	file, e := os.Open(*context)
	if e != nil {
		return e
	}
	raw, e := io.ReadAll(io.LimitReader(file, 2*1024*1024+1))
	file.Close()
	if e != nil {
		return e
	}
	if len(raw) > 2*1024*1024 || recovery.HashBytes(raw) != *pin {
		return fmt.Errorf("context pin mismatch")
	}
	var f partition.Frozen
	if e = codec.Decode(raw, "partition_frozen", &f); e != nil {
		return e
	}
	if len(f.Lanes) == 0 {
		return fmt.Errorf("no lanes")
	}
	if e = f.Validate(strings.TrimSuffix(f.Lanes[0].ChainID, "-v00")); e != nil {
		return e
	}
	input, e := os.Open(*samples)
	if e != nil {
		return e
	}
	defer input.Close()
	report, e := qualification.AnalyzeReceipts(f, input, *start, *end)
	if e != nil {
		return e
	}
	data := append(codec.Must(report), '\n')
	if *output != "" {
		file, e := os.OpenFile(*output, os.O_WRONLY|os.O_CREATE|os.O_EXCL, 0600)
		if e != nil {
			return e
		}
		_, writeErr := file.Write(data)
		closeErr := file.Close()
		if writeErr != nil {
			return writeErr
		}
		return closeErr
	}
	_, e = os.Stdout.Write(data)
	return e
}
func main() {
	if e := run(); e != nil {
		fmt.Fprintln(os.Stderr, e)
		os.Exit(1)
	}
}
