package settings

import (
	"context"
	"encoding/json"
	"path/filepath"
	"reflect"
	"sync"
	"sync/atomic"

	"github.com/rm4n0s/cave/internal/core/storage"
	"github.com/rm4n0s/errors"
)

// The Tags of this package's errors.
const (
	TagParseFailed        = "SettingsParseFailed"
	TagVersionUnsupported = "SettingsVersionUnsupported"
	TagFileValueInvalid   = "SettingsFileValueInvalid"
	TagEncodeFailed       = "SettingsEncodeFailed"
	TagSettingUnknown     = "SettingUnknown"
	TagSettingInvalid     = "SettingInvalid"
)

// FileName is the name of the settings file in the configuration folder.
const FileName = "settings.json"

// fileVersion is the version of the file's format that this code reads and writes.
const fileVersion = 1

// maxFileBytes bounds the settings file.
const maxFileBytes = 1 << 20

// file is the settings file as JSON.
type file struct {
	Version  int                        `json:"version"`
	Settings map[string]json.RawMessage `json:"settings"`
}

// state is the content of the store at one moment. It is never changed after it is made.
type state struct {
	// explicit holds the values that the file sets, normalized.
	explicit map[string]any
	// unknown holds the entries of the file whose keys this version does not know, to write
	// them back as they are.
	unknown map[string]json.RawMessage
}

// Snapshot is the value of every setting at one moment. It is safe to keep and to share.
type Snapshot struct {
	state *state
}

// value returns a setting's value: the explicit one, else the default.
func (s Snapshot) value(key string) any {
	if s.state != nil {
		if v, ok := s.state.explicit[key]; ok {
			return v
		}
	}
	def, _ := lookup(key)
	return def.Default
}

// Bool returns the value of an on/off setting.
func (s Snapshot) Bool(key BoolKey) bool {
	v, _ := s.value(string(key)).(bool)
	return v
}

// Int returns the value of a number setting.
func (s Snapshot) Int(key IntKey) int {
	v, _ := s.value(string(key)).(int)
	return v
}

// String returns the value of a text or choice setting.
func (s Snapshot) String(key StringKey) string {
	v, _ := s.value(string(key)).(string)
	return v
}

// Strings returns the value of a list setting, as the caller's own copy.
func (s Snapshot) Strings(key StringsKey) []string {
	v, _ := s.value(string(key)).([]string)
	return append([]string(nil), v...)
}

// Value returns the value of a setting by its name, in the Go type of its kind (a list as the
// caller's own copy), and whether the setting exists. It is for code that walks the Schema.
func (s Snapshot) Value(key string) (any, bool) {
	if _, ok := lookup(key); !ok {
		return nil, false
	}
	v := s.value(key)
	if list, ok := v.([]string); ok {
		return append([]string(nil), list...), true
	}
	return v, true
}

// Store holds the settings and their file. It is safe for concurrent use.
type Store struct {
	path string

	// write serializes Set, so that the file and the state change together.
	write   sync.Mutex
	current atomic.Pointer[state]

	listenersMu sync.Mutex
	listeners   []subscription
	nextID      int
}

// subscription is one listener of a Store.
type subscription struct {
	id int
	fn func(key string)
}

// Open reads the settings file of the configuration folder. A missing file means the defaults.
// A file that does not parse, has another version, or sets a known setting to a value that is
// not acceptable is an error, and the file is left as it is (docs/SPEC.md §5.2, §6).
func Open(ctx context.Context, dirs storage.Dirs) (*Store, error) {
	s := &Store{path: filepath.Join(dirs.Config, FileName)}
	st := &state{explicit: map[string]any{}, unknown: map[string]json.RawMessage{}}
	data, err := storage.ReadFile(ctx, s.path, maxFileBytes)
	if errors.Is(err, storage.ErrNotExist) {
		s.current.Store(st)
		return s, nil
	}
	if err != nil {
		return nil, err
	}
	var f file
	if err := json.Unmarshal(data, &f); err != nil {
		return nil, errors.NewErr(TagParseFailed, err).SetMetadata("path", s.path)
	}
	if f.Version != fileVersion {
		return nil, errors.New(TagVersionUnsupported, "the settings file has a version this Cave does not read",
			"path", s.path, "version", f.Version, "supported", fileVersion)
	}
	for key, raw := range f.Settings {
		def, ok := lookup(key)
		if !ok {
			st.unknown[key] = raw
			continue
		}
		var decoded any
		reason := "it is not a JSON value"
		if err := json.Unmarshal(raw, &decoded); err == nil {
			decoded, reason = normalize(def, decoded)
		}
		if reason != "" {
			return nil, errors.New(TagFileValueInvalid, "the settings file has a value that is not acceptable for "+key+": "+reason,
				"path", s.path, "key", key, "reason", reason)
		}
		st.explicit[key] = decoded
	}
	s.current.Store(st)
	return s, nil
}

// Snapshot returns the settings as they are now.
func (s *Store) Snapshot() Snapshot {
	return Snapshot{state: s.current.Load()}
}

// Set changes one setting: it checks the value, writes the file, and then tells the listeners.
// The value is a bool, an int, a string or a []string, by the setting's kind. Setting a value
// that the setting already has does nothing. When the file cannot be written, nothing changes.
func (s *Store) Set(ctx context.Context, key string, value any) error {
	def, ok := lookup(key)
	if !ok {
		return errors.New(TagSettingUnknown, "there is no setting named "+key, "key", key)
	}
	normalized, reason := normalize(def, value)
	if reason != "" {
		return errors.New(TagSettingInvalid, "the value is not acceptable for "+key+": "+reason, "key", key, "reason", reason)
	}

	s.write.Lock()
	old := s.current.Load()
	if reflect.DeepEqual(Snapshot{state: old}.value(key), normalized) {
		s.write.Unlock()
		return nil
	}
	next := &state{explicit: make(map[string]any, len(old.explicit)+1), unknown: old.unknown}
	for k, v := range old.explicit {
		next.explicit[k] = v
	}
	next.explicit[key] = normalized
	if err := s.save(ctx, next); err != nil {
		s.write.Unlock()
		return err
	}
	s.current.Store(next)
	s.write.Unlock()

	s.notify(key)
	return nil
}

// save writes a state to the file.
func (s *Store) save(ctx context.Context, st *state) error {
	values := make(map[string]any, len(st.explicit)+len(st.unknown))
	for key, raw := range st.unknown {
		values[key] = raw
	}
	for key, value := range st.explicit {
		values[key] = value
	}
	data, err := json.MarshalIndent(struct {
		Version  int            `json:"version"`
		Settings map[string]any `json:"settings"`
	}{fileVersion, values}, "", "  ")
	if err != nil {
		return errors.NewErr(TagEncodeFailed, err).SetMetadata("path", s.path)
	}
	return storage.WriteFileAtomic(ctx, s.path, append(data, '\n'))
}

// Subscribe registers a function that is called, with the setting's name, after each change.
// It is called on the goroutine that made the change, after the change is in the file and in
// the store. The returned function ends the subscription.
func (s *Store) Subscribe(listener func(key string)) (cancel func()) {
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
func (s *Store) notify(key string) {
	s.listenersMu.Lock()
	listeners := s.listeners
	s.listenersMu.Unlock()
	for _, sub := range listeners {
		sub.fn(key)
	}
}
