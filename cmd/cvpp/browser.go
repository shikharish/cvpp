package main

import (
	"fmt"
	"os/exec"
)

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
	command, args, err := editorBrowserCommand(goos, url)
	if err != nil {
		return err
	}
	if err := exec.Command(command, args...).Run(); err != nil {
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

func editorBrowserCommand(goos, url string) (string, []string, error) {
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
