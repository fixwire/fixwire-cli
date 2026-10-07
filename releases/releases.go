// Package releases tells Fixwire what a release was built from, so the AI
// debugger can read the source at that commit and the diff from the
// previous release (when the project links its repository).
package releases

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net/http"
	"net/url"
	"os/exec"
	"strings"
	"time"
)

// Options say what to set and where.
type Options struct {
	// URL is the Fixwire API, e.g. https://api.fixwire.example.
	URL string
	// Token is an API key with the releases:write scope.
	Token string
	// Org is the organization slug (any value works: the key decides).
	Org string
	// Projects are slugs; a project API key needs none.
	Projects []string
	// Release is the version the SDKs report.
	Release string
	// Commit and Repository default to the working directory's HEAD and
	// origin remote.
	Commit, Repository string
	HTTP               *http.Client
}

// Result is what Fixwire recorded.
type Result struct {
	Version    string `json:"version"`
	Commit     string `json:"commit"`
	Repository string `json:"repository"`
	Projects   int    `json:"projects"`
}

// SetCommit records the release's commit.
func SetCommit(ctx context.Context, o Options) (Result, error) {
	if o.URL == "" || o.Token == "" {
		return Result{}, errors.New("releases: set the Fixwire URL and an API key with the releases:write scope")
	}
	if o.Release == "" {
		return Result{}, errors.New("releases: name the release (--release)")
	}
	if o.Commit == "" {
		head, err := git(ctx, "rev-parse", "HEAD")
		if err != nil {
			return Result{}, fmt.Errorf("releases: no --commit and no git HEAD here: %w", err)
		}
		o.Commit = head
	}
	if o.Repository == "" {
		if remote, err := git(ctx, "remote", "get-url", "origin"); err == nil {
			o.Repository = RepositoryOf(remote)
		}
	}
	if o.Org == "" {
		o.Org = "-"
	}
	if o.HTTP == nil {
		o.HTTP = &http.Client{Timeout: 30 * time.Second}
	}
	body, _ := json.Marshal(map[string]any{
		"projects": o.Projects,
		"refs":     []map[string]string{{"repository": o.Repository, "commit": o.Commit}},
	})
	target := strings.TrimRight(o.URL, "/") + "/api/0/organizations/" + url.PathEscape(o.Org) + "/releases/" + url.PathEscape(o.Release) + "/"
	req, err := http.NewRequestWithContext(ctx, http.MethodPut, target, bytes.NewReader(body))
	if err != nil {
		return Result{}, err
	}
	req.Header.Set("Authorization", "Bearer "+o.Token)
	req.Header.Set("Content-Type", "application/json")
	req.Header.Set("User-Agent", "fixwire-cli")
	res, err := o.HTTP.Do(req)
	if err != nil {
		return Result{}, err
	}
	defer func() { _ = res.Body.Close() }()
	raw, _ := io.ReadAll(io.LimitReader(res.Body, 1<<20))
	if res.StatusCode/100 != 2 {
		var e struct {
			Error struct {
				Message string `json:"message"`
			} `json:"error"`
		}
		msg := strings.TrimSpace(string(raw))
		if json.Unmarshal(raw, &e) == nil && e.Error.Message != "" {
			msg = e.Error.Message
		}
		return Result{}, fmt.Errorf("releases: %d %s", res.StatusCode, msg)
	}
	var out Result
	return out, json.Unmarshal(raw, &out)
}

// RepositoryOf turns a git remote URL into owner/name: git@github.com:a/b.git,
// https://github.com/a/b, ssh://git@host/a/b.git. Unknown forms give "".
func RepositoryOf(remote string) string {
	r := strings.TrimSpace(remote)
	r = strings.TrimSuffix(strings.TrimSuffix(r, "/"), ".git")
	switch {
	case strings.Contains(r, "://"):
		u, err := url.Parse(r)
		if err != nil {
			return ""
		}
		r = strings.Trim(u.Path, "/")
	case strings.Contains(r, ":"):
		r = r[strings.Index(r, ":")+1:]
	default:
		return ""
	}
	parts := strings.Split(r, "/")
	if len(parts) < 2 || parts[len(parts)-2] == "" || parts[len(parts)-1] == "" {
		return ""
	}
	return parts[len(parts)-2] + "/" + parts[len(parts)-1]
}

func git(ctx context.Context, args ...string) (string, error) {
	out, err := exec.CommandContext(ctx, "git", args...).Output()
	return strings.TrimSpace(string(out)), err
}
