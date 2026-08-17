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

func TestPDFPreviewUsesResizableSplitAndBlobRefresh(t *testing.T) {
	index, err := Files.ReadFile("index.html")
	if err != nil {
		t.Fatal(err)
	}
	styles, err := Files.ReadFile("styles.css")
	if err != nil {
		t.Fatal(err)
	}
	app, err := Files.ReadFile("app.js")
	if err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(string(index), `id="editor-layout"`) || !strings.Contains(string(index), `id="pdf-resizer"`) {
		t.Fatal("PDF preview must have a dedicated split layout and resize handle")
	}
	if !strings.Contains(string(styles), ".editor-layout.has-pdf") || !strings.Contains(string(styles), ".pdf-panel { height: 100%") {
		t.Fatal("PDF preview must use a full-height editor/preview split")
	}
	if !strings.Contains(string(app), "localPDFURL(cv, signature)") || !strings.Contains(string(app), "refreshPDF(true)") {
		t.Fatal("PDF preview must reload the versioned local file after an ERP update")
	}
	if strings.Contains(string(app), "toolbar=0") {
		t.Fatal("embedded PDF preview must keep the browser PDF toolbar")
	}
}
