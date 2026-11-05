package partition

import (
	"fmt"
	"testing"
)

func TestRFC9162AllPrefixes(t *testing.T) {
	xs := [][]byte{}
	for n := 1; n <= 65; n++ {
		xs = append(xs, []byte(fmt.Sprint(n)))
		for m := 0; m <= n; m++ {
			p, e := Consistency(xs, m)
			if e != nil {
				t.Fatal(e)
			}
			if e = VerifyConsistency(LogRoot(xs[:m]), LogRoot(xs), uint64(m), uint64(n), p); e != nil {
				t.Fatalf("%d/%d: %v", m, n, e)
			}
			if m > 0 && m < n && VerifyConsistency(LogRoot(xs[:m]), LogRoot([][]byte{[]byte("forged")}), uint64(m), uint64(n), p) == nil {
				t.Fatal("accepted fork")
			}
		}
		for i := range xs {
			p, _ := Inclusion(xs, i)
			if e := VerifyInclusion(LogRoot(xs), xs[i], uint64(i), uint64(n), p); e != nil {
				t.Fatalf("%d/%d: %v", i, n, e)
			}
		}
	}
}
