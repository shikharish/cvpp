package resumedata

import (
	"net/url"
	"path/filepath"
	"strings"
	"testing"
)

func TestPortalFormatting(t *testing.T) {
	html := BlocksHTML([]Block{
		{Kind: "bullet", HTML: `Temporarily omitted.`, Hidden: true},
		{Kind: "bullet", HTML: `Added <a href="https://example.com"><strong>fast math</strong></a> with <em>care</em>: <span style="font-size: 12px;">12.7 → 7.2 ns</span> at 9.4× throughput.`},
	})
	want := `<ul><li><span style="font-size: 10px;">&nbsp;Added <strong>fast math</strong> with <em>care</em>: <span style="font-size: 12px;">12.7 to 7.2 ns</span> at 9.4x throughput.</span></li></ul>`
	if html != want {
		t.Fatalf("BlocksHTML()\n got %s\nwant %s", html, want)
	}
	if strings.Contains(html, "href") {
		t.Fatal("portal formatting retained an unsafe link")
	}
	if strings.Contains(html, "Temporarily omitted") {
		t.Fatal("portal formatting included a hidden detail block")
	}
}

func TestPortalFormattingNormalizesPastedApostrophes(t *testing.T) {
	html := BlocksHTML([]Block{{Kind: "bullet", HTML: `We’re building O’Connor’s tool with it’s copied text.`}})
	want := `<ul><li><span style="font-size: 10px;">&nbsp;We&#39;re building O&#39;Connor&#39;s tool with it&#39;s copied text.</span></li></ul>`
	if html != want {
		t.Fatalf("BlocksHTML()\n got %s\nwant %s", html, want)
	}
}

func TestResumeDefaultFontSizeControlsPortalOutput(t *testing.T) {
	resume := Resume{
		SchemaVersion:   1,
		DefaultFontSize: 12,
		Entries: []Entry{{
			ID:        "sized-entry",
			Type:      "Experience",
			Details:   []Block{{Kind: "bullet", HTML: `Default <span style="font-size: 8px;">small</span>`}},
			IncludeIn: []string{"cv1"},
		}},
		Variants: []Variant{{ID: "cv1"}, {ID: "cv2"}, {ID: "cv3"}},
	}
	if err := resume.Validate(); err != nil {
		t.Fatal(err)
	}
	want := `<ul><li><span style="font-size: 12px;">&nbsp;Default <span style="font-size: 8px;">small</span></span></li></ul>`
	if got := resume.PortalFields()["subject7"]; got != want {
		t.Fatalf("subject7\n got %s\nwant %s", got, want)
	}
}

func TestEntryDateUsesCalculatedHeadingSpacing(t *testing.T) {
	entry := Entry{
		ID:        "dated-entry",
		Type:      "Experience",
		Overview:  "Example Company | Software Engineer",
		Date:      "May 2026 - Jul 2026",
		Details:   []Block{{Kind: "bullet", HTML: "Built a system."}},
		IncludeIn: []string{"cv1"},
	}
	spaces := headingSpacerCount(entry.Overview, entry.Date, PortalFontSize)
	if spaces <= minHeadingSpaces {
		t.Fatalf("heading spacer count = %d, want a calculated gap", spaces)
	}
	target := PortalHeadingWidth * 1000 / PortalFontSize
	used := headingTextWidth(entry.Overview) + spaces*headingRuneWidth(' ') + headingTextWidth(entry.Date)
	if remaining := target - used; remaining < 0 || remaining >= headingRuneWidth(' ') {
		t.Fatalf("heading width remainder = %d, want 0-%d units", remaining, headingRuneWidth(' ')-1)
	}

	subject := EntrySubjectHTML(entry)
	gap := strings.Repeat("&nbsp;", spaces)
	if !strings.Contains(subject, `<strong>`+entry.Overview+gap+entry.Date+`</strong>`) {
		t.Fatalf("entry date was not appended with calculated spacing: %s", subject)
	}
}

func TestEntryDateSurvivesPortalRoundTrip(t *testing.T) {
	entry := Entry{
		ID:        "dated-entry",
		Type:      "Project",
		Overview:  "Distributed Scheduler",
		Date:      "Spring 2026",
		Details:   []Block{{Kind: "bullet", HTML: "Built a scheduler."}},
		IncludeIn: []string{"cv1"},
	}
	resume, err := ConvertPortalForm(url.Values{
		"standard7": {entry.Type},
		"subject7":  {EntrySubjectHTML(entry)},
		"7resume1":  {"Y"},
	})
	if err != nil {
		t.Fatal(err)
	}
	if len(resume.Entries) != 1 || resume.Entries[0].Overview != entry.Overview || resume.Entries[0].Date != entry.Date {
		t.Fatalf("round-tripped entry = %#v", resume.Entries)
	}
}

func TestResumeRejectsInvalidDefaultFontSize(t *testing.T) {
	resume := Resume{SchemaVersion: 1, DefaultFontSize: 25, Variants: []Variant{{ID: "cv1"}, {ID: "cv2"}, {ID: "cv3"}}}
	if err := resume.Validate(); err == nil || !strings.Contains(err.Error(), "default font size") {
		t.Fatalf("Validate() error = %v, want default font size error", err)
	}
}

func TestCanonicalResumeMapsToERP(t *testing.T) {
	resume, err := Load(filepath.Join("..", "..", "data", "resume.example.json"))
	if err != nil {
		t.Fatal(err)
	}
	fields := resume.PortalFields()
	if len(resume.Entries) != 4 || fields["standard7"] != "Internship" || fields["standard8"] != "Project" {
		t.Fatal("example resume did not map expected entries")
	}
	if !strings.HasPrefix(fields["subject7"], `<p><span style="font-size: 10px;"><strong>Example Company`) {
		t.Fatal("example resume did not move entry overview into the portal body")
	}
	if !strings.Contains(fields["subject7"], `May 2026 - July 2026</strong>`) || !strings.Contains(fields["subject7"], `&nbsp;&nbsp;&nbsp;&nbsp;`) {
		t.Fatal("example resume did not right-align its free-form date in the entry heading")
	}
	if !strings.Contains(fields["subject7"], `<ul><li><span style="font-size: 10px;">`) {
		t.Fatal("example resume did not use portal unordered-list bullets")
	}
	if fields["cv1_pref1"] != "Internship" || fields["8resume2"] != "Y" {
		t.Fatal("example resume did not map variant settings")
	}
}

func TestHiddenEntryKeepsItsSlotButIsExcludedFromEveryCV(t *testing.T) {
	resume, err := Load(filepath.Join("..", "..", "data", "resume.example.json"))
	if err != nil {
		t.Fatal(err)
	}
	resume.Entries[0].Hidden = true
	fields := resume.PortalFields()
	if fields["standard7"] == "" || fields["subject7"] == "" {
		t.Fatal("hidden entry content was removed from its stable portal slot")
	}
	for _, name := range []string{"7resume1", "7resume2", "7resume3"} {
		if fields[name] != "N" {
			t.Fatalf("hidden entry inclusion %s = %q, want N", name, fields[name])
		}
	}
}

func TestEntryOverviewMayBeEmpty(t *testing.T) {
	resume := Resume{
		SchemaVersion: 1,
		Entries: []Entry{{
			ID:      "entry-without-heading",
			Type:    "Experience",
			Details: []Block{{Kind: "bullet", HTML: "Implemented a systems feature."}},
		}},
		Variants: []Variant{{ID: "cv1"}, {ID: "cv2"}, {ID: "cv3"}},
	}
	if err := resume.Validate(); err != nil {
		t.Fatalf("empty entry overview was rejected: %v", err)
	}
}

func TestBlockHTML(t *testing.T) {
	got := BlockHTML(`<p><strong>Languages</strong>: C++</p><p>Linux</p>`)
	want := `<p><span style="font-size: 10px;"><strong>Languages</strong>: C++</span></p><p><span style="font-size: 10px;">Linux</span></p>`
	if got != want {
		t.Fatalf("BlockHTML()\n got %s\nwant %s", got, want)
	}
}

func TestBlockHTMLPreservesLists(t *testing.T) {
	got := BlockHTML(`<ul><li><strong>Systems</strong> programming</li><li>Linux</li></ul>`)
	want := `<ul><li><span style="font-size: 10px;">&nbsp;<strong>Systems</strong> programming</span></li><li><span style="font-size: 10px;">&nbsp;Linux</span></li></ul>`
	if got != want {
		t.Fatalf("BlockHTML()\n got %s\nwant %s", got, want)
	}
}

func TestBlockHTMLPreservesCustomSizeWithoutAccumulatingBulletSpacing(t *testing.T) {
	got := BlockHTML(`<ul><li><span style="font-size: 9px;">&nbsp;Systems programming</span></li></ul>`)
	want := `<ul><li><span style="font-size: 10px;">&nbsp;<span style="font-size: 9px;">Systems programming</span></span></li></ul>`
	if got != want {
		t.Fatalf("BlockHTML()\n got %s\nwant %s", got, want)
	}
}
