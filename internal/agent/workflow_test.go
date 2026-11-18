package agent

import (
	"strings"
	"testing"
)

func TestEveryFlavorIsComplete(t *testing.T) {
	for _, w := range Workflows {
		if len(w.Flavors) == 0 || w.Describe == "" {
			t.Errorf("%s: needs a description and at least one flavor", w.Domain)
		}
		if d, ok := ParseDomain(string(w.Domain)); !ok || d != w.Domain {
			t.Errorf("%s does not parse as itself", w.Domain)
		}
		for _, f := range w.Flavors {
			tr := Track{Domain: w.Domain, Flavor: f.Name}
			if f.Describe == "" || len(f.Phases) == 0 {
				t.Errorf("%s: needs a description and phases", tr)
			}
			if got := tr.Lenses(4); len(got) != 4 {
				t.Errorf("%s: want 4 lenses, got %d", tr, len(got))
			}
			if !strings.Contains(tr.instructions(), f.Phases[0]) {
				t.Errorf("%s: instructions miss the phases", tr)
			}
		}
	}
}

func TestSoftwareWorkRules(t *testing.T) {
	tr := Track{Domain: DomainSoftware, Flavor: "work"}
	in := tr.instructions()
	for _, want := range []string{"anticipation code", "caller", "Security", "Loop until everything passes"} {
		if !strings.Contains(in, want) {
			t.Errorf("software/work instructions should mention %q", want)
		}
	}
	if l := tr.Lenses(4); !strings.HasPrefix(l[0], "anticipation code") {
		t.Errorf("work reviews should start with the anticipation-code lens: %q", l[0])
	}
}

func TestTrackDefaultsAndCycling(t *testing.T) {
	tr := Track{Domain: DomainSoftware}
	if tr.FlavorSpec().Name != "project" {
		t.Errorf("software defaults to project, got %s", tr.FlavorSpec().Name)
	}
	if _, ok := tr.WithFlavor("nope"); ok {
		t.Error("unknown flavor should not be accepted")
	}
	if c, ok := (Track{Domain: DomainCyber}).WithFlavor("BLUE"); !ok || c.String() != "cyber/blue" {
		t.Errorf("flavor names are case-insensitive: %v %v", c, ok)
	}
	d := DomainSoftware
	for range Workflows {
		d = NextDomain(d)
	}
	if d != DomainSoftware {
		t.Errorf("cycling through every domain should come back to software, got %s", d)
	}
}
