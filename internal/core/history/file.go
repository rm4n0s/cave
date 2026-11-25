package history

import (
	"bufio"
	"encoding/json"
	"io"
	"os"
	"sort"
	"time"

	"github.com/rm4n0s/cave/internal/core/ids"
	"github.com/rm4n0s/errors"
)

// The kinds of lines (docs/SPEC.md §9.11).
const (
	kindConnected = "connected"
	kindEnded     = "ended"
	kindTransfer  = "transfer"
	kindState     = "state"
)

// line is one line of the file: every kind's fields, the ones a kind does not have left out.
type line struct {
	V    int    `json:"v"`
	Kind string `json:"kind"`
	ID   string `json:"id,omitempty"`

	PersonID       string     `json:"personId,omitempty"`
	PersonName     string     `json:"personName,omitempty"`
	KeyFingerprint string     `json:"keyFingerprint,omitempty"`
	Address        string     `json:"address,omitempty"`
	ClientVersion  string     `json:"clientVersion,omitempty"`
	ConnectedAt    *time.Time `json:"connectedAt,omitempty"`

	EndedAt        *time.Time `json:"endedAt,omitempty"`
	Reason         Reason     `json:"reason,omitempty"`
	BytesIn        int64      `json:"bytesIn,omitempty"`
	BytesOut       int64      `json:"bytesOut,omitempty"`
	Activities     []Activity `json:"activities,omitempty"`
	MoreActivities int        `json:"moreActivities,omitempty"`

	ConnectionID string     `json:"connectionId,omitempty"`
	Path         string     `json:"path,omitempty"`
	OpenedAt     *time.Time `json:"openedAt,omitempty"`
	ClosedAt     *time.Time `json:"closedAt,omitempty"`
	Downloaded   int64      `json:"downloaded,omitempty"`
	Uploaded     int64      `json:"uploaded,omitempty"`
	Created      bool       `json:"created,omitempty"`
	Result       Result     `json:"result,omitempty"`

	LastClearedAt *time.Time `json:"lastClearedAt,omitempty"`
	NextClearAt   *time.Time `json:"nextClearAt,omitempty"`
}

// timePtr returns a pointer to t, or nil for the zero time.
func timePtr(t time.Time) *time.Time {
	if t.IsZero() {
		return nil
	}
	return &t
}

// timeOf returns the time p points to, or the zero time.
func timeOf(p *time.Time) time.Time {
	if p == nil {
		return time.Time{}
	}
	return p.UTC()
}

// connectedLine is the line of a connection's start.
func connectedLine(c Connection) *line {
	return &line{
		V: lineVersion, Kind: kindConnected, ID: c.ID, PersonID: c.PersonID, PersonName: c.PersonName,
		KeyFingerprint: c.KeyFingerprint, Address: c.Address, ClientVersion: c.ClientVersion, ConnectedAt: timePtr(c.ConnectedAt),
	}
}

// endedLine is the line of a connection's end.
func endedLine(c Connection) *line {
	return &line{
		V: lineVersion, Kind: kindEnded, ID: c.ID, EndedAt: timePtr(c.EndedAt), Reason: c.Reason, BytesIn: c.BytesIn,
		BytesOut: c.BytesOut, Activities: c.Activities, MoreActivities: c.MoreActivities,
	}
}

// transferLine is the line of a transfer.
func transferLine(t Transfer) *line {
	return &line{
		V: lineVersion, Kind: kindTransfer, ID: t.ID, ConnectionID: t.ConnectionID, PersonID: t.PersonID, PersonName: t.PersonName,
		Path: t.Path, OpenedAt: timePtr(t.OpenedAt), ClosedAt: timePtr(t.ClosedAt), Downloaded: t.Downloaded, Uploaded: t.Uploaded,
		Created: t.Created, Result: t.Result,
	}
}

// encode turns a line into its bytes, with the newline.
func encode(l *line) ([]byte, error) {
	data, err := json.Marshal(l)
	if err != nil {
		return nil, errors.NewErr(TagEncodeFailed, err).SetMetadata("kind", l.Kind)
	}
	return append(data, '\n'), nil
}

// appender appends lines to the file, opening it when it is not open and putting it aside when
// it is full. Its owner holds the lock of the Store around every call.
type appender struct {
	path string
	// max points at the size the file may reach.
	max *int64
	// header is the line every new file starts with.
	header func() *line
	// file is the open file; nil before the first line, after a failure and after close.
	file *os.File
	size int64
}

// append writes one line. When the file is full it is put aside first; when that fails, the
// line is written to the full file all the same and the failure is returned.
func (a *appender) append(l *line) error {
	data, err := encode(l)
	if err != nil {
		return err
	}
	var rotateErr error
	if a.file != nil && a.size > 0 && a.size+int64(len(data)) > *a.max {
		// The lines are written already; a failed close of a file that is put aside loses nothing.
		_ = a.file.Close()
		a.file = nil
		if err := os.Rename(a.path, a.path+".1"); err != nil {
			rotateErr = errors.NewErr(TagRotateFailed, err).SetMetadata("path", a.path)
		}
	}
	if a.file == nil {
		fresh, err := a.open()
		if err != nil {
			return err
		}
		if fresh && l.Kind == kindState {
			return rotateErr // the new file starts with this very line
		}
	}
	if err := a.raw(data); err != nil {
		return err
	}
	return rotateErr
}

// open opens the file for appending, creating it with mode 0600. A new file gets the header
// line first, and fresh is true; a file whose last line was cut off gets a newline, so that the
// next line stands on its own.
func (a *appender) open() (fresh bool, err error) {
	f, err := os.OpenFile(a.path, os.O_APPEND|os.O_CREATE|os.O_RDWR, 0o600)
	if err == nil {
		var info os.FileInfo
		if info, err = f.Stat(); err == nil {
			a.file, a.size = f, info.Size()
		} else {
			_ = f.Close() // nothing was written; the stat's error is the one that matters
		}
	}
	if err != nil {
		return false, errors.NewErr(TagFileOpen, err).SetMetadata("path", a.path)
	}
	if a.size == 0 {
		data, err := encode(a.header())
		if err != nil {
			return false, err
		}
		return true, a.raw(data)
	}
	last := make([]byte, 1)
	if _, err := a.file.ReadAt(last, a.size-1); err == nil && last[0] != '\n' {
		return false, a.raw([]byte{'\n'})
	}
	return false, nil
}

// raw writes bytes to the open file. After a failure the file is closed, and the next line opens
// it again.
func (a *appender) raw(data []byte) error {
	n, err := a.file.Write(data)
	a.size += int64(n)
	if err != nil {
		_ = a.file.Close() // the write failed already; the next line opens the file again
		a.file = nil
		return errors.NewErr(TagWriteFailed, err).SetMetadata("path", a.path)
	}
	return nil
}

// close closes the file, when it is open.
func (a *appender) close() error {
	if a.file == nil {
		return nil
	}
	f := a.file
	a.file = nil
	if err := f.Close(); err != nil {
		return errors.NewErr(TagCloseFailed, err).SetMetadata("path", a.path)
	}
	return nil
}

// reading is the history being read at start.
type reading struct {
	store *Store
	// skipped counts the lines that could not be.
	skipped int
}

// file reads one file of the history; a missing file has nothing.
func (r *reading) file(path string) error {
	err := scan(path, r.line)
	if err != nil && !os.IsNotExist(err) {
		return errors.NewErr(TagReadFailed, err).SetMetadata("path", path)
	}
	return nil
}

// scan calls fn with every line of the file, without its newline, and counts a line that is
// longer than MaxLineBytes as skipped by calling fn with nil. It returns the operating system's
// errors as they are.
func scan(path string, fn func([]byte)) error {
	f, err := os.Open(path)
	if err != nil {
		return err
	}
	defer func() { _ = f.Close() }() // read only: a failed close loses nothing
	br := bufio.NewReaderSize(f, MaxLineBytes+1)
	for {
		data, err := br.ReadSlice('\n')
		if err == bufio.ErrBufferFull {
			// Too long: skip the rest of it without holding it.
			for err == bufio.ErrBufferFull {
				_, err = br.ReadSlice('\n')
			}
			fn(nil)
		} else if len(data) > 0 {
			if data[len(data)-1] == '\n' {
				data = data[:len(data)-1]
			}
			if len(data) > 0 {
				fn(data)
			}
		}
		if err == io.EOF {
			return nil
		}
		if err != nil {
			return err
		}
	}
}

// line takes one line of the file into the history, or counts it as skipped.
func (r *reading) line(data []byte) {
	if !r.take(data) {
		r.skipped++
	}
}

// take takes one line into the history, and reports whether it could be.
func (r *reading) take(data []byte) bool {
	if data == nil {
		return false
	}
	var l line
	if err := json.Unmarshal(data, &l); err != nil || l.V != lineVersion {
		return false
	}
	s := r.store
	switch l.Kind {
	case kindConnected:
		if !ids.Valid(connectionPrefix, l.ID) || l.ConnectedAt == nil || s.byID[l.ID] != nil {
			return false
		}
		c := cleanConnection(Connection{
			ID: l.ID, PersonID: l.PersonID, PersonName: l.PersonName, KeyFingerprint: l.KeyFingerprint, Address: l.Address,
			ClientVersion: l.ClientVersion, ConnectedAt: timeOf(l.ConnectedAt),
		})
		s.connections = append(s.connections, &c)
		s.byID[c.ID] = &c
	case kindEnded:
		c := s.byID[l.ID]
		if c == nil || !c.Open() || l.EndedAt == nil || !l.Reason.valid() || l.Reason == ReasonUnknown {
			return false
		}
		c.EndedAt, c.Reason, c.BytesIn, c.BytesOut = timeOf(l.EndedAt), l.Reason, max(l.BytesIn, 0), max(l.BytesOut, 0)
		c.Activities, c.MoreActivities = cleanActivities(l.Activities, l.MoreActivities)
	case kindTransfer:
		if !ids.Valid(transferPrefix, l.ID) || l.OpenedAt == nil || l.ClosedAt == nil || !l.Result.valid() {
			return false
		}
		s.transfers = append(s.transfers, cleanTransfer(Transfer{
			ID: l.ID, ConnectionID: l.ConnectionID, PersonID: l.PersonID, PersonName: l.PersonName, Path: l.Path,
			OpenedAt: timeOf(l.OpenedAt), ClosedAt: timeOf(l.ClosedAt), Downloaded: l.Downloaded, Uploaded: l.Uploaded,
			Created: l.Created, Result: l.Result,
		}))
	case kindState:
		s.lastClearedAt, s.nextClearAt = timeOf(l.LastClearedAt), timeOf(l.NextClearAt)
	default:
		return false
	}
	return true
}

// finish ends the reading: a connection without its end ended in a way nobody knows, and only
// the newest records are kept.
func (r *reading) finish() {
	s := r.store
	for _, c := range s.connections {
		if c.Open() {
			c.Reason = ReasonUnknown
		}
	}
	sort.SliceStable(s.connections, func(i, j int) bool { return s.connections[i].ConnectedAt.Before(s.connections[j].ConnectedAt) })
	if extra := len(s.connections) - MaxConnections; extra > 0 {
		s.connections = append([]*Connection(nil), s.connections[extra:]...)
	}
	s.byID = make(map[string]*Connection, len(s.connections))
	for _, c := range s.connections {
		s.byID[c.ID] = c
	}
	sort.SliceStable(s.transfers, func(i, j int) bool { return s.transfers[i].ClosedAt.Before(s.transfers[j].ClosedAt) })
	if extra := len(s.transfers) - MaxTransfers; extra > 0 {
		s.transfers = append([]Transfer(nil), s.transfers[extra:]...)
	}
}
