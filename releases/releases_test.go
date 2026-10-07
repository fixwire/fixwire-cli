package releases_test

import (
	"testing"

	"github.com/fixwire/fixwire-cli/releases"
)

func TestRepositoryOf(t *testing.T) {
	for remote, want := range map[string]string{
		"git@github.com:acme/shop.git":          "acme/shop",
		"https://github.com/acme/shop":          "acme/shop",
		"https://github.com/acme/shop.git/":     "acme/shop",
		"ssh://git@github.com/acme/shop.git":    "acme/shop",
		"https://gitlab.example/group/sub/shop": "sub/shop",
		"/srv/repos/shop":                       "",
		"":                                      "",
	} {
		if got := releases.RepositoryOf(remote); got != want {
			t.Errorf("RepositoryOf(%q) = %q, want %q", remote, got, want)
		}
	}
}
