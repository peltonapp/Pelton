package cli

import (
	"fmt"
	"strings"
)

const exitHelp = `Exit codes:
  0  done
  1  error
  2  wrong arguments
  3  Pelton is not running
  4  command line access is off, or the token was refused
  5  the switch this command needs is off
  6  Pelton is locked
  7  not found
  8  refused, or not confirmed in Pelton`

func cmdHelp(e *env, args []string) error {
	if len(args) > 0 {
		c, ok := lookup(args[0])
		if !ok {
			return usageErr("unknown command %q", args[0])
		}
		e.printf("usage: pelton %s\n\n%s\n", c.usage, c.summary)
		if c.detail != "" {
			e.printf("\n%s\n", c.detail)
		}
		return nil
	}
	e.printf("pelton talks to the running Pelton app, so mail can be read and handled from a shell.\n")
	e.printf("Turn it on in Pelton under Settings, Backup, Command line access; every capability is\na switch of its own there, and every request is logged.\n\n")
	e.printf("usage: pelton COMMAND [ARGUMENTS] [--json]\n\nCommands:\n")
	width := 0
	for _, c := range commands {
		width = max(width, len(c.name))
	}
	for _, c := range commands {
		e.printf("  %-*s  %s\n", width, c.name, c.summary)
	}
	e.printf("\n--json prints the result as JSON with stable field names.\n")
	e.printf("Run `pelton help COMMAND` for the flags of one command.\n\n%s\n", exitHelp)
	return nil
}

func cmdVersion(e *env, _ []string) error {
	if e.json {
		return e.printJSON(map[string]any{"version": e.opts.Version, "protocol": 1})
	}
	e.printf("pelton %s\n", e.opts.Version)
	return nil
}

func cmdCompletion(e *env, args []string) error {
	if len(args) != 1 {
		return usageErr("usage: pelton completion bash|zsh|fish")
	}
	names := make([]string, 0, len(commands))
	for _, c := range commands {
		names = append(names, c.name)
	}
	words := strings.Join(names, " ")
	switch args[0] {
	case "bash":
		e.printf(`_pelton() {
  if [ "$COMP_CWORD" -eq 1 ]; then
    COMPREPLY=($(compgen -W "%s" -- "${COMP_WORDS[1]}"))
  else
    COMPREPLY=($(compgen -f -- "${COMP_WORDS[COMP_CWORD]}"))
  fi
}
complete -F _pelton pelton
`, words)
	case "zsh":
		e.printf(`#compdef pelton
_pelton() {
  if (( CURRENT == 2 )); then
    compadd %s
  else
    _files
  fi
}
compdef _pelton pelton
`, words)
	case "fish":
		for _, c := range commands {
			e.printf("complete -c pelton -n __fish_use_subcommand -f -a %s -d %s\n", c.name, fishQuote(c.summary))
		}
	default:
		return usageErr("no completion for %q; pick bash, zsh or fish", args[0])
	}
	return nil
}

func fishQuote(s string) string {
	return fmt.Sprintf("'%s'", strings.ReplaceAll(strings.ReplaceAll(s, `\`, `\\`), "'", `\'`))
}
