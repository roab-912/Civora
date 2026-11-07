// Package qualification measures authenticated receipts. These measurements do
// not qualify the installation, secret ballots, complete audits or operator independence.
package qualification

import (
	"bufio"
	"civora/internal/codec"
	"civora/internal/partition"
	"crypto/sha256"
	"encoding/json"
	"fmt"
	"io"
)

type Sample struct {
	Sent     string             `json:"sent_at_ms"`
	Received string             `json:"received_at_ms"`
	Receipt  *partition.Receipt `json:"receipt"`
}
type Report struct {
	Format       string   `json:"format"`
	Network      string   `json:"network_id"`
	Poll         string   `json:"poll_id"`
	Start        string   `json:"window_start_ms"`
	End          string   `json:"window_end_ms"`
	Lanes        string   `json:"partitions"`
	Attempts     string   `json:"attempts"`
	Unique       string   `json:"verified_unique_receipts"`
	Duplicates   string   `json:"duplicates"`
	Failures     string   `json:"failed_attempts"`
	P95          string   `json:"receipt_p95_ms"`
	P99          string   `json:"receipt_p99_ms"`
	MaxLatency   string   `json:"receipt_max_ms"`
	Cap          string   `json:"histogram_cap_ms"`
	MinRate      string   `json:"minimum_receipts_per_second"`
	Counts       []string `json:"per_second"`
	PerLane      []string `json:"per_lane"`
	AverageBytes string   `json:"average_envelope_bytes"`
	MaxBytes     string   `json:"maximum_envelope_bytes"`
	Qualified    bool     `json:"public_qualified"`
}
type accumulator struct {
	start, end, attempts, unique, duplicates, failures, maxLatency, totalBytes, maxBytes uint64
	histogram                                                                            [60002]uint64
	seconds, lanes                                                                       []uint64
	seen                                                                                 map[[32]byte]struct{}
}

func (s *accumulator) add(id [32]byte, lane, sent, received, size uint64) error {
	if received < sent || received < s.start || received >= s.end || lane >= uint64(len(s.lanes)) {
		return fmt.Errorf("measurement outside the declared clock/window/lane")
	}
	if _, ok := s.seen[id]; ok {
		s.duplicates++
		return nil
	}
	s.seen[id] = struct{}{}
	s.unique++
	s.lanes[lane]++
	s.seconds[(received-s.start)/1000]++
	latency := received - sent
	if latency > s.maxLatency {
		s.maxLatency = latency
	}
	bucket := latency
	if bucket > 60000 {
		bucket = 60001
	}
	s.histogram[bucket]++
	s.totalBytes += size
	if size > s.maxBytes {
		s.maxBytes = size
	}
	return nil
}
func (s *accumulator) percentile(percent uint64) string {
	if s.unique == 0 {
		return "0"
	}
	rank := (s.unique*percent + 99) / 100
	var n uint64
	for i, count := range s.histogram {
		n += count
		if n >= rank {
			return fmt.Sprint(i)
		}
	}
	panic("histogram inconsistent")
}
func AnalyzeReceipts(f partition.Frozen, input io.Reader, start, end uint64) (Report, error) {
	if end <= start || end-start > 86400000 || (end-start)%1000 != 0 {
		return Report{}, fmt.Errorf("window must contain whole seconds, at most 24 hours")
	}
	s := accumulator{start: start, end: end, seconds: make([]uint64, (end-start)/1000), lanes: make([]uint64, len(f.Lanes)), seen: map[[32]byte]struct{}{}}
	scanner := bufio.NewScanner(input)
	scanner.Buffer(make([]byte, 65536), 2*1024*1024)
	for scanner.Scan() {
		if s.attempts >= 20000000 {
			return Report{}, fmt.Errorf("sample capacity 20 million exceeded")
		}
		s.attempts++
		raw := scanner.Bytes()
		v, e := codec.Parse(raw)
		if e != nil {
			return Report{}, fmt.Errorf("sample %d: %w", s.attempts, e)
		}
		object, ok := v.(map[string]any)
		if !ok || len(object) != 3 {
			return Report{}, fmt.Errorf("sample requires exactly sent_at_ms, received_at_ms, receipt")
		}
		for _, key := range []string{"sent_at_ms", "received_at_ms", "receipt"} {
			if _, ok := object[key]; !ok {
				return Report{}, fmt.Errorf("missing sample field %s", key)
			}
		}
		var sample Sample
		if e = json.Unmarshal(raw, &sample); e != nil {
			return Report{}, e
		}
		sent, e := codec.Uint(sample.Sent)
		if e != nil {
			return Report{}, e
		}
		received, e := codec.Uint(sample.Received)
		if e != nil {
			return Report{}, e
		}
		if received < sent || received < start || received >= end {
			return Report{}, fmt.Errorf("sample timestamp outside declared window")
		}
		if sample.Receipt == nil {
			s.failures++
			continue
		}
		if e = partition.VerifyReceipt(f, *sample.Receipt); e != nil {
			s.failures++
			continue
		}
		envelope, e := codec.Blob(sample.Receipt.Envelope)
		if e != nil {
			return Report{}, e
		}
		lane, e := codec.Uint(sample.Receipt.Reference.Lane)
		if e != nil {
			return Report{}, e
		}
		if e = s.add(sha256.Sum256(envelope), lane, sent, received, uint64(len(envelope))); e != nil {
			return Report{}, e
		}
	}
	if e := scanner.Err(); e != nil {
		return Report{}, e
	}
	minRate := s.seconds[0]
	counts := []string{}
	lanes := []string{}
	for _, n := range s.seconds {
		if n < minRate {
			minRate = n
		}
		counts = append(counts, fmt.Sprint(n))
	}
	for _, n := range s.lanes {
		lanes = append(lanes, fmt.Sprint(n))
	}
	average := uint64(0)
	if s.unique > 0 {
		average = s.totalBytes / s.unique
	}
	return Report{"civic-receipt-measurements-1", f.Context.NetworkID, f.Context.PollID, fmt.Sprint(start), fmt.Sprint(end), fmt.Sprint(len(f.Lanes)), fmt.Sprint(s.attempts), fmt.Sprint(s.unique), fmt.Sprint(s.duplicates), fmt.Sprint(s.failures), s.percentile(95), s.percentile(99), fmt.Sprint(s.maxLatency), "60000", fmt.Sprint(minRate), counts, lanes, fmt.Sprint(average), fmt.Sprint(s.maxBytes), false}, nil
}
