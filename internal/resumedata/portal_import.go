package resumedata

import (
	"html"
	"net/url"
	"regexp"
	"strconv"
	"strings"

	xhtml "golang.org/x/net/html"
)

// ConvertPortalForm converts the submitted values from StudentForm.jsp into
// the schema-v1 document used by the editor. It never performs a portal write.
// Keeping this converter independent from erp.Client makes fixture and
// round-trip tests deterministic.
func ConvertPortalForm(values url.Values) (*Resume, error) {
	resume := &Resume{
		SchemaVersion: 1,
		Metadata:      Metadata{DocumentID: "resume"},
		Variants:      []Variant{{ID: "cv1", Label: "CV1"}, {ID: "cv2", Label: "CV2"}, {ID: "cv3", Label: "CV3"}},
	}
	for slot := 1; slot <= 5; slot++ {
		academic := Academic{Slot: slot, Standard: valueAt(values, "standard", slot), Qualification: valueAt(values, "qualification", slot), Institution: valueAt(values, "university", slot), CompletionYear: valueAt(values, "year", slot), Score: scoreFrom(values, slot)}
		if academic.Standard != "" || academic.Qualification != "" || academic.Institution != "" || academic.CompletionYear != "" || academic.Score.Value != "" {
			resume.Academics.Previous = append(resume.Academics.Previous, academic)
		}
	}
	resume.Academics.Current = Academic{Slot: 6, Standard: valueAt(values, "standard", 6), Qualification: valueAt(values, "qualification", 6), Institution: valueAt(values, "university", 6), CompletionYear: valueAt(values, "year", 6), Specialization: firstValue(values, "specialization6", "branch6", "stream6"), Score: scoreFrom(values, 6)}

	resume.Shared.CourseworkHTML = firstValue(values, "research_area", "coursework")
	sectionFields := []string{"skill", "skill2", "skill3"}
	extraFields := []string{"eaa", "objective", "gymkhana"}
	for index, variant := range resume.Variants {
		variant.ShowMinor = strings.EqualFold(firstValue(values, "showminor"+itoa(index+1)), "Y")
		variant.ShowMicro = strings.EqualFold(firstValue(values, "showmicro"+itoa(index+1)), "Y")
		variant.SkillsHTML = firstValue(values, sectionFields[index])
		variant.ExtracurricularHTML = firstValue(values, extraFields[index])
		for position := 1; position <= 14; position++ {
			if section := strings.TrimSpace(firstValue(values, "cv"+itoa(index+1)+"_pref"+itoa(position))); section != "" {
				variant.SectionOrder = append(variant.SectionOrder, section)
			}
		}
		resume.Variants[index] = variant
	}

	for slot := 7; slot <= 56; slot++ {
		typeName := strings.TrimSpace(valueAt(values, "standard", slot))
		institution := strings.TrimSpace(valueAt(values, "university", slot))
		subject := strings.TrimSpace(firstValue(values, "subject"+itoa(slot), "description"+itoa(slot)))
		if typeName == "" && institution == "" && subject == "" {
			continue
		}
		if !entryTypeAllowed(typeName) {
			typeName = "Experience"
		}
		blocks, heading := importBlocks(subject)
		overview := firstValue(values, "overview"+itoa(slot), "title"+itoa(slot), "heading"+itoa(slot))
		if strings.TrimSpace(overview) == "" {
			overview = institution
		}
		if strings.TrimSpace(overview) == "" {
			overview = heading
		}
		if len(blocks) == 0 {
			blocks = []Block{{Kind: "paragraph", HTML: html.EscapeString(subject)}}
		}
		include := make([]string, 0, 3)
		for index := 1; index <= 3; index++ {
			if strings.EqualFold(firstValue(values, itoa(slot)+"resume"+itoa(index)), "Y") {
				include = append(include, "cv"+itoa(index))
			}
		}
		if len(include) == 0 {
			// Older portal variants omit inclusion controls. Retaining the entry
			// in CV1 is the least surprising editable result.
			include = []string{"cv1"}
		}
		resume.Entries = append(resume.Entries, Entry{ID: "portal-" + itoa(slot), Type: typeName, Overview: strings.TrimSpace(overview), Details: blocks, IncludeIn: include})
	}
	if err := resume.Validate(); err != nil {
		return nil, err
	}
	return resume, nil
}

func valueAt(values url.Values, prefix string, slot int) string {
	return strings.TrimSpace(firstValue(values, prefix+itoa(slot)))
}

func scoreFrom(values url.Values, slot int) Score {
	if strings.EqualFold(firstValue(values, "percgpa"+itoa(slot)), "cgparadio") || firstValue(values, "cgpa"+itoa(slot)) != "" {
		return Score{Kind: "cgpa", Value: firstValue(values, "cgpa"+itoa(slot)), OutOf: firstValue(values, "outof"+itoa(slot))}
	}
	return Score{Kind: "percentage", Value: firstValue(values, "percentage"+itoa(slot))}
}

func firstValue(values url.Values, names ...string) string {
	for _, name := range names {
		if items := values[name]; len(items) > 0 {
			return strings.TrimSpace(items[0])
		}
	}
	return ""
}

func itoa(value int) string {
	return strconv.Itoa(value)
}

func entryTypeAllowed(value string) bool { return entryTypes[value] }

func importBlocks(source string) ([]Block, string) {
	nodes, err := parseFragment(source)
	if err != nil {
		return nil, ""
	}
	var blocks []Block
	for _, node := range nodes {
		if node.Type == xhtml.TextNode && strings.TrimSpace(node.Data) == "" {
			continue
		}
		switch node.Data {
		case "ul", "ol":
			for child := node.FirstChild; child != nil; child = child.NextSibling {
				if child.Type == xhtml.ElementNode && child.Data == "li" {
					if value := strings.TrimSpace(serializeInline(child)); value != "" {
						blocks = append(blocks, Block{Kind: "bullet", HTML: value, GapBefore: elementGap(child, "data-gap-before"), GapAfter: elementGap(child, "data-gap-after")})
					}
				}
			}
		default:
			value := strings.TrimSpace(serializeInline(node))
			if value != "" {
				blocks = append(blocks, Block{Kind: "paragraph", HTML: value, GapBefore: elementGap(node, "data-gap-before"), GapAfter: elementGap(node, "data-gap-after")})
			}
		}
	}
	heading := ""
	if len(blocks) > 0 && blocks[0].Kind == "paragraph" {
		if firstIsBold(nodes) {
			heading = CompatibleText(html.UnescapeString(stripTags(blocks[0].HTML)))
			blocks = blocks[1:]
		}
	}
	return blocks, strings.TrimSpace(heading)
}

func firstIsBold(nodes []*xhtml.Node) bool {
	for _, node := range nodes {
		if node.Type == xhtml.TextNode && strings.TrimSpace(node.Data) == "" {
			continue
		}
		if node.Type != xhtml.ElementNode || (node.Data != "p" && node.Data != "div") {
			return false
		}
		return strings.TrimSpace(textContent(node)) != "" && isBoldOnly(node)
	}
	return false
}

func isBoldOnly(node *xhtml.Node) bool {
	if node == nil {
		return false
	}
	if node.Type == xhtml.TextNode {
		return false
	}
	if node.Type != xhtml.ElementNode {
		return true
	}
	if node.Data == "strong" || node.Data == "b" {
		return strings.TrimSpace(textContent(node)) != ""
	}
	if node.Data != "p" && node.Data != "div" && node.Data != "span" {
		return false
	}
	found := false
	for child := node.FirstChild; child != nil; child = child.NextSibling {
		if child.Type == xhtml.TextNode && strings.TrimSpace(child.Data) == "" {
			continue
		}
		if !isBoldOnly(child) {
			return false
		}
		found = true
	}
	return found
}

func textContent(node *xhtml.Node) string {
	if node == nil {
		return ""
	}
	if node.Type == xhtml.TextNode {
		return node.Data
	}
	var out strings.Builder
	for child := node.FirstChild; child != nil; child = child.NextSibling {
		out.WriteString(textContent(child))
	}
	return out.String()
}

func serializeInline(node *xhtml.Node) string {
	if node == nil {
		return ""
	}
	if node.Type == xhtml.TextNode {
		return html.EscapeString(strings.ReplaceAll(node.Data, "\u00a0", " "))
	}
	if node.Type != xhtml.ElementNode {
		return ""
	}
	var out strings.Builder
	for child := node.FirstChild; child != nil; child = child.NextSibling {
		out.WriteString(serializeInline(child))
	}
	switch node.Data {
	case "strong", "b":
		return "<strong>" + out.String() + "</strong>"
	case "em", "i":
		return "<em>" + out.String() + "</em>"
	case "br":
		return "<br>"
	default:
		return out.String()
	}
}

func stripTags(value string) string {
	return regexp.MustCompile(`<[^>]+>`).ReplaceAllString(value, "")
}
