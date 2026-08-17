package resumedata

import (
	"net/url"
	"strings"
	"testing"
)

func TestConvertPortalFormRecoversBoldHeadingAndFormatting(t *testing.T) {
	values := url.Values{
		"standard7": {"Internship"},
		"subject7":  {`<p><strong>Acme | Intern</strong></p><ul><li>Built <strong>fast</strong> systems</li><li>Shipped safely</li></ul>`},
		"7resume1":  {"Y"},
		"cv1_pref1": {"Internship"},
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
