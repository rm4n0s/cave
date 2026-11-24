// Package agentfwd is the session's side of agent forwarding (docs/SPEC.md §9.8): a private
// place where the programs of a session find an SSH agent, whose connections Cave carries to the
// agent of the client. On Linux it is a Unix socket; on Windows a named pipe.
package agentfwd

import (
	"io"
	"log/slog"
)

// The Tags of this package's errors.
const (
	TagRuntimeDirFailed     = "AgentRuntimeDirFailed"
	TagDirFailed            = "AgentDirFailed"
	TagListenFailed         = "AgentListenFailed"
	TagModeFailed           = "AgentModeFailed"
	TagAcceptFailed         = "AgentAcceptFailed"
	TagOpenFailed           = "AgentOpenFailed"
	TagPipeUserFailed       = "AgentPipeUserFailed"
	TagPipeDescriptorFailed = "AgentPipeDescriptorFailed"
	TagPipeCreateFailed     = "AgentPipeCreateFailed"
	TagPipeConnectFailed    = "AgentPipeConnectFailed"
	TagPipeNextFailed       = "AgentPipeNextFailed"
)

const (
	// SocketName is the name of the socket in its folder.
	SocketName = "agent.sock"
	// MaxConns is how many connections to the agent a session may have open at once; one more
	// is closed as it arrives.
	MaxConns = 32
)

// Opener opens a connection to the client's agent: an auth-agent@openssh.com channel.
type Opener func() (io.ReadWriteCloser, error)

// Options are what a Socket needs. Every field is required.
type Options struct {
	// RuntimeDir is the folder the socket's own folder is made in (storage.Dirs.Runtime).
	RuntimeDir string
	// Open opens a connection to the client's agent for each program that connects.
	Open Opener
	// Logger gets what fails.
	Logger *slog.Logger
}
