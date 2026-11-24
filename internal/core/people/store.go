package people

import (
	"context"
	"encoding/json"
	"path/filepath"
	"sort"
	"strings"
	"sync"
	"sync/atomic"
	"time"

	"github.com/rm4n0s/cave/internal/core/clock"
	"github.com/rm4n0s/cave/internal/core/ids"
	"github.com/rm4n0s/cave/internal/core/storage"
	"github.com/rm4n0s/errors"
	"golang.org/x/crypto/ssh"
)

// The Tags of the store's errors.
const (
	TagPeopleParseFailed        = "PeopleParseFailed"
	TagPeopleVersionUnsupported = "PeopleVersionUnsupported"
	TagPeopleFileInvalid        = "PeopleFileInvalid"
	TagPeopleEncodeFailed       = "PeopleEncodeFailed"
	TagPersonNameInvalid        = "PersonNameInvalid"
	TagPersonNameTaken          = "PersonNameTaken"
	TagPersonNotFound           = "PersonNotFound"
	TagKeyNotFound              = "KeyNotFound"
	TagKeyAlreadyAssigned       = "KeyAlreadyAssigned"
)

// FileName is the name of the people file in the configuration folder.
const FileName = "people.json"

// Limits and names of the file.
const (
	// fileVersion is the version of the file's format that this code reads and writes.
	fileVersion = 1
	// maxFileBytes bounds the people file.
	maxFileBytes = 16 << 20
	// MaxImportBytes bounds a file of keys that is imported.
	MaxImportBytes = 1 << 20
	// MaxNameLength is how many characters a person's name may have.
	MaxNameLength = 32
	// personPrefix and keyPrefix start the ids of people and keys.
	personPrefix = "p_"
	keyPrefix    = "k_"
)

// Key is a public key of a person.
type Key struct {
	// ID never changes.
	ID string
	// Type is the key's type, such as "ssh-ed25519".
	Type string
	// Fingerprint is OpenSSH's "SHA256:..." fingerprint.
	Fingerprint string
	// Comment is display text.
	Comment string
	// AddedAt is when the key was added.
	AddedAt time.Time
	// Public is the key itself.
	Public ssh.PublicKey
}

// AuthorizedKey returns the key as "<type> <base64>".
func (k Key) AuthorizedKey() string {
	return authorizedKey(k.Public)
}

// Person is somebody who may connect.
type Person struct {
	// ID never changes.
	ID string
	// Name is the login name.
	Name string
	// Enabled says whether the person may connect.
	Enabled bool
	// CreatedAt is when the person was added.
	CreatedAt time.Time
	// Keys are the person's public keys, in the order they were added.
	Keys []Key
}

// clone returns a copy that shares nothing that can be changed.
func (p Person) clone() Person {
	p.Keys = append([]Key(nil), p.Keys...)
	return p
}

// ChangeKind says what changed in the store.
type ChangeKind int

// The kinds of changes.
const (
	// PersonAdded: a person was added.
	PersonAdded ChangeKind = iota
	// PersonRenamed: a person's login name changed. Their connections must end.
	PersonRenamed
	// PersonEnabled: a person may connect again.
	PersonEnabled
	// PersonDisabled: a person may no longer connect. Their connections must end.
	PersonDisabled
	// PersonRemoved: a person was removed. Their connections must end.
	PersonRemoved
	// KeysAdded: keys were added to a person.
	KeysAdded
	// KeyCommentChanged: a key's comment changed.
	KeyCommentChanged
	// KeyRemoved: a key was removed. The connections authenticated with it must end.
	KeyRemoved
)

// Change is one change of the store, as its listeners get it.
type Change struct {
	// Kind says what changed.
	Kind ChangeKind
	// PersonID is the person it happened to.
	PersonID string
	// KeyIDs are the keys it happened to (KeysAdded, KeyCommentChanged, KeyRemoved; for
	// PersonRemoved, the keys the person had).
	KeyIDs []string
}

// Reason is the answer of Lookup (docs/SPEC.md §7.3).
type Reason int

// The answers of Lookup.
const (
	// Accepted: the key is a key of the enabled person with that name.
	Accepted Reason = iota
	// UnknownName: nobody has that name.
	UnknownName
	// Disabled: the person with that name is disabled.
	Disabled
	// WrongKey: the key is not one of the person's.
	WrongKey
)

// Result is what Lookup found.
type Result struct {
	// Reason is the answer.
	Reason Reason
	// PersonID and PersonName are the person with the login name; empty for UnknownName.
	PersonID, PersonName string
	// KeyID is the person's key that matched; only for Accepted.
	KeyID string
	// KeyOwner is the name of another person who has the key, or "".
	KeyOwner string
}

// keyOwner says whose a key is.
type keyOwner struct {
	personID, personName, keyID string
}

// state is the content of the store at one moment. It is never changed after it is made.
type state struct {
	// people are sorted by name, ignoring case.
	people []Person
	// byName finds a person by the exact login name.
	byName map[string]int
	// byKey finds the owner of a key by its wire encoding.
	byKey map[string]keyOwner
}

// newState indexes a list of people. The list becomes the state's own.
func newState(list []Person) *state {
	sort.SliceStable(list, func(i, j int) bool {
		a, b := strings.ToLower(list[i].Name), strings.ToLower(list[j].Name)
		if a != b {
			return a < b
		}
		return list[i].Name < list[j].Name
	})
	st := &state{people: list, byName: make(map[string]int, len(list)), byKey: map[string]keyOwner{}}
	for i, p := range list {
		st.byName[p.Name] = i
		for _, k := range p.Keys {
			st.byKey[string(k.Public.Marshal())] = keyOwner{personID: p.ID, personName: p.Name, keyID: k.ID}
		}
	}
	return st
}

// copyPeople returns a deep copy of the state's people, for a writer to change.
func (st *state) copyPeople() []Person {
	list := make([]Person, len(st.people))
	for i, p := range st.people {
		list[i] = p.clone()
	}
	return list
}

// Store holds the people and their file. It is safe for concurrent use; Lookup never waits for
// a writer.
type Store struct {
	path  string
	clock clock.Clock

	// write serializes the changes, so that the file and the state change together.
	write   sync.Mutex
	current atomic.Pointer[state]

	listenersMu sync.Mutex
	listeners   []subscription
	nextID      int
}

// subscription is one listener of a Store.
type subscription struct {
	id int
	fn func(Change)
}

// fileKey and filePerson and fileRoot are the people file as JSON.
type fileKey struct {
	ID      string    `json:"id"`
	Key     string    `json:"key"`
	Comment string    `json:"comment"`
	AddedAt time.Time `json:"addedAt"`
}

type filePerson struct {
	ID        string    `json:"id"`
	Name      string    `json:"name"`
	Enabled   bool      `json:"enabled"`
	CreatedAt time.Time `json:"createdAt"`
	Keys      []fileKey `json:"keys"`
}

type fileRoot struct {
	Version int          `json:"version"`
	People  []filePerson `json:"people"`
}

// Open reads the people file of the configuration folder. A missing file means nobody. A file
// that does not parse, has another version, or breaks a rule of docs/SPEC.md §7 is an error, and
// the file is left as it is: Cave never starts with nobody over a broken file.
func Open(ctx context.Context, dirs storage.Dirs, clk clock.Clock) (*Store, error) {
	s := &Store{path: filepath.Join(dirs.Config, FileName), clock: clk}
	data, err := storage.ReadFile(ctx, s.path, maxFileBytes)
	if errors.Is(err, storage.ErrNotExist) {
		s.current.Store(newState(nil))
		return s, nil
	}
	if err != nil {
		return nil, err
	}
	var root fileRoot
	if err := json.Unmarshal(data, &root); err != nil {
		return nil, errors.NewErr(TagPeopleParseFailed, err).SetMetadata("path", s.path)
	}
	if root.Version != fileVersion {
		return nil, errors.New(TagPeopleVersionUnsupported, "the people file has a version this Cave does not read",
			"path", s.path, "version", root.Version, "supported", fileVersion)
	}
	list, reason := fromFile(root.People)
	if reason != "" {
		return nil, errors.New(TagPeopleFileInvalid, "the people file breaks a rule: "+reason, "path", s.path, "reason", reason)
	}
	s.current.Store(newState(list))
	return s, nil
}

// fromFile turns the people of a file into people, checking every rule that the store keeps
// when it changes them. It returns why the file is not acceptable, or "".
func fromFile(in []filePerson) ([]Person, string) {
	list := make([]Person, 0, len(in))
	names := map[string]bool{}
	personIDs := map[string]bool{}
	keyIDs := map[string]bool{}
	keyBlobs := map[string]bool{}
	for _, fp := range in {
		if !ids.Valid(personPrefix, fp.ID) || personIDs[fp.ID] {
			return nil, "a person has a missing, malformed or repeated id"
		}
		personIDs[fp.ID] = true
		if nameProblem(fp.Name) != "" {
			return nil, "the person " + fp.ID + " has a name that is not acceptable"
		}
		if names[strings.ToLower(fp.Name)] {
			return nil, "the name " + fp.Name + " is used twice"
		}
		names[strings.ToLower(fp.Name)] = true
		p := Person{ID: fp.ID, Name: fp.Name, Enabled: fp.Enabled, CreatedAt: fp.CreatedAt}
		for _, fk := range fp.Keys {
			if !ids.Valid(keyPrefix, fk.ID) || keyIDs[fk.ID] {
				return nil, "a key of " + fp.Name + " has a missing, malformed or repeated id"
			}
			keyIDs[fk.ID] = true
			if strings.Contains(fk.Key, "\n") {
				return nil, "the key " + fk.ID + " of " + fp.Name + " is more than one line"
			}
			public, _, problem := parseLine(strings.TrimSpace(fk.Key))
			if problem != lineOK {
				return nil, "the key " + fk.ID + " of " + fp.Name + " is not an acceptable public key"
			}
			blob := string(public.Public.Marshal())
			if keyBlobs[blob] {
				return nil, "the key " + fk.ID + " of " + fp.Name + " is in the file twice"
			}
			keyBlobs[blob] = true
			p.Keys = append(p.Keys, newKey(fk.ID, public.Public, cleanComment(fk.Comment), fk.AddedAt))
		}
		list = append(list, p)
	}
	return list, ""
}

// newKey makes a Key from its parts.
func newKey(id string, public ssh.PublicKey, comment string, addedAt time.Time) Key {
	return Key{ID: id, Type: public.Type(), Fingerprint: ssh.FingerprintSHA256(public), Comment: comment, AddedAt: addedAt, Public: public}
}

// nameProblem says why a login name is not acceptable, or "" (docs/SPEC.md §7.1).
func nameProblem(name string) string {
	if name == "" {
		return "a name is needed"
	}
	if len(name) > MaxNameLength {
		return "a name has at most 32 characters"
	}
	if name[0] == '-' || name[0] == '.' {
		return "a name does not start with - or ."
	}
	for _, r := range name {
		letter := (r >= 'A' && r <= 'Z') || (r >= 'a' && r <= 'z')
		digit := r >= '0' && r <= '9'
		if !letter && !digit && r != '.' && r != '_' && r != '-' {
			return "a name has only the letters A to Z and a to z, digits, and . _ -"
		}
	}
	return ""
}

// checkName refuses a login name that is not acceptable, or that a person other than exceptID
// has, ignoring case.
func checkName(st *state, name, exceptID string) error {
	if problem := nameProblem(name); problem != "" {
		return errors.New(TagPersonNameInvalid, problem, "name", cleanComment(name))
	}
	for _, p := range st.people {
		if p.ID != exceptID && strings.EqualFold(p.Name, name) {
			return errors.New(TagPersonNameTaken, "another person has this name", "name", name, "takenBy", p.Name)
		}
	}
	return nil
}

// findPerson returns the index of the person with the id in the list.
func findPerson(list []Person, id string) (int, error) {
	for i, p := range list {
		if p.ID == id {
			return i, nil
		}
	}
	return 0, errors.New(TagPersonNotFound, "there is no such person", "personID", id)
}

// findKey returns the index of the key with the id among a person's keys.
func findKey(p Person, keyID string) (int, error) {
	for i, k := range p.Keys {
		if k.ID == keyID {
			return i, nil
		}
	}
	return 0, errors.New(TagKeyNotFound, "the person has no such key", "personID", p.ID, "keyID", keyID)
}

// now returns the time for a new record: the clock's, in UTC, to the second.
func (s *Store) now() time.Time {
	return s.clock.Now().UTC().Truncate(time.Second)
}

// commit writes a new list of people to the file and, when that worked, makes it the store's
// state. The caller holds the write lock.
func (s *Store) commit(ctx context.Context, list []Person) error {
	st := newState(list)
	root := fileRoot{Version: fileVersion, People: make([]filePerson, 0, len(st.people))}
	for _, p := range st.people {
		fp := filePerson{ID: p.ID, Name: p.Name, Enabled: p.Enabled, CreatedAt: p.CreatedAt, Keys: make([]fileKey, 0, len(p.Keys))}
		for _, k := range p.Keys {
			fp.Keys = append(fp.Keys, fileKey{ID: k.ID, Key: k.AuthorizedKey(), Comment: k.Comment, AddedAt: k.AddedAt})
		}
		root.People = append(root.People, fp)
	}
	data, err := json.MarshalIndent(root, "", "  ")
	if err != nil {
		return errors.NewErr(TagPeopleEncodeFailed, err).SetMetadata("path", s.path)
	}
	if err := storage.WriteFileAtomic(ctx, s.path, append(data, '\n')); err != nil {
		return err
	}
	s.current.Store(st)
	return nil
}

// List returns everybody, sorted by name. The result is the caller's own copy.
func (s *Store) List() []Person {
	return s.current.Load().copyPeople()
}

// Get returns the person with the id, and whether there is one.
func (s *Store) Get(id string) (Person, bool) {
	for _, p := range s.current.Load().people {
		if p.ID == id {
			return p.clone(), true
		}
	}
	return Person{}, false
}

// ByName returns the person with exactly this login name, and whether there is one.
func (s *Store) ByName(name string) (Person, bool) {
	st := s.current.Load()
	i, ok := st.byName[name]
	if !ok {
		return Person{}, false
	}
	return st.people[i].clone(), true
}

// Add adds an enabled person without keys.
func (s *Store) Add(ctx context.Context, name string) (Person, error) {
	s.write.Lock()
	st := s.current.Load()
	if err := checkName(st, name, ""); err != nil {
		s.write.Unlock()
		return Person{}, err
	}
	p := Person{ID: ids.New(personPrefix), Name: name, Enabled: true, CreatedAt: s.now()}
	if err := s.commit(ctx, append(st.copyPeople(), p)); err != nil {
		s.write.Unlock()
		return Person{}, err
	}
	s.write.Unlock()
	s.notify(Change{Kind: PersonAdded, PersonID: p.ID})
	return p.clone(), nil
}

// Rename changes a person's login name. Renaming to the name the person has does nothing.
func (s *Store) Rename(ctx context.Context, id, name string) error {
	s.write.Lock()
	st := s.current.Load()
	list := st.copyPeople()
	i, err := findPerson(list, id)
	if err != nil {
		s.write.Unlock()
		return err
	}
	if err := checkName(st, name, id); err != nil {
		s.write.Unlock()
		return err
	}
	if list[i].Name == name {
		s.write.Unlock()
		return nil
	}
	list[i].Name = name
	if err := s.commit(ctx, list); err != nil {
		s.write.Unlock()
		return err
	}
	s.write.Unlock()
	s.notify(Change{Kind: PersonRenamed, PersonID: id})
	return nil
}

// SetEnabled lets a person connect, or not. Setting what the person already is does nothing.
func (s *Store) SetEnabled(ctx context.Context, id string, enabled bool) error {
	s.write.Lock()
	list := s.current.Load().copyPeople()
	i, err := findPerson(list, id)
	if err != nil {
		s.write.Unlock()
		return err
	}
	if list[i].Enabled == enabled {
		s.write.Unlock()
		return nil
	}
	list[i].Enabled = enabled
	if err := s.commit(ctx, list); err != nil {
		s.write.Unlock()
		return err
	}
	s.write.Unlock()
	kind := PersonDisabled
	if enabled {
		kind = PersonEnabled
	}
	s.notify(Change{Kind: kind, PersonID: id})
	return nil
}

// Remove removes a person and their keys.
func (s *Store) Remove(ctx context.Context, id string) error {
	s.write.Lock()
	list := s.current.Load().copyPeople()
	i, err := findPerson(list, id)
	if err != nil {
		s.write.Unlock()
		return err
	}
	change := Change{Kind: PersonRemoved, PersonID: id}
	for _, k := range list[i].Keys {
		change.KeyIDs = append(change.KeyIDs, k.ID)
	}
	if err := s.commit(ctx, append(list[:i], list[i+1:]...)); err != nil {
		s.write.Unlock()
		return err
	}
	s.write.Unlock()
	s.notify(change)
	return nil
}

// AddKeys adds the public keys of a text in the authorized_keys format to a person, and returns
// them. Either every key of the text is added or none: a line that is not an acceptable key, or
// a key that somebody already has (KeyAlreadyAssigned, with the owner's name and the key's
// fingerprint), refuses the whole text.
func (s *Store) AddKeys(ctx context.Context, personID, text string) ([]Key, error) {
	return s.addKeys(ctx, personID, text)
}

// ImportKeys adds the public keys of a file (a .pub file or an authorized_keys file, of at most
// MaxImportBytes) to a person, by the rules of AddKeys.
func (s *Store) ImportKeys(ctx context.Context, personID, path string) ([]Key, error) {
	data, err := storage.ReadFile(ctx, path, MaxImportBytes)
	if err != nil {
		return nil, err
	}
	return s.addKeys(ctx, personID, string(data))
}

// addKeys is AddKeys.
func (s *Store) addKeys(ctx context.Context, personID, text string) ([]Key, error) {
	parsed, err := ParseKeys(text)
	if err != nil {
		return nil, err
	}
	s.write.Lock()
	st := s.current.Load()
	list := st.copyPeople()
	i, err := findPerson(list, personID)
	if err != nil {
		s.write.Unlock()
		return nil, err
	}
	now := s.now()
	added := make([]Key, 0, len(parsed))
	for _, pk := range parsed {
		if owner, taken := st.byKey[string(pk.Public.Marshal())]; taken {
			s.write.Unlock()
			return nil, errors.New(TagKeyAlreadyAssigned, "this key already belongs to "+owner.personName,
				"owner", owner.personName, "ownerID", owner.personID, "fingerprint", ssh.FingerprintSHA256(pk.Public))
		}
		added = append(added, newKey(ids.New(keyPrefix), pk.Public, pk.Comment, now))
	}
	list[i].Keys = append(list[i].Keys, added...)
	if err := s.commit(ctx, list); err != nil {
		s.write.Unlock()
		return nil, err
	}
	s.write.Unlock()
	change := Change{Kind: KeysAdded, PersonID: personID}
	for _, k := range added {
		change.KeyIDs = append(change.KeyIDs, k.ID)
	}
	s.notify(change)
	return added, nil
}

// SetKeyComment changes the comment of a key. The comment is made safe to show and cut to
// MaxCommentLength characters. Setting the comment the key has does nothing.
func (s *Store) SetKeyComment(ctx context.Context, personID, keyID, comment string) error {
	s.write.Lock()
	list := s.current.Load().copyPeople()
	i, err := findPerson(list, personID)
	if err != nil {
		s.write.Unlock()
		return err
	}
	k, err := findKey(list[i], keyID)
	if err != nil {
		s.write.Unlock()
		return err
	}
	comment = cleanComment(comment)
	if list[i].Keys[k].Comment == comment {
		s.write.Unlock()
		return nil
	}
	list[i].Keys[k].Comment = comment
	if err := s.commit(ctx, list); err != nil {
		s.write.Unlock()
		return err
	}
	s.write.Unlock()
	s.notify(Change{Kind: KeyCommentChanged, PersonID: personID, KeyIDs: []string{keyID}})
	return nil
}

// RemoveKey removes a key from a person.
func (s *Store) RemoveKey(ctx context.Context, personID, keyID string) error {
	s.write.Lock()
	list := s.current.Load().copyPeople()
	i, err := findPerson(list, personID)
	if err != nil {
		s.write.Unlock()
		return err
	}
	k, err := findKey(list[i], keyID)
	if err != nil {
		s.write.Unlock()
		return err
	}
	list[i].Keys = append(list[i].Keys[:k], list[i].Keys[k+1:]...)
	if err := s.commit(ctx, list); err != nil {
		s.write.Unlock()
		return err
	}
	s.write.Unlock()
	s.notify(Change{Kind: KeyRemoved, PersonID: personID, KeyIDs: []string{keyID}})
	return nil
}

// Lookup answers authentication's question for one key that a client offers under a login name
// (docs/SPEC.md §7.3, steps 2 to 5), from the store's state at this moment. It also says when
// the key belongs to somebody else.
func (s *Store) Lookup(name string, key ssh.PublicKey) Result {
	st := s.current.Load()
	owner, owned := st.byKey[string(key.Marshal())]
	i, found := st.byName[name]
	if !found {
		res := Result{Reason: UnknownName}
		if owned {
			res.KeyOwner = owner.personName
		}
		return res
	}
	p := st.people[i]
	res := Result{PersonID: p.ID, PersonName: p.Name}
	if owned && owner.personID != p.ID {
		res.KeyOwner = owner.personName
	}
	switch {
	case !p.Enabled:
		res.Reason = Disabled
	case !owned || owner.personID != p.ID:
		res.Reason = WrongKey
	default:
		res.Reason = Accepted
		res.KeyID = owner.keyID
	}
	return res
}

// Subscribe registers a function that is called after each change, on the goroutine that made
// the change, when the change is in the file and in the store. The returned function ends the
// subscription.
func (s *Store) Subscribe(listener func(Change)) (cancel func()) {
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
func (s *Store) notify(change Change) {
	s.listenersMu.Lock()
	listeners := s.listeners
	s.listenersMu.Unlock()
	for _, sub := range listeners {
		sub.fn(change)
	}
}
