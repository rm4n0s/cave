// Package settings holds Cave's settings: their names, kinds, defaults and limits, the file they
// are kept in, and the store that validates every change and tells its listeners
// (docs/SPEC.md §6).
package settings

import (
	"net/netip"
	"strconv"
	"strings"
)

// BoolKey is the name of a setting that is on or off.
type BoolKey string

// IntKey is the name of a setting that is a whole number in a range.
type IntKey string

// StringKey is the name of a setting that is text, or one of a list of choices.
type StringKey string

// StringsKey is the name of a setting that is a list of texts.
type StringsKey string

// The settings (docs/SPEC.md §6).
const (
	ServerPort             IntKey     = "server.port"
	ServerAddress          StringKey  = "server.address"
	ServerStartOnLaunch    BoolKey    = "server.startOnLaunch"
	ServerKeepAliveSeconds IntKey     = "server.keepAliveSeconds"
	AttemptsClear          StringKey  = "attempts.clear"
	SessionShell           StringKey  = "session.shell"
	SessionAcceptEnv       StringsKey = "session.acceptEnv"
	SFTPEnabled            BoolKey    = "sftp.enabled"
	ForwardingLocal        BoolKey    = "forwarding.local"
	ForwardingRemote       BoolKey    = "forwarding.remote"
	ForwardingGatewayPorts BoolKey    = "forwarding.gatewayPorts"
	ForwardingAgent        BoolKey    = "forwarding.agent"
	NotifyAccepted         BoolKey    = "notify.accepted"
	NotifyRejected         BoolKey    = "notify.rejected"
	UITheme                StringKey  = "ui.theme"
	UICloseToTray          BoolKey    = "ui.closeToTray"
)

// The choices of attempts.clear (docs/SPEC.md §8.4).
const (
	ClearNever   = "never"
	ClearOnStart = "onStart"
	ClearHourly  = "hourly"
	ClearDaily   = "daily"
	ClearWeekly  = "weekly"
	ClearMonthly = "monthly"
)

// The choices of ui.theme.
const (
	ThemeSystem = "system"
	ThemeLight  = "light"
	ThemeDark   = "dark"
)

// Kind is what kind of value a setting has.
type Kind int

// The kinds of settings.
const (
	// KindBool is a bool.
	KindBool Kind = iota
	// KindInt is an int between Min and Max.
	KindInt
	// KindString is a string.
	KindString
	// KindChoice is a string that is one of Choices.
	KindChoice
	// KindStrings is a []string.
	KindStrings
)

// Limits of the free-text settings.
const (
	// maxShellLength bounds session.shell.
	maxShellLength = 4096
	// maxEnvPatterns bounds how many patterns session.acceptEnv has.
	maxEnvPatterns = 64
	// maxEnvPatternLength bounds one pattern of session.acceptEnv.
	maxEnvPatternLength = 256
)

// Def describes one setting.
type Def struct {
	// Key is the setting's name.
	Key string
	// Kind is the kind of its value.
	Kind Kind
	// Default is the value it has when the file does not set it: a bool, an int, a string or a
	// []string, by Kind.
	Default any
	// Min and Max are the range of a KindInt, both included.
	Min, Max int
	// Choices are the values of a KindChoice.
	Choices []string
	// check says why a value of the right kind is still not acceptable, or "".
	check func(value any) string
}

// schema is every setting, in the order of docs/SPEC.md §6. It is never changed.
var schema = []Def{
	{Key: string(ServerPort), Kind: KindInt, Default: 2222, Min: 1, Max: 65535},
	{Key: string(ServerAddress), Kind: KindString, Default: "", check: checkAddress},
	{Key: string(ServerStartOnLaunch), Kind: KindBool, Default: true},
	{Key: string(ServerKeepAliveSeconds), Kind: KindInt, Default: 60, Min: 0, Max: 3600},
	{Key: string(AttemptsClear), Kind: KindChoice, Default: ClearNever,
		Choices: []string{ClearNever, ClearOnStart, ClearHourly, ClearDaily, ClearWeekly, ClearMonthly}},
	{Key: string(SessionShell), Kind: KindString, Default: "", check: checkShell},
	{Key: string(SessionAcceptEnv), Kind: KindStrings, Default: []string{"LANG", "LC_*"}, check: checkEnvPatterns},
	{Key: string(SFTPEnabled), Kind: KindBool, Default: true},
	{Key: string(ForwardingLocal), Kind: KindBool, Default: true},
	{Key: string(ForwardingRemote), Kind: KindBool, Default: true},
	{Key: string(ForwardingGatewayPorts), Kind: KindBool, Default: false},
	{Key: string(ForwardingAgent), Kind: KindBool, Default: true},
	{Key: string(NotifyAccepted), Kind: KindBool, Default: true},
	{Key: string(NotifyRejected), Kind: KindBool, Default: false},
	{Key: string(UITheme), Kind: KindChoice, Default: ThemeSystem, Choices: []string{ThemeSystem, ThemeLight, ThemeDark}},
	{Key: string(UICloseToTray), Kind: KindBool, Default: false},
}

// Schema returns the description of every setting, in the order of docs/SPEC.md §6. The result
// is the caller's own copy.
func Schema() []Def {
	out := make([]Def, len(schema))
	for i, def := range schema {
		out[i] = def
		out[i].Choices = append([]string(nil), def.Choices...)
		if list, ok := def.Default.([]string); ok {
			out[i].Default = append([]string(nil), list...)
		}
	}
	return out
}

// lookup finds a setting's description.
func lookup(key string) (Def, bool) {
	for _, def := range schema {
		if def.Key == key {
			return def, true
		}
	}
	return Def{}, false
}

// normalize checks a value against a setting's description and returns it in the Go type of the
// setting's kind. A whole number may come as an int or, from JSON, as a float64, and a list as a
// []string or a []any of strings. When the value is not acceptable it returns why.
func normalize(def Def, value any) (any, string) {
	var out any
	switch def.Kind {
	case KindBool:
		b, ok := value.(bool)
		if !ok {
			return nil, "it must be true or false"
		}
		out = b
	case KindInt:
		var n int
		switch v := value.(type) {
		case int:
			n = v
		case float64:
			n = int(v)
			if float64(n) != v {
				return nil, "it must be a whole number"
			}
		default:
			return nil, "it must be a whole number"
		}
		if n < def.Min || n > def.Max {
			return nil, "it must be between " + strconv.Itoa(def.Min) + " and " + strconv.Itoa(def.Max)
		}
		out = n
	case KindString, KindChoice:
		s, ok := value.(string)
		if !ok {
			return nil, "it must be text"
		}
		if def.Kind == KindChoice {
			found := false
			for _, choice := range def.Choices {
				if s == choice {
					found = true
				}
			}
			if !found {
				return nil, "it must be one of " + strings.Join(def.Choices, ", ")
			}
		}
		out = s
	case KindStrings:
		switch v := value.(type) {
		case []string:
			out = append([]string{}, v...)
		case []any:
			list := make([]string, 0, len(v))
			for _, item := range v {
				s, ok := item.(string)
				if !ok {
					return nil, "it must be a list of texts"
				}
				list = append(list, s)
			}
			out = list
		default:
			return nil, "it must be a list of texts"
		}
	}
	if def.check != nil {
		if reason := def.check(out); reason != "" {
			return nil, reason
		}
	}
	return out, ""
}

// checkAddress accepts an empty address (every interface) or an IP literal without a zone.
func checkAddress(value any) string {
	s := value.(string)
	if s == "" {
		return ""
	}
	addr, err := netip.ParseAddr(s)
	if err != nil || addr.Zone() != "" {
		return "it must be empty or an IP address, such as 192.168.1.5 or ::1"
	}
	return ""
}

// checkShell accepts a shell's path of a sane length without a NUL.
func checkShell(value any) string {
	s := value.(string)
	if len(s) > maxShellLength {
		return "it is too long"
	}
	if strings.ContainsRune(s, 0) {
		return "it must not contain a NUL character"
	}
	return ""
}

// checkEnvPatterns accepts a bounded list of patterns made of the characters of a variable's
// name and "*".
func checkEnvPatterns(value any) string {
	list := value.([]string)
	if len(list) > maxEnvPatterns {
		return "it has too many patterns"
	}
	for _, pattern := range list {
		if pattern == "" || len(pattern) > maxEnvPatternLength {
			return "a pattern must have between 1 and 256 characters"
		}
		for _, r := range pattern {
			letter := (r >= 'A' && r <= 'Z') || (r >= 'a' && r <= 'z')
			digit := r >= '0' && r <= '9'
			if !letter && !digit && r != '_' && r != '*' {
				return "a pattern may have only letters, digits, _ and *"
			}
		}
	}
	return ""
}

// MatchEnvName says whether the name of an environment variable matches one of the patterns of
// session.acceptEnv. In a pattern, "*" matches any run of characters, also none; every other
// character matches itself.
func MatchEnvName(patterns []string, name string) bool {
	for _, pattern := range patterns {
		if matchPattern(pattern, name) {
			return true
		}
	}
	return false
}

// matchPattern matches one pattern, in time proportional to the two lengths.
func matchPattern(pattern, name string) bool {
	// p and n walk the pattern and the name; star and mark remember the last "*" and where in the
	// name it was tried, to come back to when a later character does not match.
	p, n, star, mark := 0, 0, -1, 0
	for n < len(name) {
		switch {
		case p < len(pattern) && pattern[p] == '*':
			star, mark = p, n
			p++
		case p < len(pattern) && pattern[p] == name[n]:
			p++
			n++
		case star >= 0:
			mark++
			p, n = star+1, mark
		default:
			return false
		}
	}
	for p < len(pattern) && pattern[p] == '*' {
		p++
	}
	return p == len(pattern)
}
