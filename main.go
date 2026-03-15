package main

import (
	"context"
	"fmt"
	"os"
	"os/signal"
	"strings"
	"syscall"

	"github.com/awan/awan/core/config"
	"github.com/awan/awan/core/interfaces"
	"github.com/awan/awan/core/runtime"
	"github.com/awan/awan/core/types"
	"github.com/awan/awan/internal/updater"
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
		Repo:           "awan/core",
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

	server := interfaces.NewServer(rt)
	ctx, stop := signal.NotifyContext(context.Background(), os.Interrupt, syscall.SIGTERM)
	defer stop()

	return server.Start(ctx)
}
