// Command fixwire-cli prepares and uploads a JavaScript build's source maps
// so Fixwire shows original code in stack traces.
//
//	fixwire-cli sourcemaps inject dist
//	fixwire-cli sourcemaps upload dist --release web@1.4.0
//
// Settings come from flags or the environment: FIXWIRE_URL,
// FIXWIRE_AUTH_TOKEN (an API key with the artifacts:write scope),
// FIXWIRE_ORG, FIXWIRE_PROJECT and FIXWIRE_RELEASE.
package main

import (
	"cmp"
	"context"
	"errors"
	"flag"
	"fmt"
	"io"
	"os"
	"os/signal"
	"strings"

	"github.com/fixwire/fixwire-cli/releases"
	"github.com/fixwire/fixwire-cli/sourcemaps"
)

// version is set at build time.
var version = "dev"

func main() {
	ctx, stop := signal.NotifyContext(context.Background(), os.Interrupt)
	defer stop()
	if err := run(ctx, os.Args[1:], os.Stdout, os.Getenv); err != nil {
		fmt.Fprintln(os.Stderr, "fixwire-cli:", err)
		stop()
		os.Exit(1)
	}
}

func run(ctx context.Context, args []string, out io.Writer, env func(string) string) error {
	handled, err := dispatch(ctx, args, out, commands(env)...)
	if !handled {
		_, _ = fmt.Fprintf(out, "fixwire-cli %s\n\nCommands:\n", version)
		_, _ = dispatch(ctx, []string{"help"}, out, commands(env)...)
		if len(args) > 0 {
			return fmt.Errorf("unknown command %q", strings.Join(args, " "))
		}
	}
	return err
}

func commands(env func(string) string) []command {
	setting := func(name string) string { return env("FIXWIRE_" + name) }
	return []command{
		{
			name:  "sourcemaps inject",
			usage: "Stamp each JavaScript file with a source map in <dir> with a debug id",
			run: func(_ context.Context, args []string, out io.Writer) error {
				fs := flag.NewFlagSet("sourcemaps inject", flag.ContinueOnError)
				fs.SetOutput(out)
				if err := fs.Parse(args); err != nil {
					return err
				}
				dir, err := oneDir(fs)
				if err != nil {
					return err
				}
				files, err := sourcemaps.Inject(dir)
				if err != nil {
					return err
				}
				for _, f := range files {
					state := "injected"
					if f.Already {
						state = "already injected"
					}
					_, _ = fmt.Fprintf(out, "%s  %s (%s)\n", f.DebugID, f.File, state)
				}
				_, _ = fmt.Fprintf(out, "%d files with source maps\n", len(files))
				return nil
			},
		},
		{
			name:  "sourcemaps upload",
			usage: "Upload the JavaScript files and source maps in <dir> (--inject stamps them first)",
			run: func(ctx context.Context, args []string, out io.Writer) error {
				fs := flag.NewFlagSet("sourcemaps upload", flag.ContinueOnError)
				fs.SetOutput(out)
				o := sourcemaps.Options{}
				fs.StringVar(&o.URL, "url", setting("URL"), "Fixwire API URL (FIXWIRE_URL)")
				// Not the flag's default: usage text prints defaults.
				fs.StringVar(&o.Token, "auth-token", "", "API key with artifacts:write (FIXWIRE_AUTH_TOKEN)")
				fs.StringVar(&o.Org, "org", setting("ORG"), "organization slug (FIXWIRE_ORG); the key decides")
				project := fs.String("project", setting("PROJECT"), "project slug (FIXWIRE_PROJECT); optional, a key writes only to its own project")
				fs.StringVar(&o.Release, "release", setting("RELEASE"), "release (FIXWIRE_RELEASE), for SDKs without debug ids")
				fs.StringVar(&o.Dist, "dist", "", "distribution within the release")
				fs.StringVar(&o.URLPrefix, "url-prefix", "~/", "URL the directory is served under; ~/ matches any host")
				inject := fs.Bool("inject", false, "stamp debug ids first")
				if err := fs.Parse(args); err != nil {
					return err
				}
				o.Token = cmp.Or(o.Token, setting("AUTH_TOKEN"))
				dir, err := oneDir(fs)
				if err != nil {
					return err
				}
				for _, p := range strings.Split(*project, ",") {
					if p = strings.TrimSpace(p); p != "" {
						o.Projects = append(o.Projects, p)
					}
				}
				if *inject {
					if _, err := sourcemaps.Inject(dir); err != nil {
						return err
					}
				}
				res, err := sourcemaps.Upload(ctx, dir, o)
				if err != nil {
					return err
				}
				_, _ = fmt.Fprintf(out, "uploaded %d files (%d with debug ids), %d KiB bundle %s, %d new chunks\n",
					res.Files, res.DebugIDs, res.Bytes>>10, res.Checksum[:12], res.Uploaded)
				if res.DebugIDs == 0 && o.Release == "" {
					_, _ = fmt.Fprintln(out, "warning: no debug ids and no --release: run `fixwire-cli sourcemaps inject` before deploying, or pass --release")
				}
				return nil
			},
		},
		{
			name:  "releases set-commit",
			usage: "Record the commit a release was built from (default: git HEAD and the origin remote)",
			run: func(ctx context.Context, args []string, out io.Writer) error {
				fs := flag.NewFlagSet("releases set-commit", flag.ContinueOnError)
				fs.SetOutput(out)
				o := releases.Options{}
				fs.StringVar(&o.URL, "url", setting("URL"), "Fixwire API URL (FIXWIRE_URL)")
				fs.StringVar(&o.Token, "auth-token", "", "API key with releases:write (FIXWIRE_AUTH_TOKEN)")
				fs.StringVar(&o.Org, "org", setting("ORG"), "organization slug (FIXWIRE_ORG); the key decides")
				project := fs.String("project", setting("PROJECT"), "project slug (FIXWIRE_PROJECT); optional, a key writes only to its own project")
				fs.StringVar(&o.Release, "release", setting("RELEASE"), "release (FIXWIRE_RELEASE), as the SDKs report it")
				fs.StringVar(&o.Commit, "commit", "", "commit SHA (default: git rev-parse HEAD)")
				fs.StringVar(&o.Repository, "repository", "", "owner/name (default: from the origin remote)")
				if err := fs.Parse(args); err != nil {
					return err
				}
				o.Token = cmp.Or(o.Token, setting("AUTH_TOKEN"))
				for _, p := range strings.Split(*project, ",") {
					if p = strings.TrimSpace(p); p != "" {
						o.Projects = append(o.Projects, p)
					}
				}
				res, err := releases.SetCommit(ctx, o)
				if err != nil {
					return err
				}
				_, _ = fmt.Fprintf(out, "release %s: commit %s %s (%d projects)\n", res.Version, res.Commit, res.Repository, res.Projects)
				return nil
			},
		},
	}
}

func oneDir(fs *flag.FlagSet) (string, error) {
	if fs.NArg() != 1 {
		return "", errors.New("name one build directory, e.g. dist")
	}
	info, err := os.Stat(fs.Arg(0))
	if err != nil {
		return "", err
	}
	if !info.IsDir() {
		return "", fmt.Errorf("%s is not a directory", fs.Arg(0))
	}
	return fs.Arg(0), nil
}
