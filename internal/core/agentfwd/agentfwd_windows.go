package agentfwd

import (
	"context"

	"github.com/rm4n0s/errors"
)

// Socket is the place where a session finds the forwarded agent. On Windows it is a named pipe,
// built by step 7.4 (docs/SPEC.md §13.5).
type Socket struct{}

// Listen makes the agent's place for a session. Until step 7.4 there is none on Windows, and the
// request for agent forwarding is refused.
func Listen(context.Context, Options) (*Socket, error) {
	return nil, errors.New(TagUnsupported, "agent forwarding is not built for Windows yet")
}

// Path returns what the session's SSH_AUTH_SOCK is set to.
func (s *Socket) Path() string { return "" }

// Close stops the forwarding.
func (s *Socket) Close() {}
