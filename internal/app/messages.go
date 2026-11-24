package app

import (
	"fmt"
	"strings"

	"github.com/rm4n0s/cave/internal/core/sanitize"
	"github.com/rm4n0s/errors"
)

// maxMessageValue is the most characters of one value that is put into a sentence, and
// maxMessageText the most of an error's own text that is shown when it has no sentence.
const (
	maxMessageValue = 120
	maxMessageText  = 200
)

// messages holds the sentence of every error Tag that a command, the opening of the application
// or the server can give to a user interface. It is never changed after the program starts.
//
// A sentence is written for a person who does not know SSH: what happened and, when there is
// something to do, what to do. It may hold placeholders: {name} is the metadata value of that
// key, {cause} the text of the error that was wrapped (what the system said), and {message}
// the error's own message. A value is made safe with sanitize.Display before it is put in.
// Every Tag of the folders that TestEveryTagHasAMessage lists has an entry.
var messages = map[string]string{
	// Application and command line.
	"RunningAsRoot":           "Cave does not run as root, because every person it lets in would get a root shell. Start it as your own user.",
	"FlagsInvalid":            "The command line is not right: {cause}. Run with --help to see the flags.",
	"FlagsHelp":               "Run with --help to see the flags.",
	"FlagsUnexpectedArgument": "Cave takes flags only, not {argument}. Run with --help to see them.",
	"FlagsPortInvalid":        "A port is a number from 1 to 65535, not {port}.",

	// People and keys.
	"PersonNameInvalid":  "A name has 1 to 32 letters, digits, dots, underscores or hyphens, and does not start with a dot or a hyphen.",
	"PersonNameTaken":    "There is already a person named {takenBy}.",
	"PersonNotFound":     "There is no such person any more.",
	"KeyNotFound":        "This person has no such key any more.",
	"KeyAlreadyAssigned": "This key ({fingerprint}) already belongs to {owner}.",
	"KeyIsPrivate":       "This is a private key. Paste the public key instead: the file that ends in .pub.",
	"KeyOptionsUnsupported": "Line {line}: a key with options before its type, such as command=\"…\", is not supported. " +
		"Paste only the key.",
	"KeyTypeUnsupported": "Line {line}: keys of the type {type} are not supported. Cave takes ed25519, ecdsa, rsa and security keys.",
	"KeyTooWeak":         "Line {line}: an RSA key needs at least {minBits} bits.",
	"KeyParseFailed":     "Line {line}: this is not a public key. Paste the contents of a .pub file.",
	"KeyTextEmpty":       "There is no public key in the text. Paste the contents of a .pub file.",
	"PeopleParseFailed":  "The people file {path} cannot be read: {cause}. Fix it or move it away.",
	"PeopleVersionUnsupported": "The people file {path} was written by another version of Cave (it has version {version}; " +
		"this Cave reads version {supported}).",
	"PeopleFileInvalid":    "The people file {path} breaks a rule: {reason}. Fix it or move it away.",
	"PeopleEncodeFailed":   "The people could not be saved to {path}: {cause}.",
	"AttemptNotFound":      "That entry is no longer in the list.",
	"ConnectionNotFound":   "That connection has already ended.",
	"AttemptsParseFailed":  "The attempts file {path} cannot be read: {cause}. Fix it or move it away.",
	"AttemptsFileInvalid":  "The attempts file {path} has an entry that cannot be right (entry {index}). Fix it or move it away.",
	"AttemptsEncodeFailed": "The attempts list could not be saved to {path}: {cause}.",
	"AttemptsVersionUnsupported": "The attempts file {path} was written by another version of Cave (it has version {version}; " +
		"this Cave reads version {supported}).",

	// The host key.
	"HostKeyGenerateFailed":  "A new host key could not be made: {cause}.",
	"HostKeyEncodeFailed":    "A new host key could not be written: {cause}.",
	"HostKeyParseFailed":     "The host key file {path} is not a private key that Cave can read: {cause}. Cave will not replace it by itself.",
	"HostKeyTypeUnsupported": "The host key in {path} is a key of the type {type}; Cave needs an ed25519 key. Cave will not replace it by itself.",

	// The server.
	"ListenFailed":         "Cave cannot listen on {address}: {cause}. Choose another port in Settings.",
	"ServerRelistenFailed": "Cave cannot listen on {address} any more: {cause}. The server is stopped; choose another port in Settings.",
	"ServerAcceptFailed":   "The server could not accept a connection: {cause}.",
	"ServerStartCancelled": "The server was not started: the request was cancelled.",
	"ServerStopTimedOut":   "Some connections did not end in time. The server is stopped.",

	// Settings.
	"SettingInvalid":             "The value is not acceptable for {key}: {reason}.",
	"SettingUnknown":             "There is no setting named {key}.",
	"SettingsFileValueInvalid":   "The settings file {path} has a value that is not acceptable for {key}: {reason}. Fix it or move it away.",
	"SettingsParseFailed":        "The settings file {path} cannot be read: {cause}. Fix it or move it away.",
	"SettingsEncodeFailed":       "The settings could not be saved to {path}: {cause}.",
	"SettingsVersionUnsupported": "The settings file {path} was written by another version of Cave (it has version {version}; this Cave reads version {supported}).",

	// The log.
	"LogLevelInvalid":    "The log level must be debug, info, warn or error, not {level}.",
	"LogOpenCancelled":   "The log file {path} was not opened: the request was cancelled.",
	"LogOpenFailed":      "The log file {path} cannot be opened: {cause}.",
	"LogReopenFailed":    "The log file {path} cannot be opened again: {cause}.",
	"LogRotateFailed":    "The log file {path} could not be moved aside: {cause}.",
	"LogWriteFailed":     "The log file {path} cannot be written: {cause}.",
	"LogWriteAfterClose": "The log file {path} is already closed.",
	"LogCloseFailed":     "The log file {path} could not be closed: {cause}.",

	// Folders and files.
	"AlreadyRunning":                "Cave is already running on these folders. Quit the other one first, or use other folders with --config-dir and --state-dir.",
	"FileTooLarge":                  "The file {path} is too large (the limit is {maxBytes} bytes).",
	"StorageDirNotAbsolute":         "A folder given on the command line or in a CAVE_ variable must be an absolute path, not {dir}.",
	"StorageHomeUnknown":            "HOME is not set, so Cave does not know where to keep its files. Use --config-dir and --state-dir.",
	"StorageAppDataUnknown":         "%AppData% is not set, so Cave does not know where to keep its settings. Use --config-dir.",
	"StorageLocalAppDataUnknown":    "%LocalAppData% is not set, so Cave does not know where to keep its state. Use --state-dir.",
	"StorageDirCreateFailed":        "The folder {dir} cannot be made: {cause}.",
	"StorageEnsureCancelled":        "The folders were not made: the request was cancelled.",
	"StorageFileNotExist":           "The file {path} does not exist.",
	"StorageFileOpenFailed":         "The file {path} cannot be opened: {cause}.",
	"StorageFileReadFailed":         "The file {path} cannot be read: {cause}.",
	"StorageReadCancelled":          "The file {path} was not read: the request was cancelled.",
	"StorageWriteCancelled":         "The file {path} was not written: the request was cancelled.",
	"StorageWriteCreateFailed":      "The file {path} cannot be written: no temporary file can be made beside it ({cause}).",
	"StorageWriteFailed":            "The file {path} cannot be written: {cause}.",
	"StorageWriteSyncFailed":        "The file {path} could not be saved to the disk: {cause}.",
	"StorageWriteCloseFailed":       "The file {path} could not be closed: {cause}.",
	"StorageWriteRenameFailed":      "The file {path} could not be put in place: {cause}.",
	"StorageWriteDirSyncFailed":     "The folder of {path} could not be saved to the disk: {cause}.",
	"StorageLockCancelled":          "Cave was not started: the request was cancelled.",
	"StorageLockOpenFailed":         "The lock file {path} cannot be opened: {cause}.",
	"StorageLockFailed":             "The lock file {path} cannot be locked: {cause}.",
	"StorageLockWriteFailed":        "The lock file {path} cannot be written: {cause}.",
	"StorageLockReleaseFailed":      "The lock file {path} could not be released: {cause}.",
	"StorageLockCloseFailed":        "The lock file {path} could not be closed: {cause}.",
	"StorageRuntimeDirCreateFailed": "The runtime folder {dir} cannot be made: {cause}.",
	"StorageRuntimeDirStatFailed":   "The runtime folder {dir} cannot be looked at: {cause}.",
	"StorageRuntimeDirUnsafe":       "The runtime folder {dir} must belong to you and be closed to others (mode 0700), and it is {mode}.",
}

// Message turns an error into the sentence a user interface shows (docs/SPEC.md §10). A nil
// error gives "". An error of this program's own kind with a sentence in the table gives that
// sentence with its placeholders filled in; any other error gives its own text, made safe and
// cut short, because the text of a system error can hold what a client sent.
func Message(err error) string {
	if err == nil {
		return ""
	}
	appErr, ok := errors.FromError(err)
	if !ok {
		return sanitize.Display(err.Error(), maxMessageText)
	}
	sentence, ok := messages[appErr.Tag]
	if !ok {
		return sanitize.Display(appErr.Message, maxMessageText)
	}
	return fill(sentence, appErr)
}

// fill puts the values of the error in the placeholders of the sentence. A key the error does not
// have gives "?", so that a mistake in a sentence shows in a test and does not hide the rest.
func fill(sentence string, appErr *errors.Error) string {
	var out strings.Builder
	for {
		open := strings.IndexByte(sentence, '{')
		if open < 0 {
			break
		}
		end := strings.IndexByte(sentence[open:], '}')
		if end < 0 {
			break
		}
		out.WriteString(sentence[:open])
		out.WriteString(placeholder(sentence[open+1:open+end], appErr))
		sentence = sentence[open+end+1:]
	}
	out.WriteString(sentence)
	return out.String()
}

// placeholder returns the value of one placeholder, made safe.
func placeholder(key string, appErr *errors.Error) string {
	var value string
	switch key {
	case "cause":
		if appErr.OriginalErr != nil {
			value = appErr.OriginalErr.Error()
		}
	case "message":
		value = appErr.Message
	default:
		v, ok := appErr.Metadata[key]
		if !ok {
			return "?"
		}
		value = fmt.Sprint(v)
	}
	return sanitize.Display(value, maxMessageValue)
}
