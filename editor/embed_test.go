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

func TestExpiredSessionPromptsOnlyForOTP(t *testing.T) {
	index, err := Files.ReadFile("index.html")
	if err != nil {
		t.Fatal(err)
	}
	app, err := Files.ReadFile("app.js")
	if err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(string(index), `id="saved-login-panel"`) {
		t.Fatal("expired-session dialog must explain that saved login details are being reused")
	}
	if !strings.Contains(string(app), "function showSavedLogin(show, onboarding)") || !strings.Contains(string(app), "showSavedLogin(canReuseSavedLogin, savedLoginIsOnboarding)") {
		t.Fatal("expired-session flow must hide the login form and show only the OTP prompt")
	}
}

func TestOnboardingAutomaticallyReusesSavedCredentials(t *testing.T) {
	app, err := Files.ReadFile("app.js")
	if err != nil {
		t.Fatal(err)
	}
	content := string(app)
	if !strings.Contains(content, "async function importWithSavedCredentials()") {
		t.Fatal("onboarding must be able to import with backend-saved credentials")
	}
	if !strings.Contains(content, "status.onboarding && canReuseSavedLogin") || !strings.Contains(content, "await importWithSavedCredentials()") {
		t.Fatal("first start with saved credentials must begin import automatically")
	}
	if !strings.Contains(content, `JSON.stringify({ freshLogin: false })`) {
		t.Fatal("saved-credential onboarding must reuse a valid existing ERP session when available")
	}
}

func TestUnknownSecurityQuestionCanBeAnsweredWithoutReenteringLogin(t *testing.T) {
	index, err := Files.ReadFile("index.html")
	if err != nil {
		t.Fatal(err)
	}
	app, err := Files.ReadFile("app.js")
	if err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(string(index), `id="security-answer-panel"`) || !strings.Contains(string(index), `id="security-answer-form"`) {
		t.Fatal("unknown security questions must use a dedicated answer-only form")
	}
	if !strings.Contains(string(app), `apiFetch("/api/erp/security-answer"`) || !strings.Contains(string(app), "status.securityAnswerRequired") {
		t.Fatal("answer-only form must resume the waiting ERP login")
	}
}

func TestCurrentCGPAIsReadOnly(t *testing.T) {
	app, err := Files.ReadFile("app.js")
	if err != nil {
		t.Fatal(err)
	}
	content := string(app)
	if !strings.Contains(content, `readonly aria-readonly="true" title="Fetched from the ERP student profile"`) {
		t.Fatal("current CGPA must be rendered as an ERP-managed read-only field")
	}
	if !strings.Contains(content, "if (!current) {") {
		t.Fatal("current CGPA controls must not bind editing handlers")
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

func TestDefaultAndInlineFontSizesReachPortalOutput(t *testing.T) {
	index, err := Files.ReadFile("index.html")
	if err != nil {
		t.Fatal(err)
	}
	core, err := Files.ReadFile("core.js")
	if err != nil {
		t.Fatal(err)
	}
	app, err := Files.ReadFile("app.js")
	if err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(string(index), `id="default-font-size"`) {
		t.Fatal("editor must expose the persisted default PDF font size")
	}
	if !strings.Contains(string(core), "defaultFontSize: normalizeFontSize(source.defaultFontSize)") || !strings.Contains(string(core), "entrySubjectHtml(entry, data.defaultFontSize)") {
		t.Fatal("default font size must be normalized and used for portal entry output")
	}
	if strings.Contains(string(core), `querySelectorAll("span, a")`) {
		t.Fatal("portal formatting must not discard custom-size spans")
	}
	if !strings.Contains(string(app), "state.defaultFontSize = Number(event.target.value)") {
		t.Fatal("default font size control must update resume state")
	}
}

func TestEntryDateFieldReachesCalculatedPortalHeading(t *testing.T) {
	core, err := Files.ReadFile("core.js")
	if err != nil {
		t.Fatal(err)
	}
	app, err := Files.ReadFile("app.js")
	if err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(string(core), "date: text(entry.date)") || !strings.Contains(string(core), "headingSpacerCount(overview, date, defaultFontSize)") {
		t.Fatal("entry dates must be normalized and spaced in portal headings")
	}
	if !strings.Contains(string(app), `class="input entry-date"`) || !strings.Contains(string(app), `.date = input.value`) {
		t.Fatal("editor must expose and persist the free-form entry date")
	}
}

func TestQuitReportsERPLogoutFailure(t *testing.T) {
	app, err := Files.ReadFile("app.js")
	if err != nil {
		t.Fatal(err)
	}
	content := string(app)
	if !strings.Contains(content, "payload.logoutError") || !strings.Contains(content, "closed with an ERP logout warning") {
		t.Fatal("quit flow must show an ERP logout failure while allowing CV++ to close")
	}
}
