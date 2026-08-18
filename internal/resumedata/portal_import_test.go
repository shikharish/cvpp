package resumedata

import (
	"net/url"
	"strings"
	"testing"
)

func TestConvertPortalFormRecoversBoldHeadingAndFormatting(t *testing.T) {
	values := url.Values{
		"standard7":     {"Internship"},
		"subject7":      {`<p><strong>Acme | Intern</strong></p><ul><li><span style="font-size: 10px;">&nbsp;Built </span><span style="font-size: 12px;"><strong>fast</strong></span><span style="font-size: 10px;"> systems</span></li><li>Shipped safely</li></ul>`},
		"7resume1":      {"Y"},
		"cv1_pref1":     {"Internship"},
		"research_area": {`<p><span style="font-size: 10px;">Default <span style="font-size: 12px;">custom</span></span></p>`},
	}
	resume, err := ConvertPortalForm(values)
	if err != nil {
		t.Fatal(err)
	}
	if len(resume.Entries) != 1 || resume.Entries[0].Overview != "Acme | Intern" {
		t.Fatalf("heading = %#v", resume.Entries)
	}
	if len(resume.Entries[0].Details) != 2 || !strings.Contains(resume.Entries[0].Details[0].HTML, "<strong>fast</strong>") {
		t.Fatalf("details = %#v", resume.Entries[0].Details)
	}
	if !strings.Contains(resume.Entries[0].Details[0].HTML, `style="font-size: 12px;"`) {
		t.Fatalf("custom font size was not imported: %#v", resume.Entries[0].Details)
	}
	if strings.Contains(resume.Entries[0].Details[0].HTML, `font-size: 10px`) {
		t.Fatalf("portal line wrapper was imported as a custom size: %#v", resume.Entries[0].Details)
	}
	if strings.Contains(resume.Shared.CourseworkHTML, `font-size: 10px`) || !strings.Contains(resume.Shared.CourseworkHTML, `font-size: 12px`) {
		t.Fatalf("shared portal typography was not normalized: %s", resume.Shared.CourseworkHTML)
	}
	resume.DefaultFontSize = 11
	portalSubject := resume.PortalFields()["subject7"]
	if !strings.Contains(portalSubject, `font-size: 11px`) || !strings.Contains(portalSubject, `font-size: 12px`) || strings.Contains(portalSubject, `font-size: 10px`) {
		t.Fatalf("imported entry did not adopt the new default and retain its custom size: %s", portalSubject)
	}
	if resume.Entries[0].IncludeIn[0] != "cv1" {
		t.Fatalf("include = %#v", resume.Entries[0].IncludeIn)
	}
}

func TestConvertPortalFormUsesPortalHeadingAndEmptyForms(t *testing.T) {
	resume, err := ConvertPortalForm(url.Values{"standard7": {"Project"}, "university7": {"Portal heading"}, "subject7": {`<p><span style="font-size: 9px;"><strong>Portal heading</strong></span></p><ul><li>One</li></ul>`}, "7resume1": {"Y"}})
	if err != nil {
		t.Fatal(err)
	}
	if got := resume.Entries[0].Overview; got != "Portal heading" {
		t.Fatalf("overview = %q", got)
	}
	if len(resume.Entries[0].Details) != 1 || resume.Entries[0].Details[0].Kind != "bullet" {
		t.Fatalf("details = %#v", resume.Entries[0].Details)
	}
	empty, err := ConvertPortalForm(url.Values{})
	if err != nil {
		t.Fatal(err)
	}
	if len(empty.Entries) != 0 || len(empty.Variants) != 3 {
		t.Fatalf("empty import = %#v", empty)
	}
}
