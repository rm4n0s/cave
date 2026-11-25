// Package history keeps the history of Cave's connections and of the files moved over SFTP: the
// records, kept in memory with a limit and appended as JSON lines to a file of the state folder
// that is put aside when it grows past its limit, read back at start, and emptied by hand or by
// the setting history.clear (docs/SPEC.md §9.11).
package history

import (
	"context"
	"log/slog"
	"os"
	"path/filepath"
	"slices"
	"sort"
	"sync"
	"time"

	"github.com/rm4n0s/cave/internal/core/attempts"
	"github.com/rm4n0s/cave/internal/core/clock"
	"github.com/rm4n0s/cave/internal/core/ids"
	"github.com/rm4n0s/cave/internal/core/sanitize"
	"github.com/rm4n0s/cave/internal/core/storage"
	"github.com/rm4n0s/errors"
)

// The Tags of this package's errors.
const (
	TagOpenCancelled = "HistoryOpenCancelled"
	TagReadFailed    = "HistoryReadFailed"
	TagFileOpen      = "HistoryFileOpenFailed"
	TagWriteFailed   = "HistoryWriteFailed"
	TagRotateFailed  = "HistoryRotateFailed"
	TagClearCancel   = "HistoryClearCancelled"
	TagClearFailed   = "HistoryClearFailed"
	TagCloseFailed   = "HistoryCloseFailed"
	TagEncodeFailed  = "HistoryEncodeFailed"
)

// FileName is the name of the history file in the state folder; the file put aside has ".1"
// after it.
const FileName = "history.jsonl"

// The limits of docs/SPEC.md §9.11.
const (
	// MaxFileBytes is the size the file may reach; the line that would pass it starts a new file.
	MaxFileBytes = 4 << 20
	// MaxConnections is how many connections the history keeps in memory, the newest.
	MaxConnections = 2000
	// MaxTransfers is how many transfers the history keeps in memory, the newest.
	MaxTransfers = 5000
	// MaxActivities is how many different activities a connection's record keeps.
	MaxActivities = 32
	// MaxLineBytes is how long a line of the file may be; a longer one is skipped when the file
	// is read.
	MaxLineBytes = 64 << 10
	// MaxPathLength is how many characters the path of a transfer keeps.
	MaxPathLength = 256

	// maxNameLength bounds a person's name and id.
	maxNameLength = 32
	// maxTextLength bounds a client version and an address.
	maxTextLength = 64
	// maxFingerprintLength bounds a key fingerprint.
	maxFingerprintLength = 128
	// maxKindLength bounds the kind of an activity.
	maxKindLength = 32
	// maxDetailLength bounds the detail of an activity, as the registry does.
	maxDetailLength = 80
	// lineVersion is the version of the lines this code reads and writes.
	lineVersion = 1
	// connectionPrefix and transferPrefix start the ids of connections and transfers.
	connectionPrefix = "c_"
	transferPrefix   = "t_"
)

// Reason is how a connection ended (docs/SPEC.md §9.11).
type Reason string

// The reasons.
const (
	// ReasonClient: the client ended it, or the network did.
	ReasonClient Reason = "client"
	// ReasonDisconnected: the user disconnected it.
	ReasonDisconnected Reason = "disconnected"
	// ReasonPersonChanged: the person was renamed, disabled or removed, or the key removed.
	ReasonPersonChanged Reason = "personChanged"
	// ReasonKeepAlive: the client stopped answering.
	ReasonKeepAlive Reason = "keepalive"
	// ReasonServerStopped: the server stopped, or Cave closed.
	ReasonServerStopped Reason = "serverStopped"
	// ReasonUnknown: Cave ended without writing the end.
	ReasonUnknown Reason = "unknown"
)

// valid says whether r is one of the reasons.
func (r Reason) valid() bool {
	switch r {
	case ReasonClient, ReasonDisconnected, ReasonPersonChanged, ReasonKeepAlive, ReasonServerStopped, ReasonUnknown:
		return true
	}
	return false
}

// Result is how a transfer ended (docs/SPEC.md §9.11).
type Result string

// The results.
const (
	// ResultDone: the client closed the file.
	ResultDone Result = "done"
	// ResultFailed: a read or a write failed.
	ResultFailed Result = "failed"
	// ResultInterrupted: the session or the connection ended while the file was open.
	ResultInterrupted Result = "interrupted"
)

// valid says whether r is one of the results.
func (r Result) valid() bool {
	switch r {
	case ResultDone, ResultFailed, ResultInterrupted:
		return true
	}
	return false
}

// Direction is which way a transfer moved its bytes.
type Direction int

// The directions.
const (
	// Upload: the client only wrote, or created or emptied the file.
	Upload Direction = iota
	// Download: the client only read.
	Download
	// Both: the client read and wrote.
	Both
)

// Activity is one thing a connection opened: its kind ("shell", "exec", "sftp", ...) and which
// one (a command, a destination).
type Activity struct {
	Kind   string `json:"kind"`
	Detail string `json:"detail,omitempty"`
}

// Connection is the record of an authenticated connection. Its texts are safe to show.
type Connection struct {
	// ID is the connection's id in the registry.
	ID string
	// PersonID and PersonName say who it was; the name is a copy.
	PersonID, PersonName string
	// KeyFingerprint is the key it was authenticated with.
	KeyFingerprint string
	// Address is the client's IP address, without the port.
	Address string
	// ClientVersion is the client's version string.
	ClientVersion string
	// ConnectedAt is when it was authenticated.
	ConnectedAt time.Time
	// EndedAt is when it ended; the zero time while it is open and for ReasonUnknown.
	EndedAt time.Time
	// Reason is how it ended; "" while it is open.
	Reason Reason
	// BytesIn and BytesOut are the bytes that arrived from the client and were sent to it.
	BytesIn, BytesOut int64
	// Activities are what it opened, each once, in the order of first use.
	Activities []Activity
	// MoreActivities counts the activities past MaxActivities.
	MoreActivities int
}

// Open says whether the connection has not ended.
func (c Connection) Open() bool {
	return c.Reason == ""
}

// Duration returns how long the connection lasted, and false while it is open and when its end
// is not known.
func (c Connection) Duration() (time.Duration, bool) {
	if c.Open() || c.EndedAt.IsZero() {
		return 0, false
	}
	return c.EndedAt.Sub(c.ConnectedAt), true
}

// clone returns a copy that shares nothing that can be changed.
func (c Connection) clone() Connection {
	c.Activities = append([]Activity(nil), c.Activities...)
	return c
}

// Transfer is the record of one file an SFTP session read or wrote. Its texts are safe to show.
type Transfer struct {
	// ID is the transfer's id; AddTransfer gives it.
	ID string
	// ConnectionID, PersonID and PersonName say whose session it was.
	ConnectionID, PersonID, PersonName string
	// Path is the file, absolute.
	Path string
	// OpenedAt and ClosedAt are when the file was opened and closed.
	OpenedAt, ClosedAt time.Time
	// Downloaded and Uploaded are the bytes the client read and wrote.
	Downloaded, Uploaded int64
	// Created says that the file was opened to be created or emptied.
	Created bool
	// Result is how it ended.
	Result Result
}

// Direction says which way the transfer went (docs/SPEC.md §9.11).
func (t Transfer) Direction() Direction {
	switch {
	case t.Downloaded > 0 && t.Uploaded > 0:
		return Both
	case t.Downloaded > 0:
		return Download
	}
	return Upload
}

// Options are what a Store needs. Every field is required.
type Options struct {
	// Dirs are Cave's folders; the file is in the state folder.
	Dirs storage.Dirs
	// Clock tells the time of a clear.
	Clock clock.Clock
	// Logger gets the errors of writes, which have nobody to return to, and the count of lines
	// that were skipped.
	Logger *slog.Logger
}

// Store is the history. It is safe for concurrent use.
type Store struct {
	path   string
	clock  clock.Clock
	logger *slog.Logger
	// maxBytes is MaxFileBytes; tests lower it.
	maxBytes int64

	mu sync.Mutex
	// connections are in the order they were recorded, and byID finds them.
	connections []*Connection
	byID        map[string]*Connection
	// transfers are in the order they were recorded.
	transfers     []Transfer
	lastClearedAt time.Time
	nextClearAt   time.Time
	// out is the file the lines are appended to.
	out appender
	// closed says that Close was called: nothing is written any more.
	closed bool

	listenersMu sync.Mutex
	listeners   []subscription
	nextID      int
}

// subscription is one listener of a Store.
type subscription struct {
	id int
	fn func()
}

// Open reads the history of the state folder: the file put aside, then the file. A missing file
// is an empty history; a line that cannot be is skipped (docs/SPEC.md §9.11). A file that exists
// and cannot be read is HistoryReadFailed.
func Open(ctx context.Context, opts Options) (*Store, error) {
	path := filepath.Join(opts.Dirs.State, FileName)
	if err := ctx.Err(); err != nil {
		return nil, errors.NewErr(TagOpenCancelled, err).SetMetadata("path", path)
	}
	s := &Store{
		path: path, clock: opts.Clock, logger: opts.Logger, maxBytes: MaxFileBytes,
		byID: map[string]*Connection{},
	}
	s.out = appender{path: path, max: &s.maxBytes, header: s.stateLine}
	r := reading{store: s}
	for _, p := range []string{path + ".1", path} {
		if err := r.file(p); err != nil {
			return nil, err
		}
	}
	r.finish()
	if r.skipped > 0 {
		s.logger.Warn("history lines skipped", "path", path, "count", r.skipped)
	}
	return s, nil
}

// now returns the clock's time in UTC.
func (s *Store) now() time.Time {
	return s.clock.Now().UTC()
}

// cleanConnection makes the texts of a connection safe to show and bounds its activities.
func cleanConnection(c Connection) Connection {
	c.ID = sanitize.Display(c.ID, maxTextLength)
	c.PersonID = sanitize.Display(c.PersonID, maxNameLength)
	c.PersonName = sanitize.Display(c.PersonName, maxNameLength)
	c.KeyFingerprint = sanitize.Display(c.KeyFingerprint, maxFingerprintLength)
	c.Address = sanitize.Display(c.Address, maxTextLength)
	c.ClientVersion = sanitize.Display(c.ClientVersion, maxTextLength)
	c.ConnectedAt, c.EndedAt = c.ConnectedAt.UTC(), c.EndedAt.UTC()
	c.Activities, c.MoreActivities = cleanActivities(c.Activities, c.MoreActivities)
	return c
}

// cleanActivities makes the activities safe to show, keeps each once and at most MaxActivities,
// and counts the others into more.
func cleanActivities(in []Activity, more int) ([]Activity, int) {
	if more < 0 {
		more = 0
	}
	var out []Activity
	seen := map[Activity]bool{}
	for _, a := range in {
		a = Activity{Kind: sanitize.Display(a.Kind, maxKindLength), Detail: sanitize.Display(a.Detail, maxDetailLength)}
		if seen[a] {
			continue
		}
		seen[a] = true
		if len(out) >= MaxActivities {
			more++
			continue
		}
		out = append(out, a)
	}
	return out, more
}

// cleanTransfer makes the texts of a transfer safe to show.
func cleanTransfer(t Transfer) Transfer {
	t.ID = sanitize.Display(t.ID, maxTextLength)
	t.ConnectionID = sanitize.Display(t.ConnectionID, maxTextLength)
	t.PersonID = sanitize.Display(t.PersonID, maxNameLength)
	t.PersonName = sanitize.Display(t.PersonName, maxNameLength)
	t.Path = sanitize.Display(t.Path, MaxPathLength)
	t.OpenedAt, t.ClosedAt = t.OpenedAt.UTC(), t.ClosedAt.UTC()
	if t.Downloaded < 0 {
		t.Downloaded = 0
	}
	if t.Uploaded < 0 {
		t.Uploaded = 0
	}
	return t
}

// addConnection keeps a connection in memory, dropping the oldest past the limit. The caller
// holds the lock, or is Open.
func (s *Store) addConnection(c *Connection) {
	s.connections = append(s.connections, c)
	s.byID[c.ID] = c
	for len(s.connections) > MaxConnections {
		delete(s.byID, s.connections[0].ID)
		s.connections[0] = nil
		s.connections = s.connections[1:]
	}
}

// addTransfer keeps a transfer in memory, dropping the oldest past the limit. The caller holds
// the lock, or is Open.
func (s *Store) addTransfer(t Transfer) {
	s.transfers = append(s.transfers, t)
	if extra := len(s.transfers) - MaxTransfers; extra > 0 {
		s.transfers = append([]Transfer(nil), s.transfers[extra:]...)
	}
}

// Connected records a connection that was authenticated. Its texts are made safe here.
func (s *Store) Connected(c Connection) {
	c = cleanConnection(c)
	c.EndedAt, c.Reason, c.BytesIn, c.BytesOut, c.Activities, c.MoreActivities = time.Time{}, "", 0, 0, nil, 0
	s.mu.Lock()
	if s.closed {
		s.mu.Unlock()
		return
	}
	if _, ok := s.byID[c.ID]; ok {
		s.mu.Unlock()
		return // recorded already
	}
	kept := c
	s.addConnection(&kept)
	s.write(connectedLine(kept))
	s.mu.Unlock()
	s.notify()
}

// Ended records the end of a connection that Connected recorded. A connection the history does
// not have (it was cleared, or dropped past the limit) is not written.
func (s *Store) Ended(id string, endedAt time.Time, reason Reason, bytesIn, bytesOut int64, activities []Activity, more int) {
	activities, more = cleanActivities(activities, more)
	if !reason.valid() || reason == ReasonUnknown {
		reason = ReasonClient
	}
	s.mu.Lock()
	c, ok := s.byID[id]
	if s.closed || !ok || !c.Open() {
		s.mu.Unlock()
		return
	}
	c.EndedAt, c.Reason, c.BytesIn, c.BytesOut, c.Activities, c.MoreActivities = endedAt.UTC(), reason, bytesIn, bytesOut, activities, more
	s.write(endedLine(*c))
	s.mu.Unlock()
	s.notify()
}

// AddTransfer records a transfer, gives it its id, and returns it as it is kept. Its texts are
// made safe here.
func (s *Store) AddTransfer(t Transfer) Transfer {
	t.ID = ids.New(transferPrefix)
	t = cleanTransfer(t)
	if !t.Result.valid() {
		t.Result = ResultDone
	}
	s.mu.Lock()
	if s.closed {
		s.mu.Unlock()
		return t
	}
	s.addTransfer(t)
	s.write(transferLine(t))
	s.mu.Unlock()
	s.notify()
	return t
}

// Connections returns the connections, the newest first. The result is the caller's own copy.
func (s *Store) Connections() []Connection {
	s.mu.Lock()
	out := make([]Connection, 0, len(s.connections))
	for _, c := range slices.Backward(s.connections) {
		out = append(out, c.clone())
	}
	s.mu.Unlock()
	sort.SliceStable(out, func(i, j int) bool { return out[i].ConnectedAt.After(out[j].ConnectedAt) })
	return out
}

// Transfers returns the transfers, the newest first. The result is the caller's own copy.
func (s *Store) Transfers() []Transfer {
	s.mu.Lock()
	out := make([]Transfer, 0, len(s.transfers))
	for _, t := range slices.Backward(s.transfers) {
		out = append(out, t)
	}
	s.mu.Unlock()
	sort.SliceStable(out, func(i, j int) bool { return out[i].ClosedAt.After(out[j].ClosedAt) })
	return out
}

// LastClearedAt returns when the history was last emptied; the zero time when it never was.
func (s *Store) LastClearedAt() time.Time {
	s.mu.Lock()
	defer s.mu.Unlock()
	return s.lastClearedAt
}

// NextClearAt returns when a periodic policy will empty the history next; the zero time when no
// periodic policy is in force.
func (s *Store) NextClearAt() time.Time {
	s.mu.Lock()
	defer s.mu.Unlock()
	return s.nextClearAt
}

// Clear empties the history by hand: both files are removed and a new one starts; the
// connections that are open stay. A file that cannot be removed is HistoryClearFailed, and the
// history is left as it was.
func (s *Store) Clear(ctx context.Context) error {
	if err := ctx.Err(); err != nil {
		return errors.NewErr(TagClearCancel, err).SetMetadata("path", s.path)
	}
	s.mu.Lock()
	err := s.clearLocked()
	s.mu.Unlock()
	if err != nil {
		return err
	}
	s.notify()
	return nil
}

// clearLocked empties the history. The caller holds the lock.
func (s *Store) clearLocked() error {
	if s.closed {
		return nil
	}
	s.closeOut()
	for _, p := range []string{s.path + ".1", s.path} {
		if err := os.Remove(p); err != nil && !os.IsNotExist(err) {
			return errors.NewErr(TagClearFailed, err).SetMetadata("path", p)
		}
	}
	var open []*Connection
	for _, c := range s.connections {
		if c.Open() {
			open = append(open, c)
		}
	}
	s.connections = nil
	s.byID = map[string]*Connection{}
	s.transfers = nil
	s.lastClearedAt = s.now()
	// The new file starts with the state line; the open connections' starts follow, so that
	// their ends find them when the file is read again.
	s.write(nil)
	for _, c := range open {
		s.addConnection(c)
		s.write(connectedLine(*c))
	}
	return nil
}

// ApplyPolicy empties the history when the policy says it is time, and reports whether it did;
// it means what attempts.List.ApplyPolicy means (docs/SPEC.md §8.4, §9.11).
func (s *Store) ApplyPolicy(policy attempts.Policy, atStart bool) bool {
	s.mu.Lock()
	cleared, changed := false, false
	var err error
	now := s.now()
	if period, periodic := policy.Period(); periodic {
		switch {
		case s.nextClearAt.IsZero():
			s.nextClearAt = now.Add(period)
			changed = true
		case !now.Before(s.nextClearAt):
			s.nextClearAt = now.Add(period)
			err = s.clearLocked()
			cleared = err == nil
		}
	} else {
		if !s.nextClearAt.IsZero() {
			s.nextClearAt = time.Time{}
			changed = true
		}
		if policy == attempts.OnStart && atStart {
			err = s.clearLocked()
			cleared = err == nil
		}
	}
	if changed && !cleared {
		s.write(nil)
	}
	s.mu.Unlock()
	if err != nil {
		s.logger.Error("the history could not be cleared", "err", err)
	}
	if cleared || changed {
		s.notify()
	}
	return cleared
}

// PolicyChanged is called when the setting changes: a periodic policy starts its period now, and
// any other has no next time.
func (s *Store) PolicyChanged(policy attempts.Policy) {
	s.mu.Lock()
	if period, periodic := policy.Period(); periodic {
		s.nextClearAt = s.now().Add(period)
	} else {
		s.nextClearAt = time.Time{}
	}
	s.write(nil)
	s.mu.Unlock()
	s.notify()
}

// Close closes the file. Nothing is written afterwards. A second call does nothing.
func (s *Store) Close() error {
	s.mu.Lock()
	defer s.mu.Unlock()
	if s.closed {
		return nil
	}
	s.closed = true
	if err := s.out.close(); err != nil {
		return err
	}
	return nil
}

// closeOut closes the file before it is removed; a failure loses nothing, because every line was
// written already. The caller holds the lock.
func (s *Store) closeOut() {
	if err := s.out.close(); err != nil {
		s.logger.Warn("the history file could not be closed", "err", err)
	}
}

// write appends a line, or the state line when l is nil, and logs a failure: the record stays in
// memory and the next line tries again. The caller holds the lock.
func (s *Store) write(l *line) {
	if s.closed {
		return
	}
	if l == nil {
		l = s.stateLine()
	}
	if err := s.out.append(l); err != nil {
		s.logger.Error("the history could not be written", "err", err)
	}
}

// stateLine is the line with the clearing's times. The caller holds the lock.
func (s *Store) stateLine() *line {
	return &line{V: lineVersion, Kind: kindState, LastClearedAt: timePtr(s.lastClearedAt), NextClearAt: timePtr(s.nextClearAt)}
}

// Subscribe registers a function that is called after each change of the history, on the
// goroutine that made the change. The returned function ends the subscription.
func (s *Store) Subscribe(listener func()) (cancel func()) {
	s.listenersMu.Lock()
	defer s.listenersMu.Unlock()
	id := s.nextID
	s.nextID++
	s.listeners = append(s.listeners, subscription{id: id, fn: listener})
	return func() {
		s.listenersMu.Lock()
		defer s.listenersMu.Unlock()
		for i, sub := range s.listeners {
			if sub.id == id {
				// A new slice, so that a notify that is under way keeps its own list.
				s.listeners = append(append([]subscription(nil), s.listeners[:i]...), s.listeners[i+1:]...)
				return
			}
		}
	}
}

// notify calls the listeners, in the order they subscribed, outside the locks.
func (s *Store) notify() {
	s.listenersMu.Lock()
	listeners := s.listeners
	s.listenersMu.Unlock()
	for _, sub := range listeners {
		sub.fn()
	}
}
