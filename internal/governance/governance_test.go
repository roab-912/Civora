package governance

import "testing"

func TestDecisionThresholds(t *testing.T) {
	for _, c := range []struct {
		name, y, n, e        string
		constitutional, want bool
		invalid              bool
	}{
		{"ordinary-majority", "2", "1", "8", false, true, false}, {"tie", "1", "1", "8", false, false, false}, {"empty", "0", "0", "8", false, false, false}, {"constitutional-exact-two-thirds", "2", "1", "8", true, true, false}, {"constitutional-majority-insufficient", "3", "2", "8", true, false, false}, {"constitutional-turnout-insufficient", "2", "1", "20", true, false, false}, {"constitutional-turnout-exact", "4", "0", "20", true, true, false}, {"count-exceeds-electorate", "2", "1", "2", false, false, true}, {"overflow", "18446744073709551615", "1", "18446744073709551615", false, false, true},
	} {
		t.Run(c.name, func(t *testing.T) {
			got, err := Adopted(c.y, c.n, c.e, DefaultPolicy(), c.constitutional)
			if (err != nil) != c.invalid || got != c.want {
				t.Fatalf("got %v %v", got, err)
			}
		})
	}
	p := DefaultPolicy()
	p.OrdinaryQuorumBPS = "5000"
	if got, e := Adopted("3", "0", "8", p, false); e != nil || got {
		t.Fatal(got, e)
	}
}
func TestPolicyBounds(t *testing.T) {
	if e := DefaultPolicy().Validate(); e != nil {
		t.Fatal(e)
	}
	for _, mutate := range []func(*Policy){func(p *Policy) { p.VotingMS = "1" }, func(p *Policy) { p.VotingMS = str(15 * Day) }, func(p *Policy) { p.SetupMS = str(31 * Day) }, func(p *Policy) { p.ConstitutionalExecutionDelayMS = str(Hour) }, func(p *Policy) { p.EndorsementsRequired = "0" }, func(p *Policy) { p.EndorsementsRequired = "21" }, func(p *Policy) { p.OrdinaryQuorumBPS = "5001" }, func(p *Policy) { p.ConstitutionalQuorumBPS = "1999" }} {
		p := DefaultPolicy()
		mutate(&p)
		if e := p.Validate(); e == nil {
			t.Fatal("invalid policy accepted")
		}
	}
}
