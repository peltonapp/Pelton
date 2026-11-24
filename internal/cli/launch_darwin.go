package cli

import (
	"os/exec"
	"strings"
)

// launcher opens the app bundle the command runs from, or else the
// installed one by its bundle id. A mailto: link goes along as a document,
// which macOS hands to the app as an Apple event.
func launcher(app, channel string) func([]string) error {
	return func(args []string) error {
		target := []string{"-b", "sh.arne.pelton"}
		if channel != "" {
			target[1] += "." + channel
		}
		if i := strings.Index(app, ".app/Contents/MacOS/"); i >= 0 {
			target = []string{"-a", app[:i+len(".app")]}
		}
		return exec.Command("open", append(target, args...)...).Run()
	}
}
