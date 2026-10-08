package main

import (
	"flag"
	"fmt"
	"os"
	"runtime"
	"strings"

	"github.com/second-brain/second-brain/packages/mcp-server/configs"
	"github.com/second-brain/second-brain/packages/mcp-server/internal/config"
	"github.com/second-brain/second-brain/packages/mcp-server/internal/setup"
)

// runInit implements `second-brain init` and returns the exit code
func runInit(args []string) int {
	flags := flag.NewFlagSet("second-brain init", flag.ContinueOnError)
	profile := flags.String("profile", "default", "built-in profile: "+strings.Join(configs.Profiles(), ", "))
	embedding := flags.String("embedding", "", "replace the profile's semantic search provider: "+strings.Join(setup.EmbeddingPresets, ", "))
	path := flags.String("config", config.DefaultConfigPath(), "where to write the config")
	force := flags.Bool("force", false, "overwrite an existing config")
	flags.Usage = func() {
		fmt.Fprint(flags.Output(), `Write a config file from a built-in profile.

Usage:
  second-brain init [--profile NAME] [--embedding PROVIDER] [--config PATH] [--force]

Examples:
  second-brain init                                    # defaults: keyword search, nothing sent anywhere
  second-brain init --profile saipos                   # Saipos: stop/start/continue, LiteLLM gateway
  second-brain init --profile saipos --embedding openai

Options:
`)
		flags.PrintDefaults()
	}
	if err := flags.Parse(args); err != nil {
		if err == flag.ErrHelp {
			return 0
		}
		return 2
	}

	res, err := setup.Init(setup.InitOptions{
		Profile: *profile, Embedding: *embedding, Path: *path, Force: *force,
	})
	if err != nil {
		fmt.Fprintln(os.Stderr, "second-brain init:", err)
		return 1
	}

	fmt.Printf("Config written to %s (profile %s", res.Path, *profile)
	if *embedding != "" {
		fmt.Printf(", embedding %s", *embedding)
	}
	fmt.Println(")")

	if len(res.EnvVars) > 0 {
		fmt.Println("\nThis config reads these environment variables:")
		missing := false
		for _, name := range res.SortedVars() {
			state := "set"
			if !res.EnvVars[name] {
				state, missing = "NOT SET", true
			}
			fmt.Printf("  %-32s %s\n", name, state)
		}
		if missing {
			fmt.Println("\nSet them where your MCP host (Claude Code, Codex...) can see them, then restart it:")
			if runtime.GOOS == "windows" {
				fmt.Println(`  setx NAME "value"        (applies to apps opened afterwards)`)
			} else {
				fmt.Println(`  export NAME="value"      (in your shell profile, e.g. ~/.zshrc)`)
			}
		}
	}
	fmt.Println("\nThe MCP host starts the server with this config; restart the host to apply changes.")
	return 0
}
