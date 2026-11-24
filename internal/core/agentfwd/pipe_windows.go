package agentfwd

import (
	"io"
	"sync"
	"unsafe"

	"github.com/rm4n0s/errors"
	"golang.org/x/sys/windows"
)

// pipeBuffer is the size of a pipe's buffers in each direction.
const pipeBuffer = 64 << 10

// errPipeClosed is what a read, a write or an accept of a pipe that was closed answers.
var errPipeClosed = io.ErrClosedPipe

// pipeListener listens on a named pipe that only the current user may open (docs/SPEC.md §13.5).
// It keeps one instance of the pipe waiting for a client, so that a client that arrives is never
// told the pipe is not there. Overlapped I/O lets Close end a wait that is going on.
type pipeListener struct {
	name       string
	descriptor *windows.SECURITY_DESCRIPTOR // kept alive for the attributes
	attributes windows.SecurityAttributes

	mu     sync.Mutex
	closed bool
	// pending is the instance that waits for the next client; 0 while Accept has it.
	pending windows.Handle
	// accepting is the instance that Accept is connecting, so that Close can cancel the wait.
	accepting windows.Handle
}

// listenPipe makes the pipe with the name for the current user. The first instance is made with
// FILE_FLAG_FIRST_PIPE_INSTANCE: when anybody had made a pipe of that name before, it fails.
func listenPipe(name string) (*pipeListener, error) {
	user, err := windows.GetCurrentProcessToken().GetTokenUser()
	if err != nil {
		return nil, errors.NewErr(TagPipeUserFailed, err)
	}
	descriptor, err := windows.SecurityDescriptorFromString(pipeSDDL(user.User.Sid.String()))
	if err != nil {
		return nil, errors.NewErr(TagPipeDescriptorFailed, err)
	}
	l := &pipeListener{name: name, descriptor: descriptor}
	l.attributes = windows.SecurityAttributes{Length: uint32(unsafe.Sizeof(l.attributes)), SecurityDescriptor: descriptor}
	handle, err := l.instance(true)
	if err != nil {
		return nil, errors.NewErr(TagPipeCreateFailed, err).SetMetadata("name", name)
	}
	l.pending = handle
	return l, nil
}

// instance makes one instance of the pipe, for overlapped I/O, that remote computers cannot reach.
func (l *pipeListener) instance(first bool) (windows.Handle, error) {
	namePtr, err := windows.UTF16PtrFromString(l.name)
	if err != nil {
		return 0, err
	}
	open := uint32(windows.PIPE_ACCESS_DUPLEX | windows.FILE_FLAG_OVERLAPPED)
	if first {
		open |= windows.FILE_FLAG_FIRST_PIPE_INSTANCE
	}
	mode := uint32(windows.PIPE_TYPE_BYTE | windows.PIPE_READMODE_BYTE | windows.PIPE_WAIT | windows.PIPE_REJECT_REMOTE_CLIENTS)
	return windows.CreateNamedPipe(namePtr, open, mode, windows.PIPE_UNLIMITED_INSTANCES, pipeBuffer, pipeBuffer, 0, &l.attributes)
}

// Accept waits for a client and returns its connection. It returns errPipeClosed once the
// listener is closed, also when it was waiting.
func (l *pipeListener) Accept() (*pipeConn, error) {
	l.mu.Lock()
	if l.closed {
		l.mu.Unlock()
		return nil, errPipeClosed
	}
	handle := l.pending
	l.pending, l.accepting = 0, handle
	l.mu.Unlock()

	err := l.connect(handle)
	l.mu.Lock()
	l.accepting = 0
	closed := l.closed
	l.mu.Unlock()
	if err != nil || closed {
		_ = windows.CloseHandle(handle) // nobody is connected, or nobody may be any more
		if closed {
			return nil, errPipeClosed
		}
		return nil, errors.NewErr(TagPipeConnectFailed, err).SetMetadata("name", l.name)
	}
	// The next client finds an instance waiting.
	next, err := l.instance(false)
	if err != nil {
		_ = windows.CloseHandle(handle) // the pipe cannot go on: this client is turned away with the others
		return nil, errors.NewErr(TagPipeNextFailed, err).SetMetadata("name", l.name)
	}
	l.mu.Lock()
	if l.closed {
		l.mu.Unlock()
		_ = windows.CloseHandle(next)
		_ = windows.CloseHandle(handle)
		return nil, errPipeClosed
	}
	l.pending = next
	l.mu.Unlock()
	return &pipeConn{handle: handle}, nil
}

// connect waits for a client on the instance, until Close cancels the wait.
func (l *pipeListener) connect(handle windows.Handle) error {
	event, err := windows.CreateEvent(nil, 1, 0, nil)
	if err != nil {
		return err
	}
	defer func() { _ = windows.CloseHandle(event) }()
	overlapped := windows.Overlapped{HEvent: event}
	err = windows.ConnectNamedPipe(handle, &overlapped)
	switch err {
	case nil, windows.ERROR_PIPE_CONNECTED: // a client was there already
		return nil
	case windows.ERROR_IO_PENDING:
	default:
		return err
	}
	// Close may have run before the wait began, and found nothing to cancel.
	l.mu.Lock()
	closed := l.closed
	l.mu.Unlock()
	if closed {
		_ = windows.CancelIoEx(handle, &overlapped) // nothing to cancel if it completed meanwhile
	}
	var done uint32
	return windows.GetOverlappedResult(handle, &overlapped, &done, true)
}

// Close stops the listener: a wait in Accept ends, and the instance that waits is closed. A
// second call does nothing.
func (l *pipeListener) Close() {
	l.mu.Lock()
	if l.closed {
		l.mu.Unlock()
		return
	}
	l.closed = true
	pending, accepting := l.pending, l.accepting
	l.pending = 0
	l.mu.Unlock()
	if accepting != 0 {
		_ = windows.CancelIoEx(accepting, nil) // Accept closes the instance when it wakes up
	}
	if pending != 0 {
		_ = windows.CloseHandle(pending)
	}
}

// pipeConn is a client of the pipe: a byte stream that Close can end while a read or a write
// waits.
type pipeConn struct {
	handle windows.Handle

	mu     sync.Mutex
	closed bool
	// ops counts the reads and writes going on.
	ops sync.WaitGroup
	// readMu and writeMu keep one read and one write at a time.
	readMu, writeMu sync.Mutex
}

// begin starts an operation; it fails when the connection is closed.
func (c *pipeConn) begin() bool {
	c.mu.Lock()
	defer c.mu.Unlock()
	if c.closed {
		return false
	}
	c.ops.Add(1)
	return true
}

func (c *pipeConn) isClosed() bool {
	c.mu.Lock()
	defer c.mu.Unlock()
	return c.closed
}

// transfer runs one overlapped read or write and waits for it; Close cancels it.
func (c *pipeConn) transfer(start func(overlapped *windows.Overlapped, done *uint32) error) (int, error) {
	if !c.begin() {
		return 0, errPipeClosed
	}
	defer c.ops.Done()
	event, err := windows.CreateEvent(nil, 1, 0, nil)
	if err != nil {
		return 0, err
	}
	defer func() { _ = windows.CloseHandle(event) }()
	overlapped := windows.Overlapped{HEvent: event}
	var done uint32
	err = start(&overlapped, &done)
	if err == windows.ERROR_IO_PENDING {
		if c.isClosed() {
			_ = windows.CancelIoEx(c.handle, &overlapped) // Close ran before this wait, and could not cancel it
		}
		err = windows.GetOverlappedResult(c.handle, &overlapped, &done, true)
	}
	switch err {
	case nil:
		return int(done), nil
	case windows.ERROR_BROKEN_PIPE, windows.ERROR_PIPE_NOT_CONNECTED, windows.ERROR_HANDLE_EOF:
		return int(done), io.EOF
	case windows.ERROR_OPERATION_ABORTED:
		return int(done), errPipeClosed
	}
	return int(done), err
}

// Read reads what the client wrote; io.EOF when the client closed its end.
func (c *pipeConn) Read(p []byte) (int, error) {
	if len(p) == 0 {
		return 0, nil
	}
	c.readMu.Lock()
	defer c.readMu.Unlock()
	return c.transfer(func(overlapped *windows.Overlapped, done *uint32) error {
		return windows.ReadFile(c.handle, p, done, overlapped)
	})
}

// Write writes all of p to the client.
func (c *pipeConn) Write(p []byte) (int, error) {
	c.writeMu.Lock()
	defer c.writeMu.Unlock()
	written := 0
	for written < len(p) {
		n, err := c.transfer(func(overlapped *windows.Overlapped, done *uint32) error {
			return windows.WriteFile(c.handle, p[written:], done, overlapped)
		})
		written += n
		if err != nil {
			return written, err
		}
		if n == 0 {
			return written, io.ErrShortWrite
		}
	}
	return written, nil
}

// Close ends the connection: a read or a write that waits ends, and the client sees the end. A
// second call does nothing.
func (c *pipeConn) Close() error {
	c.mu.Lock()
	if c.closed {
		c.mu.Unlock()
		return nil
	}
	c.closed = true
	c.mu.Unlock()
	_ = windows.CancelIoEx(c.handle, nil) // the operations that are waiting end
	c.ops.Wait()
	_ = windows.DisconnectNamedPipe(c.handle) // the client sees the end after it has read what was written
	return windows.CloseHandle(c.handle)
}
