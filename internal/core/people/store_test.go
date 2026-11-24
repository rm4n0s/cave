package people

import (
	"context"
	"crypto/elliptic"
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"reflect"
	"runtime"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/rm4n0s/cave/internal/core/clock"
	"github.com/rm4n0s/cave/internal/core/errtest"
	"github.com/rm4n0s/cave/internal/core/ids"
	"github.com/rm4n0s/cave/internal/core/storage"
	"github.com/rm4n0s/cave/internal/core/storage/storagetest"
	"golang.org/x/crypto/ssh"
)

var ctx = context.Background()

// start is the time of the tests' clock.
var start = time.Date(2026, 11, 24, 10, 0, 0, 0, time.UTC)

// fixture is a store in its own folders, with what it tells its listeners.
type fixture struct {
	*Store
	dirs  storage.Dirs
	clock *clock.Fake
	// mu guards changes while several goroutines change the store.
	mu      sync.Mutex
	changes []Change
}

// newFixture opens an empty store.
func newFixture(t *testing.T) *fixture {
	t.Helper()
	f := &fixture{dirs: storagetest.Isolate(t), clock: clock.NewFake(start)}
	f.reopen(t)
	return f
}

// reopen opens the store again from its file.
func (f *fixture) reopen(t *testing.T) {
	t.Helper()
	s, err := Open(ctx, f.dirs, f.clock)
	if err != nil {
		t.Fatal(err)
	}
	f.Store = s
	f.changes = nil
	s.Subscribe(func(c Change) {
		f.mu.Lock()
		defer f.mu.Unlock()
		f.changes = append(f.changes, c)
	})
}

// add adds a person, failing the test on an error.
func (f *fixture) add(t *testing.T, name string) Person {
	t.Helper()
	p, err := f.Add(ctx, name)
	if err != nil {
		t.Fatalf("Add(%q): %v", name, err)
	}
	return p
}

// addKey adds one key to a person, failing the test on an error.
func (f *fixture) addKey(t *testing.T, personID string, key testKey, comment string) Key {
	t.Helper()
	keys, err := f.AddKeys(ctx, personID, key.line+" "+comment)
	if err != nil {
		t.Fatalf("AddKeys: %v", err)
	}
	if len(keys) != 1 {
		t.Fatalf("AddKeys returned %d keys", len(keys))
	}
	return keys[0]
}

// names returns the names of the store's people, in its order.
func (f *fixture) names() []string {
	var out []string
	for _, p := range f.List() {
		out = append(out, p.Name)
	}
	return out
}

// kinds returns the kinds of the changes the listener got.
func (f *fixture) kinds() []ChangeKind {
	var out []ChangeKind
	for _, c := range f.changes {
		out = append(out, c.Kind)
	}
	return out
}

// fileContent returns the people file, or "" when there is none.
func (f *fixture) fileContent(t *testing.T) string {
	t.Helper()
	data, err := os.ReadFile(filepath.Join(f.dirs.Config, FileName))
	if os.IsNotExist(err) {
		return ""
	}
	if err != nil {
		t.Fatal(err)
	}
	return string(data)
}

func TestAddPerson(t *testing.T) {
	f := newFixture(t)
	if len(f.List()) != 0 {
		t.Fatal("a new store has people")
	}
	f.clock.Advance(1500 * time.Millisecond)
	p := f.add(t, "alice")
	if !ids.Valid("p_", p.ID) || p.Name != "alice" || !p.Enabled || len(p.Keys) != 0 {
		t.Errorf("the new person is %+v", p)
	}
	if want := start.Add(time.Second); !p.CreatedAt.Equal(want) || p.CreatedAt.Location() != time.UTC {
		t.Errorf("CreatedAt = %v, want %v in UTC, to the second", p.CreatedAt, want)
	}
	got, ok := f.Get(p.ID)
	if !ok || !reflect.DeepEqual(got, p) {
		t.Errorf("Get = %+v, %v", got, ok)
	}
	if _, ok := f.Get("p_0000000000000000"); ok {
		t.Error("Get of an unknown id found somebody")
	}
	if byName, ok := f.ByName("alice"); !ok || byName.ID != p.ID {
		t.Errorf("ByName = %+v, %v", byName, ok)
	}
	for _, name := range []string{"Alice", "alic", "", "alice "} {
		if _, ok := f.ByName(name); ok {
			t.Errorf("ByName(%q) found somebody: the name must be exact", name)
		}
	}

	for _, name := range []string{"Bob", "carol.smith", "dave_2", "x", "0day", "a-b", strings.Repeat("n", 32), "_svc"} {
		f.add(t, name)
	}
	// Sorted by name, ignoring case.
	want := []string{"0day", "_svc", "a-b", "alice", "Bob", "carol.smith", "dave_2", strings.Repeat("n", 32), "x"}
	if !reflect.DeepEqual(f.names(), want) {
		t.Errorf("names = %v, want %v", f.names(), want)
	}
	if !reflect.DeepEqual(f.kinds(), []ChangeKind{PersonAdded, PersonAdded, PersonAdded, PersonAdded, PersonAdded, PersonAdded, PersonAdded, PersonAdded, PersonAdded}) {
		t.Errorf("changes = %v", f.kinds())
	}
	if f.changes[0].PersonID != p.ID {
		t.Errorf("the change names %q", f.changes[0].PersonID)
	}
}

func TestNamesThatAreRefused(t *testing.T) {
	f := newFixture(t)
	f.add(t, "alice")
	invalid := []string{
		"", " ", "alice smith", "-alice", ".alice", strings.Repeat("n", 33), "ali/ce", "ali\\ce", "alice@host", "ali:ce",
		"alice\n", "\x1b[31malice", "ałice", "алиса", "alice\u202E", "a,b", "a\tb", "~alice", "$(id)", "a;b",
	}
	for _, name := range invalid {
		_, err := f.Add(ctx, name)
		errtest.Route(t, err, "Store.Add->checkName.PersonNameInvalid")
	}
	// The name in the metadata is safe to show.
	_, err := f.Add(ctx, "\x1b[31malice")
	if meta := errtest.Route(t, err, "checkName.PersonNameInvalid"); meta["name"] != "\uFFFD[31malice" {
		t.Errorf("metadata = %v", meta)
	}

	for _, name := range []string{"alice", "Alice", "ALICE", "aLiCe"} {
		_, err := f.Add(ctx, name)
		meta := errtest.Route(t, err, "Store.Add->checkName.PersonNameTaken")
		if meta["takenBy"] != "alice" {
			t.Errorf("Add(%q): metadata = %v", name, meta)
		}
	}
	if !reflect.DeepEqual(f.names(), []string{"alice"}) {
		t.Errorf("a refused name was added: %v", f.names())
	}
	if len(f.changes) != 1 {
		t.Errorf("listeners were told %d times, want 1", len(f.changes))
	}
}

func TestRename(t *testing.T) {
	f := newFixture(t)
	alice, bob := f.add(t, "alice"), f.add(t, "bob")
	f.changes = nil

	if err := f.Rename(ctx, alice.ID, "alice"); err != nil {
		t.Fatal(err)
	}
	if len(f.changes) != 0 {
		t.Error("renaming to the same name told the listeners")
	}
	// Another case of the person's own name is a change, and is not "taken".
	if err := f.Rename(ctx, alice.ID, "Alice"); err != nil {
		t.Fatal(err)
	}
	if err := f.Rename(ctx, alice.ID, "zoe"); err != nil {
		t.Fatal(err)
	}
	if !reflect.DeepEqual(f.names(), []string{"bob", "zoe"}) {
		t.Errorf("names = %v", f.names())
	}
	if !reflect.DeepEqual(f.changes, []Change{{Kind: PersonRenamed, PersonID: alice.ID}, {Kind: PersonRenamed, PersonID: alice.ID}}) {
		t.Errorf("changes = %+v", f.changes)
	}
	// The id and the rest stay.
	if got, _ := f.Get(alice.ID); got.Name != "zoe" || !got.CreatedAt.Equal(alice.CreatedAt) {
		t.Errorf("after the rename: %+v", got)
	}

	errtest.Route(t, f.Rename(ctx, alice.ID, "BOB"), "Store.Rename->checkName.PersonNameTaken")
	errtest.Route(t, f.Rename(ctx, bob.ID, "no good"), "Store.Rename->checkName.PersonNameInvalid")
	meta := errtest.Route(t, f.Rename(ctx, "p_0000000000000000", "eve"), "Store.Rename->findPerson.PersonNotFound")
	if meta["personID"] != "p_0000000000000000" {
		t.Errorf("metadata = %v", meta)
	}
	// The old name is free again.
	f.add(t, "alice")
}

func TestSetEnabledAndRemove(t *testing.T) {
	f := newFixture(t)
	alice := f.add(t, "alice")
	k1 := f.addKey(t, alice.ID, newEd25519(t), "one")
	k2 := f.addKey(t, alice.ID, newEd25519(t), "two")
	bob := f.add(t, "bob")
	f.changes = nil

	steps := []struct {
		enabled  bool
		wantKind []ChangeKind
	}{
		{true, nil}, // already enabled
		{false, []ChangeKind{PersonDisabled}},
		{false, nil},
		{true, []ChangeKind{PersonEnabled}},
	}
	for _, step := range steps {
		f.changes = nil
		if err := f.SetEnabled(ctx, alice.ID, step.enabled); err != nil {
			t.Fatal(err)
		}
		if got, _ := f.Get(alice.ID); got.Enabled != step.enabled {
			t.Errorf("Enabled = %v, want %v", got.Enabled, step.enabled)
		}
		if !reflect.DeepEqual(f.kinds(), step.wantKind) {
			t.Errorf("SetEnabled(%v): changes = %v, want %v", step.enabled, f.kinds(), step.wantKind)
		}
	}
	errtest.Route(t, f.SetEnabled(ctx, "p_ffffffffffffffff", false), "Store.SetEnabled->findPerson.PersonNotFound")

	f.changes = nil
	if err := f.Remove(ctx, alice.ID); err != nil {
		t.Fatal(err)
	}
	if !reflect.DeepEqual(f.names(), []string{"bob"}) {
		t.Errorf("names = %v", f.names())
	}
	want := []Change{{Kind: PersonRemoved, PersonID: alice.ID, KeyIDs: []string{k1.ID, k2.ID}}}
	if !reflect.DeepEqual(f.changes, want) {
		t.Errorf("changes = %+v, want %+v", f.changes, want)
	}
	errtest.Route(t, f.Remove(ctx, alice.ID), "Store.Remove->findPerson.PersonNotFound")
	if _, ok := f.Get(bob.ID); !ok {
		t.Error("removing alice removed bob")
	}
}

func TestAddKeys(t *testing.T) {
	f := newFixture(t)
	alice, bob := f.add(t, "alice"), f.add(t, "bob")
	a1, a2, b1 := newEd25519(t), newECDSA(t, ellipticP256()), newEd25519(t)
	f.changes = nil
	f.clock.Advance(time.Minute)

	keys, err := f.AddKeys(ctx, alice.ID, "# laptop and phone\n"+a1.line+" laptop\n"+a2.line+" phone\n")
	if err != nil {
		t.Fatal(err)
	}
	if len(keys) != 2 {
		t.Fatalf("%d keys, want 2", len(keys))
	}
	for i, want := range []struct {
		key     testKey
		comment string
	}{{a1, "laptop"}, {a2, "phone"}} {
		k := keys[i]
		if !ids.Valid("k_", k.ID) || k.Comment != want.comment || k.Type != want.key.public.Type() ||
			k.Fingerprint != ssh.FingerprintSHA256(want.key.public) || !k.AddedAt.Equal(start.Add(time.Minute)) ||
			k.AuthorizedKey() != want.key.line {
			t.Errorf("key %d = %+v", i, k)
		}
		if !strings.HasPrefix(k.Fingerprint, "SHA256:") {
			t.Errorf("fingerprint = %q", k.Fingerprint)
		}
	}
	if got, _ := f.Get(alice.ID); !reflect.DeepEqual(got.Keys, keys) {
		t.Errorf("the person's keys = %+v", got.Keys)
	}
	if want := []Change{{Kind: KeysAdded, PersonID: alice.ID, KeyIDs: []string{keys[0].ID, keys[1].ID}}}; !reflect.DeepEqual(f.changes, want) {
		t.Errorf("changes = %+v", f.changes)
	}

	t.Run("a key somebody has is refused", func(t *testing.T) {
		for _, personID := range []string{alice.ID, bob.ID} {
			_, err := f.AddKeys(ctx, personID, a1.line)
			meta := errtest.Route(t, err, "Store.AddKeys->Store.addKeys->newKeys.KeyAlreadyAssigned")
			if meta["owner"] != "alice" || meta["ownerID"] != alice.ID || meta["fingerprint"] != ssh.FingerprintSHA256(a1.public) {
				t.Errorf("metadata = %v", meta)
			}
		}
	})
	t.Run("all or nothing", func(t *testing.T) {
		before := f.fileContent(t)
		f.changes = nil
		// A new key and one that alice has: neither is added to bob.
		_, err := f.AddKeys(ctx, bob.ID, b1.line+"\n"+a2.line+"\n")
		errtest.Route(t, err, "Store.addKeys->newKeys.KeyAlreadyAssigned")
		// A new key and a line that is not a key.
		_, err = f.AddKeys(ctx, bob.ID, b1.line+"\nnot a key\n")
		meta := errtest.Route(t, err, "Store.AddKeys->Store.addKeys->ParseKeys.KeyParseFailed")
		if meta["line"] != 2 {
			t.Errorf("metadata = %v", meta)
		}
		if got, _ := f.Get(bob.ID); len(got.Keys) != 0 {
			t.Errorf("bob got %d keys from a refused text", len(got.Keys))
		}
		if f.fileContent(t) != before || len(f.changes) != 0 {
			t.Error("a refused text changed the file or told the listeners")
		}
	})
	t.Run("a person that does not exist", func(t *testing.T) {
		_, err := f.AddKeys(ctx, "p_0000000000000000", b1.line)
		errtest.Route(t, err, "Store.AddKeys->Store.addKeys->findPerson.PersonNotFound")
	})
	t.Run("the refused key can then be added alone", func(t *testing.T) {
		f.addKey(t, bob.ID, b1, "bob's")
	})
}

func TestImportKeys(t *testing.T) {
	f := newFixture(t)
	alice := f.add(t, "alice")
	a1, a2 := newEd25519(t), newEd25519(t)
	dir := t.TempDir()

	pub := filepath.Join(dir, "id_ed25519.pub")
	if err := os.WriteFile(pub, []byte(a1.line+" ada@laptop\n"), 0o600); err != nil {
		t.Fatal(err)
	}
	keys, err := f.ImportKeys(ctx, alice.ID, pub)
	if err != nil {
		t.Fatal(err)
	}
	if len(keys) != 1 || keys[0].Comment != "ada@laptop" {
		t.Errorf("imported %+v", keys)
	}

	// The same rules as pasted text.
	_, err = f.ImportKeys(ctx, alice.ID, pub)
	errtest.Route(t, err, "Store.ImportKeys->Store.addKeys->newKeys.KeyAlreadyAssigned")

	missing := filepath.Join(dir, "missing.pub")
	_, err = f.ImportKeys(ctx, alice.ID, missing)
	errtest.Route(t, err, "Store.ImportKeys->ReadFile.StorageFileNotExist")

	big := filepath.Join(dir, "authorized_keys")
	if err := os.WriteFile(big, []byte(a2.line+"\n"+strings.Repeat("# padding\n", MaxImportBytes/10)), 0o600); err != nil {
		t.Fatal(err)
	}
	_, err = f.ImportKeys(ctx, alice.ID, big)
	errtest.Route(t, err, "Store.ImportKeys->ReadFile.FileTooLarge")
	if got, _ := f.Get(alice.ID); len(got.Keys) != 1 {
		t.Errorf("alice has %d keys, want 1", len(got.Keys))
	}
}

func TestKeyCommentAndRemoveKey(t *testing.T) {
	f := newFixture(t)
	alice, bob := f.add(t, "alice"), f.add(t, "bob")
	key := newEd25519(t)
	k := f.addKey(t, alice.ID, key, "old")
	other := f.addKey(t, alice.ID, newEd25519(t), "other")
	f.changes = nil

	comments := []struct{ in, want string }{
		{"new laptop", "new laptop"},
		{"  padded  ", "padded"},
		{"\x1b]0;title\x07", "\uFFFD]0;title\uFFFD"},
		{strings.Repeat("c", 100), strings.Repeat("c", 63) + "…"},
		{"", ""},
	}
	for _, c := range comments {
		if err := f.SetKeyComment(ctx, alice.ID, k.ID, c.in); err != nil {
			t.Fatal(err)
		}
		if got, _ := f.Get(alice.ID); got.Keys[0].Comment != c.want {
			t.Errorf("comment = %q, want %q", got.Keys[0].Comment, c.want)
		}
	}
	changes := len(f.changes)
	if err := f.SetKeyComment(ctx, alice.ID, k.ID, ""); err != nil { // the same comment
		t.Fatal(err)
	}
	if len(f.changes) != changes {
		t.Error("setting the same comment told the listeners")
	}
	if want := (Change{Kind: KeyCommentChanged, PersonID: alice.ID, KeyIDs: []string{k.ID}}); !reflect.DeepEqual(f.changes[0], want) {
		t.Errorf("change = %+v", f.changes[0])
	}
	errtest.Route(t, f.SetKeyComment(ctx, alice.ID, "k_0000000000000000", "x"), "Store.SetKeyComment->findKey.KeyNotFound")
	errtest.Route(t, f.SetKeyComment(ctx, bob.ID, k.ID, "x"), "Store.SetKeyComment->findKey.KeyNotFound") // alice's key, not bob's
	errtest.Route(t, f.SetKeyComment(ctx, "p_0000000000000000", k.ID, "x"), "Store.SetKeyComment->findPerson.PersonNotFound")

	f.changes = nil
	if err := f.RemoveKey(ctx, alice.ID, k.ID); err != nil {
		t.Fatal(err)
	}
	if got, _ := f.Get(alice.ID); len(got.Keys) != 1 || got.Keys[0].ID != other.ID {
		t.Errorf("after the removal: %+v", got.Keys)
	}
	if want := []Change{{Kind: KeyRemoved, PersonID: alice.ID, KeyIDs: []string{k.ID}}}; !reflect.DeepEqual(f.changes, want) {
		t.Errorf("changes = %+v", f.changes)
	}
	meta := errtest.Route(t, f.RemoveKey(ctx, alice.ID, k.ID), "Store.RemoveKey->findKey.KeyNotFound")
	if meta["keyID"] != k.ID || meta["personID"] != alice.ID {
		t.Errorf("metadata = %v", meta)
	}
	errtest.Route(t, f.RemoveKey(ctx, "p_0000000000000000", k.ID), "Store.RemoveKey->findPerson.PersonNotFound")
	// A removed key is free for somebody else.
	f.addKey(t, bob.ID, key, "now bob's")
}

func TestLookup(t *testing.T) {
	f := newFixture(t)
	alice, bob, carol := f.add(t, "alice"), f.add(t, "bob"), f.add(t, "carol")
	aliceKey, bobKey, carolKey, stranger := newEd25519(t), newEd25519(t), newEd25519(t), newEd25519(t)
	ak := f.addKey(t, alice.ID, aliceKey, "")
	ak2 := f.addKey(t, alice.ID, bobKey2(t), "second")
	f.addKey(t, bob.ID, bobKey, "")
	f.addKey(t, carol.ID, carolKey, "")
	if err := f.SetEnabled(ctx, carol.ID, false); err != nil {
		t.Fatal(err)
	}
	second, _ := f.Get(alice.ID)

	tests := []struct {
		name string
		user string
		key  ssh.PublicKey
		want Result
	}{
		{"the person's key", "alice", aliceKey.public, Result{Reason: Accepted, PersonID: alice.ID, PersonName: "alice", KeyID: ak.ID}},
		{"the person's second key", "alice", second.Keys[1].Public, Result{Reason: Accepted, PersonID: alice.ID, PersonName: "alice", KeyID: ak2.ID}},
		{"a key nobody has", "alice", stranger.public, Result{Reason: WrongKey, PersonID: alice.ID, PersonName: "alice"}},
		{"somebody else's key", "alice", bobKey.public, Result{Reason: WrongKey, PersonID: alice.ID, PersonName: "alice", KeyOwner: "bob"}},
		{"a name nobody has", "mallory", stranger.public, Result{Reason: UnknownName}},
		{"a name nobody has, with a known key", "root", aliceKey.public, Result{Reason: UnknownName, KeyOwner: "alice"}},
		{"the name in another case is another name", "Alice", aliceKey.public, Result{Reason: UnknownName, KeyOwner: "alice"}},
		{"the empty name", "", aliceKey.public, Result{Reason: UnknownName, KeyOwner: "alice"}},
		{"a disabled person with their key", "carol", carolKey.public, Result{Reason: Disabled, PersonID: carol.ID, PersonName: "carol"}},
		{"a disabled person with a wrong key", "carol", stranger.public, Result{Reason: Disabled, PersonID: carol.ID, PersonName: "carol"}},
		{"a disabled person with somebody's key", "carol", bobKey.public, Result{Reason: Disabled, PersonID: carol.ID, PersonName: "carol", KeyOwner: "bob"}},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			if got := f.Lookup(tt.user, tt.key); got != tt.want {
				t.Errorf("Lookup(%q) = %+v, want %+v", tt.user, got, tt.want)
			}
		})
	}

	t.Run("an edit applies to the next lookup", func(t *testing.T) {
		if err := f.SetEnabled(ctx, carol.ID, true); err != nil {
			t.Fatal(err)
		}
		if got := f.Lookup("carol", carolKey.public); got.Reason != Accepted {
			t.Errorf("after enabling: %+v", got)
		}
		if err := f.Rename(ctx, alice.ID, "ada"); err != nil {
			t.Fatal(err)
		}
		if got := f.Lookup("alice", aliceKey.public); got.Reason != UnknownName || got.KeyOwner != "ada" {
			t.Errorf("the old name after a rename: %+v", got)
		}
		if got := f.Lookup("ada", aliceKey.public); got.Reason != Accepted {
			t.Errorf("the new name after a rename: %+v", got)
		}
		if err := f.RemoveKey(ctx, alice.ID, ak.ID); err != nil {
			t.Fatal(err)
		}
		if got := f.Lookup("ada", aliceKey.public); got.Reason != WrongKey || got.KeyOwner != "" {
			t.Errorf("after the key's removal: %+v", got)
		}
		if err := f.Remove(ctx, bob.ID); err != nil {
			t.Fatal(err)
		}
		if got := f.Lookup("bob", bobKey.public); got != (Result{Reason: UnknownName}) {
			t.Errorf("after the person's removal: %+v", got)
		}
	})
}

// ellipticP256 is the curve of the tests' ECDSA keys.
func ellipticP256() elliptic.Curve { return elliptic.P256() }

// bobKey2 is one more key for a table.
func bobKey2(t *testing.T) testKey { return newECDSA(t, ellipticP256()) }

func TestTheFileRoundTrips(t *testing.T) {
	f := newFixture(t)
	if f.fileContent(t) != "" {
		t.Fatal("opening wrote the file")
	}
	alice, bob := f.add(t, "alice"), f.add(t, "Bob")
	f.clock.Advance(time.Hour)
	f.addKey(t, alice.ID, newEd25519(t), "laptop")
	f.addKey(t, alice.ID, newRSA(t, 2048), "old server")
	f.addKey(t, bob.ID, newSKEd25519(t), "yubikey")
	if err := f.SetEnabled(ctx, bob.ID, false); err != nil {
		t.Fatal(err)
	}
	before := f.List()

	path := filepath.Join(f.dirs.Config, FileName)
	if runtime.GOOS != "windows" {
		info, err := os.Stat(path)
		if err != nil {
			t.Fatal(err)
		}
		if info.Mode().Perm() != 0o600 {
			t.Errorf("the file's mode is %o, want 600", info.Mode().Perm())
		}
	}
	var root struct {
		Version int `json:"version"`
		People  []struct {
			ID, Name string
			Enabled  bool
			Keys     []struct{ ID, Key, Comment string }
		} `json:"people"`
	}
	if err := json.Unmarshal([]byte(f.fileContent(t)), &root); err != nil {
		t.Fatalf("the file is not JSON: %v", err)
	}
	if root.Version != 1 || len(root.People) != 2 || root.People[0].Name != "alice" || len(root.People[0].Keys) != 2 ||
		root.People[1].Enabled || !strings.HasPrefix(root.People[0].Keys[0].Key, "ssh-ed25519 AAAA") ||
		strings.Contains(root.People[0].Keys[0].Key, "laptop") {
		t.Errorf("the file is not as docs/SPEC.md §7.1 says:\n%s", f.fileContent(t))
	}

	f.reopen(t)
	after := f.List()
	if len(after) != len(before) {
		t.Fatalf("%d people after reopening, want %d", len(after), len(before))
	}
	for i := range before {
		b, a := before[i], after[i]
		if a.ID != b.ID || a.Name != b.Name || a.Enabled != b.Enabled || !a.CreatedAt.Equal(b.CreatedAt) || len(a.Keys) != len(b.Keys) {
			t.Errorf("person %d: %+v, want %+v", i, a, b)
			continue
		}
		for j := range b.Keys {
			bk, ak := b.Keys[j], a.Keys[j]
			if ak.ID != bk.ID || ak.Type != bk.Type || ak.Fingerprint != bk.Fingerprint || ak.Comment != bk.Comment ||
				!ak.AddedAt.Equal(bk.AddedAt) || ak.AuthorizedKey() != bk.AuthorizedKey() {
				t.Errorf("key %d of person %d: %+v, want %+v", j, i, ak, bk)
			}
		}
	}
	// Reopening changed nothing in the file, and the lookup works from the file's keys.
	if got := f.Lookup("alice", before[0].Keys[0].Public); got.Reason != Accepted {
		t.Errorf("Lookup after reopening: %+v", got)
	}
}

func TestListIsACopy(t *testing.T) {
	f := newFixture(t)
	alice := f.add(t, "alice")
	f.addKey(t, alice.ID, newEd25519(t), "one")
	list := f.List()
	list[0].Name = "changed"
	list[0].Keys[0].Comment = "changed"
	got, _ := f.Get(alice.ID)
	got.Keys[0].Comment = "changed too"
	if again := f.List(); again[0].Name != "alice" || again[0].Keys[0].Comment != "one" {
		t.Errorf("the store was changed through a copy: %+v", again[0])
	}
}

func TestOpenRefusesBrokenFiles(t *testing.T) {
	good, other := newEd25519(t), newEd25519(t)
	weak := newRSA(t, 1024)
	person := func(id, name, keys string) string {
		return fmt.Sprintf(`{"id":%q,"name":%q,"enabled":true,"createdAt":"2026-11-24T10:00:00Z","keys":[%s]}`, id, name, keys)
	}
	key := func(id, line string) string {
		return fmt.Sprintf(`{"id":%q,"key":%q,"comment":"c","addedAt":"2026-11-24T10:00:00Z"}`, id, line)
	}
	file := func(people ...string) string {
		return `{"version":1,"people":[` + strings.Join(people, ",") + `]}`
	}
	const (
		p1, p2 = "p_1111111111111111", "p_2222222222222222"
		k1, k2 = "k_1111111111111111", "k_2222222222222222"
	)
	tests := []struct {
		name      string
		content   string
		wantRoute string
	}{
		{"not JSON", `{"version":1,"people":[`, "Open.PeopleParseFailed"},
		{"empty", ``, "Open.PeopleParseFailed"},
		{"people of the wrong type", `{"version":1,"people":{}}`, "Open.PeopleParseFailed"},
		{"a time that is not a time", `{"version":1,"people":[{"id":"p_1111111111111111","name":"a","createdAt":"yesterday"}]}`, "Open.PeopleParseFailed"},
		{"a newer version", `{"version":2,"people":[]}`, "Open.PeopleVersionUnsupported"},
		{"no version", `{"people":[]}`, "Open.PeopleVersionUnsupported"},
		{"a person without an id", file(person("", "alice", "")), "Open.PeopleFileInvalid"},
		{"a malformed person id", file(person("alice", "alice", "")), "Open.PeopleFileInvalid"},
		{"a repeated person id", file(person(p1, "alice", ""), person(p1, "bob", "")), "Open.PeopleFileInvalid"},
		{"a name that is not acceptable", file(person(p1, "alice smith", "")), "Open.PeopleFileInvalid"},
		{"an empty name", file(person(p1, "", "")), "Open.PeopleFileInvalid"},
		{"a name used twice, ignoring case", file(person(p1, "alice", ""), person(p2, "ALICE", "")), "Open.PeopleFileInvalid"},
		{"a malformed key id", file(person(p1, "alice", key("one", good.line))), "Open.PeopleFileInvalid"},
		{"a repeated key id", file(person(p1, "alice", key(k1, good.line)+","+key(k1, other.line))), "Open.PeopleFileInvalid"},
		{"a key that is not a key", file(person(p1, "alice", key(k1, "ssh-ed25519 AAAA"))), "Open.PeopleFileInvalid"},
		{"a weak key", file(person(p1, "alice", key(k1, weak.line))), "Open.PeopleFileInvalid"},
		{"a key with options", file(person(p1, "alice", key(k1, "restrict "+good.line))), "Open.PeopleFileInvalid"},
		{"two keys in one entry", file(person(p1, "alice", key(k1, good.line+"\n"+other.line))), "Open.PeopleFileInvalid"},
		{"an unsupported key", file(person(p1, "alice", key(k1, dsaLine()))), "Open.PeopleFileInvalid"},
		{"a key of two people", file(person(p1, "alice", key(k1, good.line)), person(p2, "bob", key(k2, good.line))), "Open.PeopleFileInvalid"},
		{"a key twice for one person", file(person(p1, "alice", key(k1, good.line)+","+key(k2, good.line))), "Open.PeopleFileInvalid"},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			dirs := storagetest.Isolate(t)
			path := filepath.Join(dirs.Config, FileName)
			if err := os.WriteFile(path, []byte(tt.content), 0o600); err != nil {
				t.Fatal(err)
			}
			_, err := Open(ctx, dirs, clock.NewFake(start))
			meta := errtest.Route(t, err, tt.wantRoute)
			if meta["path"] != path {
				t.Errorf("metadata = %v", meta)
			}
			data, readErr := os.ReadFile(path)
			if readErr != nil {
				t.Fatal(readErr)
			}
			if string(data) != tt.content {
				t.Error("the broken file was changed")
			}
		})
	}

	t.Run("a file that is fine", func(t *testing.T) {
		dirs := storagetest.Isolate(t)
		content := file(person(p1, "alice", key(k1, good.line)), person(p2, "bob", key(k2, other.line+" a comment that is ignored")))
		if err := os.WriteFile(filepath.Join(dirs.Config, FileName), []byte(content), 0o600); err != nil {
			t.Fatal(err)
		}
		s, err := Open(ctx, dirs, clock.NewFake(start))
		if err != nil {
			t.Fatal(err)
		}
		if got := s.Lookup("bob", other.public); got.Reason != Accepted || got.KeyID != k2 {
			t.Errorf("Lookup = %+v", got)
		}
	})
	t.Run("a file that is too large", func(t *testing.T) {
		dirs := storagetest.Isolate(t)
		if err := os.WriteFile(filepath.Join(dirs.Config, FileName), make([]byte, maxFileBytes+1), 0o600); err != nil {
			t.Fatal(err)
		}
		_, err := Open(ctx, dirs, clock.NewFake(start))
		errtest.Route(t, err, "Open->ReadFile.FileTooLarge")
	})
}

func TestChangesThatCannotBeSaved(t *testing.T) {
	t.Run("the file cannot be written", func(t *testing.T) {
		f := newFixture(t)
		alice := f.add(t, "alice")
		k := f.addKey(t, alice.ID, newEd25519(t), "one")
		f.changes = nil
		if err := os.RemoveAll(f.dirs.Config); err != nil {
			t.Fatal(err)
		}
		tail := "Store.commit->WriteFileAtomic->writeFileAtomic.StorageWriteCreateFailed"
		_, err := f.Add(ctx, "bob")
		errtest.Route(t, err, "Store.Add->"+tail)
		errtest.Route(t, f.Rename(ctx, alice.ID, "ada"), "Store.Rename->"+tail)
		errtest.Route(t, f.SetEnabled(ctx, alice.ID, false), "Store.SetEnabled->"+tail)
		_, err = f.AddKeys(ctx, alice.ID, newEd25519(t).line)
		errtest.Route(t, err, "Store.AddKeys->Store.addKeys->"+tail)
		errtest.Route(t, f.SetKeyComment(ctx, alice.ID, k.ID, "two"), "Store.SetKeyComment->"+tail)
		errtest.Route(t, f.RemoveKey(ctx, alice.ID, k.ID), "Store.RemoveKey->"+tail)
		errtest.Route(t, f.Remove(ctx, alice.ID), "Store.Remove->"+tail)
		// Nothing changed, and nobody was told.
		got := f.List()
		if len(got) != 1 || got[0].Name != "alice" || !got[0].Enabled || len(got[0].Keys) != 1 || got[0].Keys[0].Comment != "one" {
			t.Errorf("the store changed although the file was not written: %+v", got)
		}
		if len(f.changes) != 0 {
			t.Errorf("listeners were told of %d changes that did not happen", len(f.changes))
		}
	})
	t.Run("a time that JSON cannot hold", func(t *testing.T) {
		f := newFixture(t)
		// A clock in the year 10000: encoding/json refuses a year with five digits.
		f.clock = clock.NewFake(time.Date(10000, 1, 1, 0, 0, 0, 0, time.UTC))
		f.reopen(t)
		_, err := f.Add(ctx, "alice")
		errtest.Route(t, err, "Store.Add->Store.commit.PeopleEncodeFailed")
		if len(f.List()) != 0 {
			t.Error("the person was added although the file was not written")
		}
	})
	t.Run("a cancelled context", func(t *testing.T) {
		f := newFixture(t)
		cancelled, cancel := context.WithCancel(ctx)
		cancel()
		_, err := f.Add(cancelled, "alice")
		errtest.Route(t, err, "Store.Add->Store.commit->WriteFileAtomic->writeFileAtomic.StorageWriteCancelled")
		_, err = Open(cancelled, f.dirs, f.clock)
		errtest.Route(t, err, "Open->ReadFile.StorageReadCancelled")
	})
}

func TestConcurrentUse(t *testing.T) {
	f := newFixture(t)
	f.changes = nil
	const writers, perWriter = 4, 10
	keys := make([][]testKey, writers)
	for w := range keys {
		for range perWriter {
			keys[w] = append(keys[w], newEd25519(t))
		}
	}
	var wg sync.WaitGroup
	for w := 0; w < writers; w++ {
		wg.Add(1)
		go func() {
			defer wg.Done()
			p, err := f.Add(ctx, fmt.Sprintf("writer%d", w))
			if err != nil {
				t.Errorf("Add: %v", err)
				return
			}
			for i, key := range keys[w] {
				if _, err := f.AddKeys(ctx, p.ID, key.line); err != nil {
					t.Errorf("AddKeys: %v", err)
				}
				if i%3 == 0 {
					if err := f.SetEnabled(ctx, p.ID, i%2 == 0); err != nil {
						t.Errorf("SetEnabled: %v", err)
					}
				}
			}
			if err := f.SetEnabled(ctx, p.ID, true); err != nil {
				t.Errorf("SetEnabled: %v", err)
			}
		}()
	}
	// Readers look up while the writers write.
	stop := make(chan struct{})
	var readers sync.WaitGroup
	for r := 0; r < 4; r++ {
		readers.Add(1)
		go func() {
			defer readers.Done()
			for {
				select {
				case <-stop:
					return
				default:
				}
				f.Lookup("writer0", keys[0][0].public)
				f.List()
			}
		}()
	}
	wg.Wait()
	close(stop)
	readers.Wait()

	for w := 0; w < writers; w++ {
		for _, key := range keys[w] {
			if got := f.Lookup(fmt.Sprintf("writer%d", w), key.public); got.Reason != Accepted {
				t.Errorf("writer %d: Lookup = %+v", w, got)
			}
		}
	}
	// The file has everything.
	f.reopen(t)
	total := 0
	for _, p := range f.List() {
		total += len(p.Keys)
	}
	if len(f.List()) != writers || total != writers*perWriter {
		t.Errorf("the file has %d people and %d keys, want %d and %d", len(f.List()), total, writers, writers*perWriter)
	}
}

func TestSubscribeCancel(t *testing.T) {
	f := newFixture(t)
	var first, second int
	cancel := f.Subscribe(func(Change) { first++ })
	f.Subscribe(func(Change) { second++ })
	f.add(t, "alice")
	cancel()
	cancel()
	f.add(t, "bob")
	if first != 1 || second != 2 {
		t.Errorf("first = %d, second = %d; want 1 and 2", first, second)
	}
}

func TestThePeopleFileIsPrivate(t *testing.T) {
	if runtime.GOOS == "windows" {
		t.Skip("the mode bits mean nothing on Windows: the access is the profile's ACLs (docs/SPEC.md §13.1)")
	}
	f := newFixture(t)
	p := f.add(t, "alice")
	f.addKey(t, p.ID, newEd25519(t), "laptop")
	path := filepath.Join(f.dirs.Config, FileName)
	for name, want := range map[string]os.FileMode{path: 0o600, f.dirs.Config: os.ModeDir | 0o700} {
		info, err := os.Stat(name)
		if err != nil {
			t.Fatal(err)
		}
		if info.Mode() != want {
			t.Errorf("%s has the mode %v, want %v", name, info.Mode(), want)
		}
	}
	// A rewrite keeps it so: every change writes a new temporary file and renames it over.
	f.add(t, "bob")
	if info, err := os.Stat(path); err != nil || info.Mode() != 0o600 {
		t.Errorf("after a second write the file is %v, %v", info, err)
	}
	entries, err := os.ReadDir(f.dirs.Config)
	if err != nil {
		t.Fatal(err)
	}
	for _, e := range entries {
		if e.Name() != FileName {
			t.Errorf("%s is left behind next to the people file", e.Name())
		}
	}
}

func TestAddWithKeys(t *testing.T) {
	f := newFixture(t)
	a1, a2 := newEd25519(t), newECDSA(t, ellipticP256())
	f.clock.Advance(time.Minute)
	p, err := f.AddWithKeys(ctx, "alice", "# keys\n"+a1.line+" laptop\n"+a2.line+" phone\n")
	if err != nil {
		t.Fatal(err)
	}
	if p.Name != "alice" || !p.Enabled || len(p.Keys) != 2 || p.Keys[0].Comment != "laptop" || p.Keys[1].Comment != "phone" {
		t.Errorf("the new person is %+v", p)
	}
	if got, ok := f.Get(p.ID); !ok || !reflect.DeepEqual(got, p) {
		t.Errorf("Get = %+v, %v", got, ok)
	}
	if want := []Change{{Kind: PersonAdded, PersonID: p.ID, KeyIDs: []string{p.Keys[0].ID, p.Keys[1].ID}}}; !reflect.DeepEqual(f.changes, want) {
		t.Errorf("changes = %+v, want one change that holds the person and the keys", f.changes)
	}
	// The keys work at once, and the file holds the person with the keys.
	if res := f.Lookup("alice", a1.public); res.Reason != Accepted {
		t.Errorf("Lookup of the first key = %+v", res)
	}
	if got := f.fileContent(t); !strings.Contains(got, "laptop") || !strings.Contains(got, "alice") {
		t.Errorf("the file lacks the person or the keys: %s", got)
	}

	// Whatever is wrong adds nobody, writes nothing and tells nobody.
	before := f.fileContent(t)
	f.changes = nil
	b1 := newEd25519(t)
	tests := []struct {
		name, person, text, route string
	}{
		{"no key", "bob", "", "Store.AddWithKeys->ParseKeys.KeyTextEmpty"},
		{"only a comment", "bob", "# nothing\n", "Store.AddWithKeys->ParseKeys.KeyTextEmpty"},
		{"a line that is not a key", "bob", b1.line + "\nnot a key\n", "Store.AddWithKeys->ParseKeys.KeyParseFailed"},
		{"a private key", "bob", "-----BEGIN OPENSSH PRIVATE KEY-----\nx\n-----END OPENSSH PRIVATE KEY-----", "Store.AddWithKeys->ParseKeys.KeyIsPrivate"},
		{"a key somebody has", "bob", b1.line + "\n" + a2.line, "Store.AddWithKeys->newKeys.KeyAlreadyAssigned"},
		{"a name that is not valid", "-bob", b1.line, "Store.AddWithKeys->checkName.PersonNameInvalid"},
		{"a name that is taken", "Alice", b1.line, "Store.AddWithKeys->checkName.PersonNameTaken"},
		{"a bad name and no key: the name is told first", "-bob", "", "Store.AddWithKeys->checkName.PersonNameInvalid"},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			_, err := f.AddWithKeys(ctx, tt.person, tt.text)
			errtest.Route(t, err, tt.route)
		})
	}
	if got := f.fileContent(t); got != before {
		t.Error("a refused addition changed the file")
	}
	if len(f.changes) != 0 || !reflect.DeepEqual(f.names(), []string{"alice"}) {
		t.Errorf("a refused addition left changes %+v and the people %v", f.changes, f.names())
	}
}

func TestCheckName(t *testing.T) {
	f := newFixture(t)
	f.add(t, "alice")
	f.changes = nil
	if err := f.CheckName("bob"); err != nil {
		t.Errorf("CheckName(bob) = %v", err)
	}
	errtest.Route(t, f.CheckName("ALICE"), "Store.CheckName->checkName.PersonNameTaken")
	errtest.Route(t, f.CheckName("-x"), "Store.CheckName->checkName.PersonNameInvalid")
	if len(f.changes) != 0 || !reflect.DeepEqual(f.names(), []string{"alice"}) {
		t.Errorf("CheckName changed something: %+v %v", f.changes, f.names())
	}
}
