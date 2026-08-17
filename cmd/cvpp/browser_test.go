package main

import (
	"reflect"
	"testing"
)

func TestBrowserCommand(t *testing.T) {
	url := "https://erp.example/IIT_ERP3/?ssoToken=secret"
	tests := []struct {
		goos    string
		command string
		args    []string
	}{
		{
			goos:    "darwin",
			command: "/usr/bin/open",
			args:    []string{url},
		},
		{goos: "linux", command: "xdg-open", args: []string{url}},
		{goos: "windows", command: "rundll32", args: []string{"url.dll,FileProtocolHandler", url}},
	}
	for _, test := range tests {
		command, args, err := browserCommand(test.goos, url)
		if err != nil {
			t.Fatalf("browserCommand(%s): %v", test.goos, err)
		}
		if command != test.command || !reflect.DeepEqual(args, test.args) {
			t.Fatalf("browserCommand(%s) = %q %v", test.goos, command, args)
		}
	}
}

func TestBrowserCommandRejectsUnsupportedOS(t *testing.T) {
	if _, _, err := browserCommand("plan9", "https://erp.example"); err == nil {
		t.Fatal("unsupported OS was accepted")
	}
}

func TestEditorAppCommandsUseStandaloneWindow(t *testing.T) {
	url := "http://127.0.0.1:1234/bootstrap?token=one-time"
	for _, goos := range []string{"darwin", "linux", "windows"} {
		commands := editorAppCommands(goos, url)
		if len(commands) == 0 {
			t.Fatalf("no app-window command for %s", goos)
		}
		found := false
		for _, command := range commands {
			for _, argument := range command.args {
				if argument == "--app="+url {
					found = true
				}
			}
		}
		if !found {
			t.Fatalf("%s commands do not use browser app mode: %#v", goos, commands)
		}
	}
}
