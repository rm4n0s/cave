package app

// SettingText is how the user interfaces present one setting (docs/SPEC.md §6, §11.5).
type SettingText struct {
	// Key is the name of the setting, as settings.Schema has it.
	Key string
	// Group is the heading it is shown under.
	Group string
	// Label is its name for the user.
	Label string
	// Help is one sentence that says what it does.
	Help string
	// DesktopOnly settings do nothing in the TUI: it shows them greyed.
	DesktopOnly bool
}

// The groups of the settings, in the order they are shown.
const (
	GroupServer        = "Server"
	GroupAttempts      = "Attempts and history"
	GroupSessions      = "Sessions"
	GroupForwarding    = "File transfer and forwarding"
	GroupNotifications = "Notifications"
	GroupAppearance    = "Appearance"
)

// settingTexts is the text of every setting, in the order of settings.Schema. It is never
// changed.
var settingTexts = []SettingText{
	{Key: "server.port", Group: GroupServer, Label: "Port",
		Help: "The port Cave listens on for connections."},
	{Key: "server.address", Group: GroupServer, Label: "Address",
		Help: "The address to listen on; empty means every network interface."},
	{Key: "server.startOnLaunch", Group: GroupServer, Label: "Start when Cave starts",
		Help: "Start the server as soon as Cave opens."},
	{Key: "server.keepAliveSeconds", Group: GroupServer, Label: "Keep-alive (seconds)",
		Help: "How often Cave checks that a connected client is still there, and disconnects one that stays silent for three more checks; 0 turns this off."},
	{Key: "attempts.clear", Group: GroupAttempts, Label: "Clear the list",
		Help: "When the list of connection attempts is emptied by itself."},
	{Key: "history.clear", Group: GroupAttempts, Label: "Clear the history",
		Help: "When the history of connections and transfers is emptied by itself."},
	{Key: "session.shell", Group: GroupSessions, Label: "Shell",
		Help: "The shell that people get; empty means your own shell."},
	{Key: "session.acceptEnv", Group: GroupSessions, Label: "Variables clients may set",
		Help: "Names of environment variables that a client may set in its session, separated by commas; * matches any run of characters."},
	{Key: "sftp.enabled", Group: GroupForwarding, Label: "File transfer (SFTP)",
		Help: "Let people copy files with sftp and scp."},
	{Key: "forwarding.local", Group: GroupForwarding, Label: "Local forwarding",
		Help: "Let people open connections from this computer (ssh -L and -D)."},
	{Key: "forwarding.remote", Group: GroupForwarding, Label: "Remote forwarding",
		Help: "Let people listen on a port of this computer (ssh -R)."},
	{Key: "forwarding.gatewayPorts", Group: GroupForwarding, Label: "Forwarded ports open to the network",
		Help: "Let a remote forward listen on addresses other than this computer's own loopback address."},
	{Key: "forwarding.agent", Group: GroupForwarding, Label: "Agent forwarding",
		Help: "Let people forward their SSH agent (ssh -A). While a person is connected with ssh -A, " +
			"the programs of this computer's user and of the other connected people can use that person's agent."},
	{Key: "notify.accepted", Group: GroupNotifications, Label: "Notify when somebody connects", DesktopOnly: true,
		Help: "Show a desktop notification when a person connects."},
	{Key: "notify.rejected", Group: GroupNotifications, Label: "Notify when a connection is refused", DesktopOnly: true,
		Help: "Show a desktop notification when an attempt is refused, at most one a minute."},
	{Key: "ui.theme", Group: GroupAppearance, Label: "Theme", DesktopOnly: true,
		Help: "Light, dark, or the same as the system."},
	{Key: "ui.closeToTray", Group: GroupAppearance, Label: "Close to the tray", DesktopOnly: true,
		Help: "The close button hides the window and Cave keeps running in the tray, when the desktop has a tray."},
}

// SettingTexts returns the text of every setting, in the order the user interfaces show them.
// The result is the caller's own copy.
func SettingTexts() []SettingText {
	return append([]SettingText(nil), settingTexts...)
}
