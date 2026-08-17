package erp

import (
	"net/url"
	"os"
	"path/filepath"
	"testing"
)

func TestParseForm(t *testing.T) {
	snapshot, err := ParseForm([]byte(`<!doctype html><form id="from2_stu" action="SFA.jsp">
      <input name="plain" value="value"><input type="radio" name="choice" value="A"><input type="radio" name="choice" value="B" checked>
      <textarea name="subject7"><p><strong>Fast</strong></p></textarea>
      <select name="section"><option value="">Select</option><option value="Experience" selected>Experience</option></select>
      <button name="save" value="yes">Save</button>
    </form>`))
	if err != nil {
		t.Fatal(err)
	}
	if snapshot.Action != "/TrainingPlacementSSO/SFA.jsp" || snapshot.Values.Get("choice") != "B" || snapshot.Values.Get("section") != "Experience" {
		t.Fatalf("unexpected snapshot: %#v", snapshot)
	}
	if snapshot.Values.Get("save") != "" {
		t.Fatal("button should not be submitted")
	}
}

func TestParseSanitizedPortalFixture(t *testing.T) {
	body, err := os.ReadFile(filepath.Join("..", "..", "tests", "fixtures", "student-form.html"))
	if err != nil {
		t.Fatal(err)
	}
	snapshot, err := ParseForm(body)
	if err != nil {
		t.Fatal(err)
	}
	if snapshot.Values.Get("full_name") != "Test Student" || snapshot.Values.Get("7resume1") != "Y" {
		t.Fatal("sanitized fixture did not parse expected fields")
	}
}

func TestVerifyFieldsNormalizesPortalHTML(t *testing.T) {
	actual := url.Values{
		"subject7":  {`<ul><li><span style="font-size:10px;">Added </span><span style="font-size:10px;"><strong>fast math</strong></span><span>.</span></li></ul>`},
		"subject12": {`<ul><li><span style="font-size:10px;">Attached to /proc/<pid>/exe with uprobes.</span></li></ul>`},
	}
	expected := map[string]string{
		"subject7":  `<ul><li><span style="font-size: 9px;">&nbsp;Added fast math.</span></li></ul>`,
		"subject12": `<ul><li><span style="font-size: 9px;">&nbsp;Attached to /proc/&lt;pid&gt;/exe with uprobes.</span></li></ul>`,
	}
	if err := VerifyFields(actual, expected); err != nil {
		t.Fatal(err)
	}
}

func TestVerifyFieldsIgnoresInclusionDefaultsForUnusedEntries(t *testing.T) {
	actual := url.Values{
		"standard17": {""}, "university17": {""}, "subject17": {""},
		"17resume1": {"Y"}, "17resume2": {"Y"}, "17resume3": {"Y"},
	}
	expected := map[string]string{
		"standard17": "", "university17": "", "subject17": "",
		"17resume1": "N", "17resume2": "N", "17resume3": "N",
	}
	if err := VerifyFields(actual, expected); err != nil {
		t.Fatal(err)
	}

	expected["standard17"] = "Experience"
	if err := VerifyFields(actual, expected); err == nil {
		t.Fatal("populated entry inclusion mismatch was ignored")
	}
}
