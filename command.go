package main

import (
	"context"
	"fmt"
	"io"
	"slices"
	"strings"
)

// command is one subcommand.
type command struct {
	// name is one or more words, e.g. "sourcemaps upload".
	name string
	// usage is a one-line description, shown by "help".
	usage string
	// run receives the arguments after the name.
	run func(ctx context.Context, args []string, out io.Writer) error
}

// dispatch runs the command named by the leading args and reports whether
// args named one ("help" lists them). With no args, or args that name no
// command, it returns false.
func dispatch(ctx context.Context, args []string, out io.Writer, cmds ...command) (bool, error) {
	if len(args) == 0 {
		return false, nil
	}
	if args[0] == "help" || args[0] == "-h" || args[0] == "--help" {
		sorted := slices.Clone(cmds)
		slices.SortFunc(sorted, func(a, b command) int { return strings.Compare(a.name, b.name) })
		for _, c := range sorted {
			_, _ = fmt.Fprintf(out, "  %-20s %s\n", c.name, c.usage)
		}
		return true, nil
	}
	var best *command
	for i, c := range cmds {
		words := strings.Fields(c.name)
		if len(words) <= len(args) && slices.Equal(words, args[:len(words)]) &&
			(best == nil || len(words) > len(strings.Fields(best.name))) {
			best = &cmds[i]
		}
	}
	if best == nil {
		return false, nil
	}
	return true, best.run(ctx, args[len(strings.Fields(best.name)):], out)
}
