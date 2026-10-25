package main

import (
	"context"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"runtime"

	"cmnet/internal/discordbot"
	"cmnet/internal/startup"
)

func discordBinaryName() string {
	if runtime.GOOS == "windows" {
		return "advisor-discord.exe"
	}
	return "advisor-discord"
}

func lookDiscordBin() (string, error) {
	name := discordBinaryName()
	if exe, err := os.Executable(); err == nil {
		p := filepath.Join(filepath.Dir(exe), name)
		if st, err := os.Stat(p); err == nil && !st.IsDir() {
			return p, nil
		}
	}
	if wd, err := os.Getwd(); err == nil {
		p := filepath.Join(wd, name)
		if st, err := os.Stat(p); err == nil && !st.IsDir() {
			return p, nil
		}
	}
	if p, err := exec.LookPath(name); err == nil {
		return p, nil
	}
	return "", fmt.Errorf("%s not found next to advisor.exe or on PATH", name)
}

// maybeStartDiscord launches advisor-discord as a sibling process when a usable
// discord.yaml and binary are present. It stays a separate process (Clash can
// PROXY Discord/NTTS while 127.0.0.1 stays DIRECT). Failures never abort advisor.
func maybeStartDiscord(ctx context.Context, explicitConfig string) {
	cfgPath := discordbot.FindConfig(explicitConfig)
	if _, err := os.Stat(cfgPath); err != nil {
		return
	}
	if _, err := discordbot.LoadConfig(cfgPath); err != nil {
		startup.Warnf("advisor-discord not started: %v", err)
		return
	}
	bin, err := lookDiscordBin()
	if err != nil {
		startup.Warnf("advisor-discord not started: %v", err)
		return
	}
	cmd := exec.CommandContext(ctx, bin, "--config", cfgPath)
	cmd.Stdout = os.Stderr
	cmd.Stderr = os.Stderr
	if err := cmd.Start(); err != nil {
		startup.Warnf("advisor-discord not started: %v", err)
		return
	}
	startup.Infof("advisor-discord started (pid %d, config %s)", cmd.Process.Pid, cfgPath)
	go func() {
		err := cmd.Wait()
		if ctx.Err() != nil {
			return
		}
		if err != nil {
			startup.Warnf("advisor-discord exited: %v", err)
			return
		}
		startup.Warnf("advisor-discord exited unexpectedly")
	}()
}
