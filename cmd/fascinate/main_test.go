package main

import "testing"

func TestRequiresServerConfig(t *testing.T) {
	t.Parallel()

	for _, command := range []string{"", "serve", "migrate", "runtime-machines", "netns-forward"} {
		if !requiresServerConfig(command) {
			t.Fatalf("expected %q to require server config", command)
		}
	}
	for _, command := range []string{"login", "logout", "whoami", "version"} {
		if requiresServerConfig(command) {
			t.Fatalf("expected %q to avoid server config", command)
		}
	}
}
