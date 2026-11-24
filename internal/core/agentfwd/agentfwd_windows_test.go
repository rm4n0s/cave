package agentfwd

import (
	"context"
	"testing"

	"github.com/rm4n0s/cave/internal/core/errtest"
)

func TestAgentForwardingIsNotBuiltForWindowsYet(t *testing.T) {
	socket, err := Listen(context.Background(), Options{})
	errtest.Route(t, err, "Listen.AgentUnsupported")
	if socket != nil {
		t.Errorf("Listen = %v", socket)
	}
	// The methods are there for the code that is shared with Linux.
	var none *Socket
	none.Close()
	if none.Path() != "" {
		t.Errorf("Path() = %q", none.Path())
	}
}
