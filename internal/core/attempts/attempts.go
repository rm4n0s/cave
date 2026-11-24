// Package attempts is the list of who tried to connect and how many times (docs/SPEC.md §8).
// The server records one attempt for every connection; the list counts them into entries, keeps
// itself bounded, makes every text from a client safe before it is stored, writes itself to its
// file at most once every few seconds, and empties itself when the setting says.
package attempts

import (
	"context"
	"encoding/json"
	"log/slog"
	"path/filepath"
	"sort"
	"sync"
	"time"

	"github.com/rm4n0s/cave/internal/core/clock"
	"github.com/rm4n0s/cave/internal/core/ids"
	"github.com/rm4n0s/cave/internal/core/sanitize"
	"github.com/rm4n0s/cave/internal/core/storage"
	"github.com/rm4n0s/errors"
)

// The Tags of this package's errors.
const (
	TagParseFailed        = "AttemptsParseFailed"
	TagVersionUnsupported = "AttemptsVersionUnsupported"
	TagFileInvalid        = "AttemptsFileInvalid"
	TagEncodeFailed       = "AttemptsEncodeFailed"
	TagAttemptNotFound    = "AttemptNotFound"
)

// FileName is the name of the attempts file in the state folder.
const FileName = "attempts.json"

// The limits of docs/SPEC.md §8.
const (
	// MaxEntries is how many entries the list keeps; one more drops the entry whose last
	// attempt is the oldest.
	MaxEntries = 5000
	// MaxCount is where an entry's count stops: 2^53, the largest whole number that every JSON
	// reader keeps exact.
	MaxCount = 1 << 53
	// MaxTextLength is how many characters a login name, a client version or an address keeps.
	MaxTextLength = 64
	// MaxKeys is how many key fingerprints an entry keeps.
	MaxKeys = 8
	// SaveDelay is how long after a change the file is written, so that a flood of attempts
	// costs one write every SaveDelay.
	SaveDelay = 5 * time.Second

	// maxNameLength bounds a person's name copied into an entry.
	maxNameLength = 32
	// maxFingerprintLength bounds one key fingerprint.
	maxFingerprintLength = 128
	// fileVersion is the version of the file's format that this code reads and writes.
	fileVersion = 1
	// maxFileBytes bounds the attempts file.
	maxFileBytes = 16 << 20
	// entryPrefix starts the ids of entries.
	entryPrefix = "a_"
)

// Outcome is how a connection ended, as far as authentication goes (docs/SPEC.md §8.1).
type Outcome string

// The outcomes.
const (
	// Accepted: authenticated as a person.
	Accepted Outcome = "accepted"
	// UnknownName: the login name is nobody's.
	UnknownName Outcome = "unknownName"
	// Disabled: the person is disabled.
	Disabled Outcome = "disabled"
	// WrongKey: the name is a person's; none of the keys offered is theirs.
	WrongKey Outcome = "wrongKey"
	// NoKey: the client reached authentication and offered no public key.
	NoKey Outcome = "noKey"
	// NotProven: the client offered a key that is the person's and did not prove it.
	NotProven Outcome = "notProven"
	// HandshakeFailed: the connection ended before authentication started.
	HandshakeFailed Outcome = "handshakeFailed"
	// Refused: Cave closed the connection at once because of its limits.
	Refused Outcome = "refused"
)

// valid says whether o is one of the outcomes.
func (o Outcome) valid() bool {
	switch o {
	case Accepted, UnknownName, Disabled, WrongKey, NoKey, NotProven, HandshakeFailed, Refused:
		return true
	}
	return false
}

// Policy says when the list is emptied; its values are those of the setting attempts.clear
// (docs/SPEC.md §8.4).
type Policy string

// The policies.
const (
	// Never: only by hand.
	Never Policy = "never"
	// OnStart: each time Cave starts.
	OnStart Policy = "onStart"
	// Hourly, Daily, Weekly and Monthly: every 1 hour, 24 hours, 7 days and 30 days.
	Hourly  Policy = "hourly"
	Daily   Policy = "daily"
	Weekly  Policy = "weekly"
	Monthly Policy = "monthly"
)

// Period returns the interval of a periodic policy, and whether the policy is one.
func (p Policy) Period() (time.Duration, bool) {
	switch p {
	case Hourly:
		return time.Hour, true
	case Daily:
		return 24 * time.Hour, true
	case Weekly:
		return 7 * 24 * time.Hour, true
	case Monthly:
		return 30 * 24 * time.Hour, true
	}
	return 0, false
}

// Attempt is one connection, as the server reports it. Every text may be whatever the client
// sent: Record makes it safe.
type Attempt struct {
	// Outcome is how it ended.
	Outcome Outcome
	// PersonID and PersonName are the person the login name belongs to, when there is one.
	PersonID, PersonName string
	// LoginName is the name the client asked to log in as; empty when it sent none.
	LoginName string
	// Address is the client's IP address, without the port.
	Address string
	// ClientVersion is the client's SSH version string.
	ClientVersion string
	// Keys are the fingerprints of the keys the client offered.
	Keys []string
	// KeyOwner is the name of another person who has one of the keys offered, or "".
	KeyOwner string
}

// Entry is every attempt with the same address, login name, outcome and person, counted
// (docs/SPEC.md §8.2). Its texts are safe to show.
type Entry struct {
	ID            string    `json:"id"`
	Outcome       Outcome   `json:"outcome"`
	PersonID      string    `json:"personId,omitempty"`
	PersonName    string    `json:"personName,omitempty"`
	LoginName     string    `json:"loginName,omitempty"`
	Address       string    `json:"address"`
	Count         int64     `json:"count"`
	FirstAt       time.Time `json:"firstAt"`
	LastAt        time.Time `json:"lastAt"`
	ClientVersion string    `json:"clientVersion,omitempty"`
	Keys          []string  `json:"keys,omitempty"`
	KeyOwner      string    `json:"keyOwner,omitempty"`
}

// clone returns a copy that shares nothing that can be changed.
func (e Entry) clone() Entry {
	e.Keys = append([]string(nil), e.Keys...)
	return e
}

// groupKey is what makes two attempts the same entry.
type groupKey struct {
	address, loginName string
	outcome            Outcome
	personID           string
}

// key returns the entry's group.
func (e *Entry) key() groupKey {
	return groupKey{address: e.Address, loginName: e.LoginName, outcome: e.Outcome, personID: e.PersonID}
}

// file is the attempts file as JSON.
type file struct {
	Version       int        `json:"version"`
	LastClearedAt *time.Time `json:"lastClearedAt,omitempty"`
	NextClearAt   *time.Time `json:"nextClearAt,omitempty"`
	Entries       []Entry    `json:"entries"`
}

// Options are what a List needs.
type Options struct {
	// Dirs are Cave's folders; the file is in the state folder.
	Dirs storage.Dirs
	// Clock tells the time and delays the writes.
	Clock clock.Clock
	// Logger gets the errors of the delayed writes, which have nobody to return to.
	Logger *slog.Logger
}

// List is the attempts list. It is safe for concurrent use.
type List struct {
	path   string
	clock  clock.Clock
	logger *slog.Logger

	mu            sync.Mutex
	entries       map[groupKey]*Entry
	lastClearedAt time.Time
	nextClearAt   time.Time
	// dirty says that the file is behind the list.
	dirty bool
	// timer is the pending delayed write, or nil.
	timer clock.Timer

	// saving serializes the writes of the file.
	saving sync.Mutex

	listenersMu sync.Mutex
	listeners   []subscription
	nextID      int
}

// subscription is one listener of a List.
type subscription struct {
	id int
	fn func()
}

// Open reads the attempts file of the state folder. A missing file means an empty list. A file
// that does not parse, has another version or holds an entry that cannot be is an error, and the
// file is left as it is (docs/SPEC.md §5.2).
func Open(ctx context.Context, opts Options) (*List, error) {
	l := &List{
		path:    filepath.Join(opts.Dirs.State, FileName),
		clock:   opts.Clock,
		logger:  opts.Logger,
		entries: map[groupKey]*Entry{},
	}
	data, err := storage.ReadFile(ctx, l.path, maxFileBytes)
	if errors.Is(err, storage.ErrNotExist) {
		return l, nil
	}
	if err != nil {
		return nil, err
	}
	var f file
	if err := json.Unmarshal(data, &f); err != nil {
		return nil, errors.NewErr(TagParseFailed, err).SetMetadata("path", l.path)
	}
	if f.Version != fileVersion {
		return nil, errors.New(TagVersionUnsupported, "the attempts file has a version this Cave does not read",
			"path", l.path, "version", f.Version, "supported", fileVersion)
	}
	if f.LastClearedAt != nil {
		l.lastClearedAt = *f.LastClearedAt
	}
	if f.NextClearAt != nil {
		l.nextClearAt = *f.NextClearAt
	}
	seen := map[string]bool{}
	for i := range f.Entries {
		e := clean(f.Entries[i])
		if !ids.Valid(entryPrefix, e.ID) || seen[e.ID] || !e.Outcome.valid() || e.Count < 1 || e.LastAt.Before(e.FirstAt) {
			return nil, errors.New(TagFileInvalid, "the attempts file has an entry that cannot be", "path", l.path, "index", i)
		}
		seen[e.ID] = true
		if e.Count > MaxCount {
			e.Count = MaxCount
		}
		if old, ok := l.entries[e.key()]; ok {
			// Two entries of one group, as an older format or an edit by hand may leave: one entry.
			merge(old, &e)
			continue
		}
		l.entries[e.key()] = &e
	}
	for len(l.entries) > MaxEntries {
		l.dropOldest()
	}
	return l, nil
}

// clean makes every text of an entry safe to show and cuts the list of keys.
func clean(e Entry) Entry {
	e.PersonID = sanitize.Display(e.PersonID, maxNameLength)
	e.PersonName = sanitize.Display(e.PersonName, maxNameLength)
	e.LoginName = sanitize.Display(e.LoginName, MaxTextLength)
	e.Address = sanitize.Display(e.Address, MaxTextLength)
	e.ClientVersion = sanitize.Display(e.ClientVersion, MaxTextLength)
	e.KeyOwner = sanitize.Display(e.KeyOwner, maxNameLength)
	if len(e.Keys) > MaxKeys {
		e.Keys = e.Keys[:MaxKeys]
	}
	keys := make([]string, 0, len(e.Keys))
	for _, k := range e.Keys {
		keys = append(keys, sanitize.Display(k, maxFingerprintLength))
	}
	e.Keys = keys
	if len(e.Keys) == 0 {
		e.Keys = nil
	}
	return e
}

// merge adds the attempts of the entry from to the entry into, which is of the same group.
func merge(into, from *Entry) {
	into.Count += from.Count
	if into.Count > MaxCount || into.Count < 0 {
		into.Count = MaxCount
	}
	if from.FirstAt.Before(into.FirstAt) {
		into.FirstAt = from.FirstAt
	}
	if from.LastAt.After(into.LastAt) {
		into.LastAt = from.LastAt
		into.ClientVersion, into.Keys, into.KeyOwner, into.PersonName = from.ClientVersion, from.Keys, from.KeyOwner, from.PersonName
	}
}

// dropOldest removes the entry whose last attempt is the oldest. The caller holds the lock, or
// is Open.
func (l *List) dropOldest() {
	var oldest *Entry
	for _, e := range l.entries {
		if oldest == nil || e.LastAt.Before(oldest.LastAt) || (e.LastAt.Equal(oldest.LastAt) && e.ID < oldest.ID) {
			oldest = e
		}
	}
	if oldest != nil {
		delete(l.entries, oldest.key())
	}
}

// now returns the clock's time in UTC.
func (l *List) now() time.Time {
	return l.clock.Now().UTC()
}

// changed marks the file as behind the list and arranges the delayed write. The caller holds
// the lock.
func (l *List) changed() {
	l.dirty = true
	if l.timer == nil {
		l.timer = l.clock.AfterFunc(SaveDelay, l.delayedSave)
	}
}

// delayedSave is the delayed write. It has nobody to return an error to, so it logs it; the list
// stays behind its file and the next change arranges another write.
func (l *List) delayedSave() {
	l.mu.Lock()
	l.timer = nil
	l.mu.Unlock()
	if err := l.Flush(context.Background()); err != nil {
		l.logger.Error("the attempts list could not be saved", "err", err)
	}
}

// Record counts one attempt: into the entry of its address, login name, outcome and person when
// there is one, else into a new entry, which may push the oldest entry out. It returns the
// entry as it is now.
func (l *List) Record(a Attempt) Entry {
	now := l.now()
	fresh := clean(Entry{
		Outcome: a.Outcome, PersonID: a.PersonID, PersonName: a.PersonName, LoginName: a.LoginName, Address: a.Address,
		Count: 1, FirstAt: now, LastAt: now, ClientVersion: a.ClientVersion, Keys: append([]string(nil), a.Keys...), KeyOwner: a.KeyOwner,
	})
	l.mu.Lock()
	e, ok := l.entries[fresh.key()]
	if ok {
		if e.Count < MaxCount {
			e.Count++
		}
		if now.After(e.LastAt) {
			e.LastAt = now
		}
		e.ClientVersion, e.Keys, e.KeyOwner, e.PersonName = fresh.ClientVersion, fresh.Keys, fresh.KeyOwner, fresh.PersonName
	} else {
		fresh.ID = ids.New(entryPrefix)
		e = &fresh
		l.entries[e.key()] = e
		if len(l.entries) > MaxEntries {
			l.dropOldest()
		}
	}
	out := e.clone()
	l.changed()
	l.mu.Unlock()
	l.notify()
	return out
}

// Entries returns the entries, the one with the newest last attempt first. The result is the
// caller's own copy.
func (l *List) Entries() []Entry {
	l.mu.Lock()
	out := make([]Entry, 0, len(l.entries))
	for _, e := range l.entries {
		out = append(out, e.clone())
	}
	l.mu.Unlock()
	sortEntries(out)
	return out
}

// sortEntries puts the entry with the newest last attempt first; entries of the same time are
// ordered by id, so that the order is stable.
func sortEntries(entries []Entry) {
	sort.Slice(entries, func(i, j int) bool {
		if !entries[i].LastAt.Equal(entries[j].LastAt) {
			return entries[i].LastAt.After(entries[j].LastAt)
		}
		return entries[i].ID < entries[j].ID
	})
}

// Len returns how many entries the list has.
func (l *List) Len() int {
	l.mu.Lock()
	defer l.mu.Unlock()
	return len(l.entries)
}

// Remove deletes one entry.
func (l *List) Remove(id string) error {
	l.mu.Lock()
	for key, e := range l.entries {
		if e.ID == id {
			delete(l.entries, key)
			l.changed()
			l.mu.Unlock()
			l.notify()
			return nil
		}
	}
	l.mu.Unlock()
	return errors.New(TagAttemptNotFound, "there is no such entry in the attempts list", "id", id)
}

// Clear empties the list by hand. The time of the next clear of a periodic policy stays.
func (l *List) Clear() {
	l.mu.Lock()
	l.clearLocked()
	l.mu.Unlock()
	l.notify()
}

// clearLocked empties the list. The caller holds the lock.
func (l *List) clearLocked() {
	l.entries = map[groupKey]*Entry{}
	l.lastClearedAt = l.now()
	l.changed()
}

// LastClearedAt returns when the list was last emptied; the zero time when it never was.
func (l *List) LastClearedAt() time.Time {
	l.mu.Lock()
	defer l.mu.Unlock()
	return l.lastClearedAt
}

// NextClearAt returns when a periodic policy will empty the list next; the zero time when no
// periodic policy is in force.
func (l *List) NextClearAt() time.Time {
	l.mu.Lock()
	defer l.mu.Unlock()
	return l.nextClearAt
}

// ApplyPolicy empties the list when the policy says it is time, and reports whether it did
// (docs/SPEC.md §8.4). atStart is true for the one call made when Cave starts: OnStart clears
// then, and a period whose time passed while Cave was closed clears then too. While Cave runs,
// it is called once a minute.
func (l *List) ApplyPolicy(policy Policy, atStart bool) bool {
	l.mu.Lock()
	cleared := false
	now := l.now()
	if period, periodic := policy.Period(); periodic {
		switch {
		case l.nextClearAt.IsZero():
			// The policy is in force but no time was set (a file from before it): start the period.
			l.nextClearAt = now.Add(period)
			l.changed()
		case !now.Before(l.nextClearAt):
			l.clearLocked()
			l.nextClearAt = now.Add(period)
			cleared = true
		}
	} else {
		if !l.nextClearAt.IsZero() {
			l.nextClearAt = time.Time{}
			l.changed()
		}
		if policy == OnStart && atStart {
			l.clearLocked()
			cleared = true
		}
	}
	l.mu.Unlock()
	if cleared {
		l.notify()
	}
	return cleared
}

// PolicyChanged is called when the setting changes: a periodic policy starts its period now,
// and any other has no next time.
func (l *List) PolicyChanged(policy Policy) {
	l.mu.Lock()
	if period, periodic := policy.Period(); periodic {
		l.nextClearAt = l.now().Add(period)
	} else {
		l.nextClearAt = time.Time{}
	}
	l.changed()
	l.mu.Unlock()
	l.notify()
}

// Flush writes the file now when the list changed since the last write, and cancels the delayed
// write. Cave calls it when it closes.
func (l *List) Flush(ctx context.Context) error {
	l.saving.Lock()
	defer l.saving.Unlock()

	l.mu.Lock()
	if l.timer != nil {
		l.timer.Stop()
		l.timer = nil
	}
	if !l.dirty {
		l.mu.Unlock()
		return nil
	}
	f := file{Version: fileVersion, Entries: make([]Entry, 0, len(l.entries))}
	if !l.lastClearedAt.IsZero() {
		t := l.lastClearedAt
		f.LastClearedAt = &t
	}
	if !l.nextClearAt.IsZero() {
		t := l.nextClearAt
		f.NextClearAt = &t
	}
	for _, e := range l.entries {
		f.Entries = append(f.Entries, e.clone())
	}
	l.dirty = false
	l.mu.Unlock()

	sortEntries(f.Entries)
	data, err := json.Marshal(f)
	if err == nil {
		err = storage.WriteFileAtomic(ctx, l.path, append(data, '\n'))
	} else {
		err = errors.NewErr(TagEncodeFailed, err).SetMetadata("path", l.path)
	}
	if err != nil {
		// The file is still behind: the next change, or the next Flush, tries again.
		l.mu.Lock()
		l.dirty = true
		l.mu.Unlock()
		return err
	}
	return nil
}

// Subscribe registers a function that is called after each change of the list, on the goroutine
// that made the change. The returned function ends the subscription.
func (l *List) Subscribe(listener func()) (cancel func()) {
	l.listenersMu.Lock()
	defer l.listenersMu.Unlock()
	id := l.nextID
	l.nextID++
	l.listeners = append(l.listeners, subscription{id: id, fn: listener})
	return func() {
		l.listenersMu.Lock()
		defer l.listenersMu.Unlock()
		for i, sub := range l.listeners {
			if sub.id == id {
				// A new slice, so that a notify that is under way keeps its own list.
				l.listeners = append(append([]subscription(nil), l.listeners[:i]...), l.listeners[i+1:]...)
				return
			}
		}
	}
}

// notify calls the listeners, in the order they subscribed, outside the locks.
func (l *List) notify() {
	l.listenersMu.Lock()
	listeners := l.listeners
	l.listenersMu.Unlock()
	for _, sub := range listeners {
		sub.fn()
	}
}
