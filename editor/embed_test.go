package editor

import (
	"strings"
	"testing"
)

func TestHiddenElementsStayHidden(t *testing.T) {
	styles, err := Files.ReadFile("styles.css")
	if err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(string(styles), "[hidden] { display: none !important; }") {
		t.Fatal("styles must preserve the hidden attribute even when a component sets display")
	}
}

func TestSetupFieldsDoNotUseEditorGridColumns(t *testing.T) {
	styles, err := Files.ReadFile("styles.css")
	if err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(string(styles), ".setup-card .field { grid-column: auto; min-width: 0; }") {
		t.Fatal("setup fields must opt out of the editor's 12-column field layout")
	}
}
