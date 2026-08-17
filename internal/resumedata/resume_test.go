package resumedata

import (
	"path/filepath"
	"strings"
	"testing"
)

func TestPortalFormatting(t *testing.T) {
	html := BlocksHTML([]Block{
		{Kind: "bullet", HTML: `Temporarily omitted.`, Hidden: true},
		{Kind: "bullet", HTML: `Added <a href="https://example.com"><strong>fast math</strong></a> with <em>care</em>: 12.7 → 7.2 ns at 9.4× throughput.`},
	})
	want := `<ul><li><span style="font-size: 10px;">&nbsp;Added <strong>fast math</strong> with <em>care</em>: 12.7 to 7.2 ns at 9.4x throughput.</span></li></ul>`
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

func TestBlockHTMLDoesNotAccumulatePortalBulletSpacing(t *testing.T) {
	got := BlockHTML(`<ul><li><span style="font-size: 9px;">&nbsp;Systems programming</span></li></ul>`)
	want := `<ul><li><span style="font-size: 10px;">&nbsp;Systems programming</span></li></ul>`
	if got != want {
		t.Fatalf("BlockHTML()\n got %s\nwant %s", got, want)
	}
}
