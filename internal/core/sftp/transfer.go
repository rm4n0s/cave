package sftp

import "time"

// The results of a transfer (docs/SPEC.md §9.11).
const (
	// ResultDone: the client closed the file.
	ResultDone = "done"
	// ResultFailed: a read that was not at the end of the file, or a write, failed.
	ResultFailed = "failed"
	// ResultInterrupted: the session ended while the file was open.
	ResultInterrupted = "interrupted"
)

// Transfer is one file a session read from or wrote to, from its opening to its closing.
type Transfer struct {
	// Path is the file the client opened, made absolute the way REALPATH answers.
	Path string
	// OpenedAt and ClosedAt are when the file was opened and closed.
	OpenedAt, ClosedAt time.Time
	// Downloaded and Uploaded are the bytes the client read and wrote.
	Downloaded, Uploaded int64
	// Created says that the file was opened to be created or emptied.
	Created bool
	// Result is ResultDone, ResultFailed or ResultInterrupted.
	Result string
}
