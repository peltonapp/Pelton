// logging.go wires internal/logging into the app: where the log directory is,
// what the settings say, and what forces logging on regardless of them.
package desktop

import (
	"log/slog"
	"os"
	"path/filepath"
	"slices"
	"sync"
	"time"

	"github.com/peltonapp/Pelton/internal/logging"
	"github.com/peltonapp/Pelton/internal/storage"
)

// logging setting keys. All of them default off on a stable build.
const (
	// settingLogToFile writes the app's slog output to a rotating file in the
	// log directory. Off means stderr only, as before, and no directory.
	settingLogToFile = "log_to_file"
	// settingLogLevel is the threshold: debug, info, warn or error.
	settingLogLevel = "log_level"
	// settingLogMessageMetadata additionally allows per-message identifiers
	// (uid, subject, sender) into the log. Its own opt-in, because it is the
	// one switch here that puts anything about the user's mail on disk.
	settingLogMessageMetadata = "log_message_metadata"
	// settingCrashLogs writes a file with the stack when the app panics.
	settingCrashLogs = "crash_logs"
)

// defaultLogLevel is what the level control starts on.
const defaultLogLevel = "info"

// debugEnvVar and debugFlag force file logging on at debug level, over
// whatever the settings say. They exist for the case the settings toggle
// cannot help with: the app not starting far enough to reach settings.
const (
	debugEnvVar = "PELTON_DEBUG"
	debugFlag   = "--debug"
)

// debugForced reports whether the env var or the flag was used.
func debugForced(args []string) bool {
	return os.Getenv(debugEnvVar) != "" || slices.Contains(args, debugFlag)
}

// goSafe runs fn on its own goroutine with a crash handler attached. activity
// names what it was doing, in the words the user would use, so a crash file
// opens with a sentence rather than a stack.
//
// Every goroutine the app starts goes through here. A panic in one of them
// takes the whole process down whether it is handled or not; the difference is
// whether anything is left to read afterwards.
func goSafe(activity string, fn func()) {
	backgroundWork.Go(func() {
		defer logging.Guard(activity)
		fn()
	})
}

// backgroundWork counts the goroutines goSafe has running. Several of them
// outlive the call that started them and keep reading and writing the store, so
// there has to be a way to tell when they are finished before it closes under
// them.
var backgroundWork sync.WaitGroup

// waitForBackgroundWork waits for every goroutine goSafe started to return,
// giving up after timeout and reporting whether they all finished.
//
// The long-lived ones (the idle loops, the outbox worker, the pollers) only
// return when their context is cancelled, so cancel it first or this waits out
// the whole timeout.
func waitForBackgroundWork(timeout time.Duration) bool {
	done := make(chan struct{})
	go func() {
		backgroundWork.Wait()
		close(done)
	}()
	select {
	case <-done:
		return true
	case <-time.After(timeout):
		return false
	}
}

// logDir is the directory logs and crash reports are written to.
//
// It normally sits inside the data directory the store opened. When the store
// failed to open there is no such directory, and that is exactly the failure
// worth having a log of, so fall back to the default path for the channel.
// Empty only if even that cannot be resolved.
func (a *App) logDir() string {
	if a.dataDir != "" {
		return logging.Dir(a.dataDir)
	}
	defaultPath, err := storage.DefaultPathForChannel(a.channel)
	if err != nil {
		return ""
	}
	return logging.Dir(filepath.Dir(defaultPath))
}

// logsOn reports whether file logging should be running, which is the setting
// unless --debug or PELTON_DEBUG overrode it.
func (a *App) logsOn() bool {
	return a.debug || a.boolSetting(settingLogToFile, a.channel == storage.ChannelNightly)
}

// crashLogsOn reports whether a panic should leave a file behind.
//
// A nightly defaults both this and file logging on. It already tells the user
// it is untested, and a crash report from a build nobody promised would work
// is the whole point of shipping it.
func (a *App) crashLogsOn() bool {
	return a.debug || a.boolSetting(settingCrashLogs, a.channel == storage.ChannelNightly)
}

// logLevel is the configured threshold, forced to debug by --debug.
func (a *App) logLevel() slog.Level {
	if a.debug {
		return slog.LevelDebug
	}
	return logging.ParseLevel(a.stringSetting(settingLogLevel, defaultLogLevel))
}

// applyLogSettings brings the log writer and the crash handlers in line with
// the current settings. It runs once the data directory is known and again
// after any of the logging settings change.
func (a *App) applyLogSettings() {
	dir := a.logDir()
	a.logWriter.SetLevel(a.logLevel())
	logging.SetMessageMetadata(a.boolSetting(settingLogMessageMetadata, false))
	logging.ConfigureCrashes(dir, a.version, a.channel, dir != "" && a.crashLogsOn())

	if dir == "" || !a.logsOn() {
		if err := a.logWriter.Disable(); err != nil {
			a.log.Error("stop file logging", "err", err)
		}
		return
	}
	if err := a.logWriter.Enable(dir); err != nil {
		// stderr still works, so say so there and carry on unlogged rather
		// than failing startup over a log file.
		a.log.Error("start file logging", "dir", dir, "err", err)
		return
	}
	a.log.Info("file logging on", "dir", dir, "level", logging.LevelName(a.logLevel()))
}
