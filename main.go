package main

import (
	"context"
	"fmt"
	"os"
	"os/signal"
	"sort"
	"strings"
	"syscall"

	"github.com/whitehai11/AWaN/core/config"
	"github.com/whitehai11/AWaN/core/interfaces"
	"github.com/whitehai11/AWaN/core/runtime"
	"github.com/whitehai11/AWaN/core/types"
	"github.com/whitehai11/AWaN/internal/updater"
)

func main() {
	if err := run(os.Args[1:]); err != nil {
		fmt.Fprintln(os.Stderr, "error:", err)
		os.Exit(1)
	}
}

func run(args []string) error {
	cfg, err := config.Load()
	if err != nil {
		return err
	}

	updater.StartBackground(updater.Options{
		AppName:        "AWaN Core",
		Repo:           "whitehai11/AWaN",
		Version:        Version,
		BinaryBaseName: "awan-core",
		Args:           args,
		Logger: func(message string) {
			fmt.Println("[AWAN]", message)
		},
	})

	rt, err := runtime.New(cfg)
	if err != nil {
		return err
	}

	if len(args) > 0 && args[0] == "run" {
		prompt := strings.TrimSpace(strings.Join(args[1:], " "))
		if prompt == "" {
			return fmt.Errorf("usage: awan run \"your prompt here\"")
		}

		response, err := rt.Run(types.AgentRequest{
			Agent:  cfg.DefaultAgent,
			Prompt: prompt,
		})
		if err != nil {
			return err
		}

		fmt.Println(response.Output)
		return nil
	}

	if len(args) > 0 && args[0] == "plugin" {
		return runPluginCommand(rt, args[1:])
	}

	server := interfaces.NewServer(rt)
	ctx, stop := signal.NotifyContext(context.Background(), os.Interrupt, syscall.SIGTERM)
	defer stop()

	return server.Start(ctx)
}

func runPluginCommand(rt *runtime.Runtime, args []string) error {
	if len(args) == 0 {
		return fmt.Errorf("usage: awan plugin <list|search|install|remove> [name]")
	}

	switch args[0] {
	case "list":
		installed, err := rt.InstalledPlugins()
		if err != nil {
			return err
		}
		if len(installed) == 0 {
			fmt.Println("no plugins installed")
			return nil
		}

		sort.Slice(installed, func(i, j int) bool {
			return strings.ToLower(installed[i].Name) < strings.ToLower(installed[j].Name)
		})
		for _, plugin := range installed {
			fmt.Println(plugin.Name)
		}
		return nil
	case "search":
		query := ""
		if len(args) > 1 {
			query = strings.Join(args[1:], " ")
		}

		plugins, err := rt.SearchRegistryPlugins(query)
		if err != nil {
			return err
		}
		if len(plugins) == 0 {
			fmt.Println("no plugins found")
			return nil
		}

		for _, plugin := range plugins {
			line := plugin.Name
			if plugin.Version != "" {
				line += " (" + plugin.Version + ")"
			}
			if strings.TrimSpace(plugin.Description) != "" {
				line += " - " + plugin.Description
			}
			fmt.Println(line)
		}
		return nil
	case "install":
		if len(args) < 2 || strings.TrimSpace(args[1]) == "" {
			return fmt.Errorf("usage: awan plugin install <name|github-url>")
		}

		result, err := rt.InstallPlugin(args[1])
		if err != nil {
			return err
		}
		fmt.Printf("installed %s (%s) to %s\n", result.Name, result.Type, result.Path)
		return nil
	case "remove":
		if len(args) < 2 || strings.TrimSpace(args[1]) == "" {
			return fmt.Errorf("usage: awan plugin remove <name>")
		}

		result, err := rt.RemoveInstalledPlugin(args[1])
		if err != nil {
			return err
		}
		fmt.Printf("removed %s from %s\n", result.Name, result.Path)
		return nil
	default:
		return fmt.Errorf("unknown plugin command %q", args[0])
	}
}
