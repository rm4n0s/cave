package agentfwd

import (
	"regexp"
	"strings"
	"testing"
)

func TestPipeName(t *testing.T) {
	pattern := regexp.MustCompile(`^\\\\\.\\pipe\\cave-agent-[0-9a-f]{32}$`)
	seen := map[string]bool{}
	for i := 0; i < 50; i++ {
		name := pipeName()
		if !pattern.MatchString(name) {
			t.Fatalf("pipeName() = %q", name)
		}
		if seen[name] {
			t.Fatalf("the name %q came twice", name)
		}
		seen[name] = true
	}
}

func TestPipeSDDL(t *testing.T) {
	const sid = "S-1-5-21-1004336348-1177238915-682003330-1000"
	got := pipeSDDL(sid)
	if got != "D:P(A;;GA;;;"+sid+")" {
		t.Errorf("pipeSDDL = %q", got)
	}
	// One protected list, one entry, the SID once: nobody else, no group, no "everyone".
	if !strings.HasPrefix(got, "D:P(") || strings.Count(got, "(") != 1 || strings.Count(got, sid) != 1 || strings.Contains(got, "S:") {
		t.Errorf("the descriptor %q is not one protected entry for the user", got)
	}
}
