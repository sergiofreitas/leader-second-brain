// Package configs embeds the configuration profiles shipped with the binary,
// so `second-brain init --profile <name>` works without the repository.
package configs

import (
	"embed"
	"fmt"
	"sort"
	"strings"
)

//go:embed profiles/*.yaml
var profiles embed.FS

// Profiles returns the names of the embedded profiles, sorted
func Profiles() []string {
	entries, _ := profiles.ReadDir("profiles")
	names := make([]string, 0, len(entries))
	for _, e := range entries {
		names = append(names, strings.TrimSuffix(e.Name(), ".yaml"))
	}
	sort.Strings(names)
	return names
}

// Profile returns the YAML of an embedded profile
func Profile(name string) ([]byte, error) {
	data, err := profiles.ReadFile("profiles/" + name + ".yaml")
	if err != nil {
		return nil, fmt.Errorf("unknown profile %q (available: %s)", name, strings.Join(Profiles(), ", "))
	}
	return data, nil
}
