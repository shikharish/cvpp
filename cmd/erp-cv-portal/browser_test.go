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
