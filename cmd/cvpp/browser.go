package main

import (
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
)

type browserLaunch struct {
	command string
	args    []string
	wait    bool
}

func openBrowser(goos, url string) error {
	command, args, err := browserCommand(goos, url)
	if err != nil {
		return err
	}
	if err := exec.Command(command, args...).Run(); err != nil {
		return err
	}
	return nil
}

func openEditorBrowser(goos, url string) error {
	var lastErr error
	for _, launch := range editorAppCommands(goos, url) {
		command := exec.Command(launch.command, launch.args...)
		if launch.wait {
			if err := command.Run(); err == nil {
				return nil
			} else {
				lastErr = err
			}
			continue
		}
		if err := command.Start(); err != nil {
			lastErr = err
			continue
		}
		_ = command.Process.Release()
		return nil
	}
	if err := openBrowser(goos, url); err != nil {
		if lastErr != nil {
			return fmt.Errorf("open standalone app window: %v; browser fallback: %w", lastErr, err)
		}
		return err
	}
	return nil
}

func browserCommand(goos, url string) (string, []string, error) {
	switch goos {
	case "darwin":
		return "/usr/bin/open", []string{url}, nil
	case "linux":
		return "xdg-open", []string{url}, nil
	case "windows":
		return "rundll32", []string{"url.dll,FileProtocolHandler", url}, nil
	default:
		return "", nil, fmt.Errorf("opening a browser is unsupported on %s", goos)
	}
}

func editorAppCommands(goos, url string) []browserLaunch {
	appArg := "--app=" + url
	switch goos {
	case "darwin":
		return []browserLaunch{
			{command: "/usr/bin/open", args: []string{"-n", "-a", "Microsoft Edge", "--args", appArg}, wait: true},
			{command: "/usr/bin/open", args: []string{"-n", "-a", "Google Chrome", "--args", appArg}, wait: true},
			{command: "/usr/bin/open", args: []string{"-n", "-a", "Chromium", "--args", appArg}, wait: true},
		}
	case "linux":
		commands := []string{"microsoft-edge", "microsoft-edge-stable", "google-chrome", "google-chrome-stable", "chromium", "chromium-browser"}
		launches := make([]browserLaunch, 0, len(commands))
		for _, command := range commands {
			launches = append(launches, browserLaunch{command: command, args: []string{appArg}})
		}
		return launches
	case "windows":
		commands := []string{"msedge.exe", "chrome.exe"}
		for _, root := range []string{os.Getenv("PROGRAMFILES(X86)"), os.Getenv("PROGRAMFILES"), os.Getenv("LOCALAPPDATA")} {
			if root == "" {
				continue
			}
			commands = append(commands,
				filepath.Join(root, "Microsoft", "Edge", "Application", "msedge.exe"),
				filepath.Join(root, "Google", "Chrome", "Application", "chrome.exe"),
			)
		}
		launches := make([]browserLaunch, 0, len(commands))
		for _, command := range commands {
			launches = append(launches, browserLaunch{command: command, args: []string{appArg}})
		}
		return launches
	default:
		return nil
	}
}
