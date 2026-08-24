package resumedata

import (
	"encoding/json"
	"fmt"
	"html"
	"os"
	"regexp"
	"sort"
	"strconv"
	"strings"
	"unicode"

	xhtml "golang.org/x/net/html"
	"golang.org/x/net/html/atom"
)

const (
	PortalFontSize = 10
	MinFontSize    = 8
	MaxFontSize    = 24
	MaxGapPixels   = 24

	// PortalHeadingWidth is the usable width of an ERP entry heading at the
	// browser-to-PDF scale used by the portal. Heading dates are positioned with
	// non-breaking spaces because ERP has no right-aligned entry-date column.
	PortalHeadingWidth = 680
	minHeadingSpaces   = 4
)

var (
	entryTypes         = setOf("Internship", "Project", "Internship/Project", "Academic Achievement", "Certification", "Training", "Experience", "Entrepreneurial", "Competition/Conference", "Publication", "Position of Responsibilities")
	sections           = setOf("Internship", "Project", "Internship/Project", "Academic Achievement", "Certification", "Training", "Experience", "Entrepreneurial", "Competition/Conference", "Publication", "Position of Responsibilities", "eaa", "skill", "coursework")
	spaceRE            = regexp.MustCompile(`\s+`)
	arrowRE            = regexp.MustCompile(`\s*[→➜➝⟶]\s*`)
	fontSizeRE         = regexp.MustCompile(`(?i)(?:^|;)\s*font-size\s*:\s*(\d{1,2})px\s*(?:;|$)`)
	compatibleReplacer = strings.NewReplacer("\u2010", "-", "\u2011", "-", "\u2012", "-", "\u2013", "-", "\u2014", "-", "\u2015", "-", "\u2212", "-", "\u2018", "'", "\u2019", "'", "\u201a", "'", "\u201b", "'", "\u02bc", "'", "\uff07", "'", "\u2192", " to ", "\u279c", " to ", "\u279d", " to ", "\u27f6", " to ", "\u00d7", "x", "\u2022", "-", "\u25e6", "-", "\u25aa", "-")
)

type Resume struct {
	SchemaVersion   int       `json:"schemaVersion"`
	DefaultFontSize int       `json:"defaultFontSize,omitempty"`
	Metadata        Metadata  `json:"metadata,omitempty"`
	Academics       Academics `json:"academics"`
	Shared          Shared    `json:"shared"`
	Variants        []Variant `json:"variants"`
	Entries         []Entry   `json:"entries"`
}

type Metadata struct {
	Name       string `json:"name,omitempty"`
	DocumentID string `json:"documentId,omitempty"`
	UpdatedAt  string `json:"updatedAt,omitempty"`
}

type Academics struct {
	Previous []Academic `json:"previous"`
	Current  Academic   `json:"current"`
}

type Academic struct {
	Slot           int    `json:"slot"`
	Standard       string `json:"standard"`
	Qualification  string `json:"qualification"`
	Institution    string `json:"institution"`
	CompletionYear string `json:"completionYear"`
	Specialization string `json:"specialization"`
	Score          Score  `json:"score"`
}

type Score struct {
	Kind  string `json:"kind"`
	Value string `json:"value"`
	OutOf string `json:"outOf"`
}

type Shared struct {
	CourseworkHTML string `json:"courseworkHtml"`
}

type Variant struct {
	ID                  string   `json:"id"`
	Label               string   `json:"label"`
	SectionOrder        []string `json:"sectionOrder"`
	SkillsHTML          string   `json:"skillsHtml"`
	ExtracurricularHTML string   `json:"extracurricularHtml"`
	ShowMinor           bool     `json:"showMinor"`
	ShowMicro           bool     `json:"showMicro"`
}

type Entry struct {
	ID        string   `json:"id"`
	Type      string   `json:"type"`
	Overview  string   `json:"overview"`
	Date      string   `json:"date,omitempty"`
	Details   []Block  `json:"details"`
	IncludeIn []string `json:"includeIn"`
	Hidden    bool     `json:"hidden,omitempty"`
}

type Block struct {
	Kind      string `json:"kind"`
	HTML      string `json:"html"`
	Hidden    bool   `json:"hidden,omitempty"`
	GapBefore int    `json:"gapBefore,omitempty"`
	GapAfter  int    `json:"gapAfter,omitempty"`
}

func Load(path string) (*Resume, error) {
	data, err := os.ReadFile(path)
	if err != nil {
		return nil, err
	}
	var resume Resume
	if err := json.Unmarshal(data, &resume); err != nil {
		return nil, fmt.Errorf("parse resume JSON: %w", err)
	}
	if err := resume.Validate(); err != nil {
		return nil, err
	}
	return &resume, nil
}

func (r *Resume) Validate() error {
	var errors []string
	if r.SchemaVersion != 1 {
		errors = append(errors, "only schemaVersion 1 is supported")
	}
	if r.DefaultFontSize != 0 && !validFontSize(r.DefaultFontSize) {
		errors = append(errors, fmt.Sprintf("default font size must be between %d and %d px", MinFontSize, MaxFontSize))
	}
	if len(r.Entries) > 50 {
		errors = append(errors, fmt.Sprintf("ERP supports 50 entries; found %d", len(r.Entries)))
	}
	ids := map[string]bool{}
	for index, entry := range r.Entries {
		label := fmt.Sprintf("entry %d", index+1)
		if strings.TrimSpace(entry.ID) == "" {
			errors = append(errors, label+" has no ID")
		}
		if ids[entry.ID] {
			errors = append(errors, label+" duplicates ID "+entry.ID)
		}
		ids[entry.ID] = true
		if !entryTypes[entry.Type] {
			errors = append(errors, label+" has unsupported type "+entry.Type)
		}
		if len(entry.Details) == 0 {
			errors = append(errors, label+" needs details")
		}
		for _, block := range entry.Details {
			if block.Kind != "bullet" && block.Kind != "paragraph" {
				errors = append(errors, label+" has unsupported block kind "+block.Kind)
			}
			if strings.TrimSpace(block.HTML) == "" {
				errors = append(errors, label+" contains an empty block")
			}
			if !validGap(block.GapBefore) || !validGap(block.GapAfter) {
				errors = append(errors, fmt.Sprintf("%s has a detail spacing outside 0-%d px", label, MaxGapPixels))
			}
		}
	}
	if len(r.Variants) != 3 {
		errors = append(errors, "resume must define CV1, CV2, and CV3")
	} else {
		for index, variant := range r.Variants {
			expected := fmt.Sprintf("cv%d", index+1)
			if variant.ID != expected {
				errors = append(errors, fmt.Sprintf("variant %d must be %s", index+1, expected))
			}
			if len(variant.SectionOrder) > 14 {
				errors = append(errors, variant.Label+" has more than 14 sections")
			}
			seen := map[string]bool{}
			for _, section := range variant.SectionOrder {
				if !sections[section] {
					errors = append(errors, variant.Label+" has unsupported section "+section)
				}
				if seen[section] {
					errors = append(errors, variant.Label+" repeats section "+section)
				}
				seen[section] = true
			}
		}
	}
	if len(errors) > 0 {
		sort.Strings(errors)
		return fmt.Errorf("invalid resume:\n- %s", strings.Join(errors, "\n- "))
	}
	return nil
}

func (r *Resume) effectiveFontSize() int {
	return normalizedFontSize(r.DefaultFontSize)
}

func validFontSize(value int) bool {
	return value >= MinFontSize && value <= MaxFontSize
}

func normalizedFontSize(value int) int {
	if validFontSize(value) {
		return value
	}
	return PortalFontSize
}

func (r *Resume) PortalFields() map[string]string {
	fields := map[string]string{}
	fontSize := r.effectiveFontSize()
	for slot := 1; slot <= 5; slot++ {
		var academic *Academic
		for index := range r.Academics.Previous {
			if r.Academics.Previous[index].Slot == slot {
				academic = &r.Academics.Previous[index]
				break
			}
		}
		fields[fmt.Sprintf("standard%d", slot)] = value(academic, func(a *Academic) string { return a.Standard })
		fields[fmt.Sprintf("qualification%d", slot)] = value(academic, func(a *Academic) string { return a.Qualification })
		fields[fmt.Sprintf("university%d", slot)] = value(academic, func(a *Academic) string { return a.Institution })
		fields[fmt.Sprintf("year%d", slot)] = value(academic, func(a *Academic) string { return a.CompletionYear })
		kind := "perradio"
		if academic != nil && academic.Score.Kind == "cgpa" {
			kind = "cgparadio"
		}
		fields[fmt.Sprintf("percgpa%d", slot)] = kind
		if academic != nil && academic.Score.Kind == "percentage" {
			fields[fmt.Sprintf("percentage%d", slot)] = academic.Score.Value
		} else {
			fields[fmt.Sprintf("percentage%d", slot)] = ""
		}
		if academic != nil && academic.Score.Kind == "cgpa" {
			fields[fmt.Sprintf("cgpa%d", slot)] = academic.Score.Value
			fields[fmt.Sprintf("outof%d", slot)] = academic.Score.OutOf
		} else {
			fields[fmt.Sprintf("cgpa%d", slot)] = ""
			fields[fmt.Sprintf("outof%d", slot)] = ""
		}
	}
	current := r.Academics.Current
	fields["year6"] = current.CompletionYear
	fields["percgpa6"] = "perradio"
	fields["percentage6"] = ""
	fields["cgpa6"] = ""
	fields["outof6"] = ""
	if current.Score.Kind == "cgpa" {
		fields["percgpa6"] = "cgparadio"
		fields["cgpa6"] = current.Score.Value
		fields["outof6"] = current.Score.OutOf
	} else {
		fields["percentage6"] = current.Score.Value
	}

	for slot := 7; slot <= 56; slot++ {
		index := slot - 7
		if index < len(r.Entries) {
			entry := r.Entries[index]
			fields[fmt.Sprintf("standard%d", slot)] = entry.Type
			fields[fmt.Sprintf("university%d", slot)] = ""
			fields[fmt.Sprintf("subject%d", slot)] = entrySubjectHTML(entry, fontSize)
			for variant := 1; variant <= 3; variant++ {
				fields[fmt.Sprintf("%dresume%d", slot, variant)] = yesNo(!entry.Hidden && contains(entry.IncludeIn, fmt.Sprintf("cv%d", variant)))
			}
		} else {
			fields[fmt.Sprintf("standard%d", slot)] = ""
			fields[fmt.Sprintf("university%d", slot)] = ""
			fields[fmt.Sprintf("subject%d", slot)] = ""
			for variant := 1; variant <= 3; variant++ {
				fields[fmt.Sprintf("%dresume%d", slot, variant)] = "N"
			}
		}
	}

	fields["research_area"] = blockHTML(r.Shared.CourseworkHTML, fontSize)
	for index, variant := range r.Variants {
		number := index + 1
		fields[fmt.Sprintf("showminor%d", number)] = yesNo(variant.ShowMinor)
		fields[fmt.Sprintf("showmicro%d", number)] = yesNo(variant.ShowMicro)
		fields[[]string{"skill", "skill2", "skill3"}[index]] = blockHTML(variant.SkillsHTML, fontSize)
		fields[[]string{"eaa", "objective", "gymkhana"}[index]] = blockHTML(variant.ExtracurricularHTML, fontSize)
		for position := 1; position <= 14; position++ {
			selected := ""
			if position <= len(variant.SectionOrder) {
				selected = variant.SectionOrder[position-1]
			}
			fields[fmt.Sprintf("cv%d_pref%d", number, position)] = selected
		}
	}
	return fields
}

func EntrySubjectHTML(entry Entry) string {
	return entrySubjectHTML(entry, PortalFontSize)
}

func entrySubjectHTML(entry Entry, fontSize int) string {
	return blocksHTML(entrySubjectBlocks(entry, fontSize), fontSize)
}

func entrySubjectBlocks(entry Entry, fontSize int) []Block {
	overview := strings.TrimSpace(entry.Overview)
	date := strings.TrimSpace(entry.Date)
	if overview == "" && date == "" {
		return entry.Details
	}
	heading := html.EscapeString(overview)
	if date != "" {
		heading += strings.Repeat("\u00a0", headingSpacerCount(overview, date, fontSize)) + html.EscapeString(date)
	}
	blocks := make([]Block, 0, len(entry.Details)+1)
	blocks = append(blocks, Block{
		Kind: "paragraph",
		HTML: "<strong>" + heading + "</strong>",
	})
	blocks = append(blocks, entry.Details...)
	return blocks
}

// headingSpacerCount converts the remaining line width into non-breaking
// spaces. These width buckets approximate the ERP heading's bold font.
func headingSpacerCount(overview, date string, fontSize int) int {
	fontSize = normalizedFontSize(fontSize)
	remainingUnits := PortalHeadingWidth*1000/fontSize - headingTextWidth(compatibleText(overview)) - headingTextWidth(compatibleText(date))
	spaces := remainingUnits / headingRuneWidth(' ')
	if spaces < minHeadingSpaces {
		return minHeadingSpaces
	}
	return spaces
}

func headingTextWidth(value string) int {
	width := 0
	for _, character := range value {
		width += headingRuneWidth(character)
	}
	return width
}

func headingRuneWidth(character rune) int {
	switch {
	case character == ' ':
		return 278
	case strings.ContainsRune("fijltI.,:;!'|[](){}", character):
		return 278
	case strings.ContainsRune("-_/\\", character):
		return 333
	case strings.ContainsRune("mwMW@%&QO", character):
		return 833
	case character >= 'A' && character <= 'Z':
		return 667
	case character >= '0' && character <= '9':
		return 556
	default:
		return 556
	}
}

func BlocksHTML(blocks []Block) string {
	return blocksHTML(blocks, PortalFontSize)
}

func blocksHTML(blocks []Block, fontSize int) string {
	fontSize = normalizedFontSize(fontSize)
	var output strings.Builder
	inList := false
	closeList := func() {
		if inList {
			output.WriteString(`</ul>`)
			inList = false
		}
	}
	for _, block := range blocks {
		if block.Hidden {
			continue
		}
		if block.GapBefore > 0 {
			closeList()
			writeSpacer(&output, block.GapBefore)
		}
		if block.Kind == "paragraph" {
			closeList()
			writeParagraph(&output, renderInline(block.HTML), fontSize)
		} else {
			if !inList {
				output.WriteString(`<ul>`)
				inList = true
			}
			writeListItem(&output, renderInline(block.HTML), fontSize)
		}
		if block.GapAfter > 0 {
			closeList()
			writeSpacer(&output, block.GapAfter)
		}
	}
	closeList()
	return output.String()
}

func BlockHTML(source string) string {
	return blockHTML(source, PortalFontSize)
}

func blockHTML(source string, fontSize int) string {
	fontSize = normalizedFontSize(fontSize)
	nodes, err := parseFragment(source)
	if err != nil {
		return ""
	}
	var output strings.Builder
	inList := false
	closeList := func() {
		if inList {
			output.WriteString(`</ul>`)
			inList = false
		}
	}
	writeBlock := func(kind string, content string, gapBefore int, gapAfter int) {
		if gapBefore > 0 {
			closeList()
			writeSpacer(&output, gapBefore)
		}
		if kind == "bullet" {
			if !inList {
				output.WriteString(`<ul>`)
				inList = true
			}
			writeListItem(&output, trimLeadingPortalSpaces(content), fontSize)
		} else {
			closeList()
			writeParagraph(&output, content, fontSize)
		}
		if gapAfter > 0 {
			closeList()
			writeSpacer(&output, gapAfter)
		}
	}
	for _, node := range nodes {
		if node.Type == xhtml.TextNode && strings.TrimSpace(node.Data) == "" {
			continue
		}
		if node.Type == xhtml.ElementNode && node.Data == "ul" {
			for child := node.FirstChild; child != nil; child = child.NextSibling {
				if child.Type == xhtml.ElementNode && child.Data == "li" {
					removeLeadingPortalSpace(child)
					writeBlock("bullet", renderChildren(child), elementGap(child, "data-gap-before"), elementGap(child, "data-gap-after"))
				}
			}
			continue
		}
		content := renderNodeContent(node)
		writeBlock("paragraph", content, elementGap(node, "data-gap-before"), elementGap(node, "data-gap-after"))
	}
	closeList()
	return output.String()
}

func writeParagraph(output *strings.Builder, content string, fontSize int) {
	content = protectAmpersandSpaces(content)
	fmt.Fprintf(output, `<p><span style="font-size: %dpx;">%s</span></p>`, fontSize, content)
}

func writeListItem(output *strings.Builder, content string, fontSize int) {
	content = protectAmpersandSpaces(content)
	fmt.Fprintf(output, `<li><span style="font-size: %dpx;">&nbsp;%s</span></li>`, fontSize, content)
}

func writeSpacer(output *strings.Builder, pixels int) {
	pixels = clampGap(pixels)
	if pixels == 0 {
		return
	}
	fmt.Fprintf(output, `<p><span style="font-size: %dpx; line-height: %dpx;">&nbsp;</span></p>`, pixels, pixels)
}

func elementGap(node *xhtml.Node, name string) int {
	if node == nil || node.Type != xhtml.ElementNode {
		return 0
	}
	for _, attr := range node.Attr {
		if attr.Key != name {
			continue
		}
		value, err := strconv.Atoi(strings.TrimSpace(attr.Val))
		if err != nil {
			return 0
		}
		return clampGap(value)
	}
	return 0
}

func validGap(value int) bool {
	return value >= 0 && value <= MaxGapPixels
}

func clampGap(value int) int {
	if value < 0 {
		return 0
	}
	if value > MaxGapPixels {
		return MaxGapPixels
	}
	return value
}

func CompatibleText(input string) string {
	return spaceRE.ReplaceAllString(strings.ReplaceAll(compatibleText(input), "\u00a0", " "), " ")
}

func portalTextHTML(input string) string {
	text := compatibleText(input)
	var output strings.Builder
	previousSpace := false
	previousAmpersand := false
	for _, character := range text {
		switch character {
		case '\u00a0':
			output.WriteString("&nbsp;")
			previousSpace = true
			previousAmpersand = false
		case ' ', '\t', '\n', '\f', '\r':
			if previousAmpersand || previousSpace {
				output.WriteString("&nbsp;")
			} else {
				output.WriteByte(' ')
			}
			previousSpace = true
			previousAmpersand = false
		case '&':
			output.WriteString("&amp;")
			previousSpace = false
			previousAmpersand = true
		case '<':
			output.WriteString("&lt;")
			previousSpace = false
			previousAmpersand = false
		case '>':
			output.WriteString("&gt;")
			previousSpace = false
			previousAmpersand = false
		case '"':
			output.WriteString("&#34;")
			previousSpace = false
			previousAmpersand = false
		case '\'':
			output.WriteString("&#39;")
			previousSpace = false
			previousAmpersand = false
		default:
			output.WriteRune(character)
			previousSpace = false
			previousAmpersand = false
		}
	}
	return output.String()
}

func compatibleText(input string) string {
	return compatibleReplacer.Replace(arrowRE.ReplaceAllString(input, " to "))
}

func trimLeadingPortalSpaces(input string) string {
	for {
		trimmed := strings.TrimLeft(input, " \t\r\n")
		if trimmed != input {
			input = trimmed
			continue
		}
		if strings.HasPrefix(input, "&nbsp;") {
			input = strings.TrimPrefix(input, "&nbsp;")
			continue
		}
		return input
	}
}

func removeLeadingPortalSpace(node *xhtml.Node) bool {
	if node == nil {
		return false
	}
	if node.Type == xhtml.TextNode {
		node.Data = strings.TrimLeftFunc(node.Data, unicode.IsSpace)
		return node.Data != ""
	}
	for child := node.FirstChild; child != nil; child = child.NextSibling {
		if removeLeadingPortalSpace(child) {
			return true
		}
	}
	return false
}

func protectAmpersandSpaces(input string) string {
	return strings.ReplaceAll(input, "&amp; ", "&amp;&nbsp;")
}

func renderInline(source string) string {
	nodes, err := parseFragment(source)
	if err != nil {
		return html.EscapeString(CompatibleText(source))
	}
	var output strings.Builder
	for _, node := range nodes {
		output.WriteString(renderAllowed(node))
	}
	return output.String()
}

func renderAllowed(node *xhtml.Node) string {
	switch node.Type {
	case xhtml.TextNode:
		return portalTextHTML(node.Data)
	case xhtml.ElementNode:
		children := renderChildren(node)
		switch node.Data {
		case "strong", "b":
			return "<strong>" + children + "</strong>"
		case "em", "i":
			return "<em>" + children + "</em>"
		case "br":
			return "<br>"
		case "span":
			if size := elementFontSize(node); size != 0 {
				return fmt.Sprintf(`<span style="font-size: %dpx;">%s</span>`, size, children)
			}
			return children
		default:
			return children
		}
	default:
		return ""
	}
}

func elementFontSize(node *xhtml.Node) int {
	if node == nil || node.Type != xhtml.ElementNode {
		return 0
	}
	for _, attr := range node.Attr {
		if !strings.EqualFold(attr.Key, "style") {
			continue
		}
		match := fontSizeRE.FindStringSubmatch(attr.Val)
		if len(match) != 2 {
			return 0
		}
		size, err := strconv.Atoi(match[1])
		if err == nil && validFontSize(size) {
			return size
		}
		return 0
	}
	return 0
}

func renderChildren(node *xhtml.Node) string {
	var output strings.Builder
	for child := node.FirstChild; child != nil; child = child.NextSibling {
		output.WriteString(renderAllowed(child))
	}
	return output.String()
}

func renderNodeContent(node *xhtml.Node) string {
	if node.Type == xhtml.ElementNode {
		return renderChildren(node)
	}
	return renderAllowed(node)
}

func parseFragment(source string) ([]*xhtml.Node, error) {
	context := &xhtml.Node{Type: xhtml.ElementNode, Data: "div", DataAtom: atom.Div}
	return xhtml.ParseFragment(strings.NewReader(source), context)
}

func setOf(values ...string) map[string]bool {
	result := map[string]bool{}
	for _, value := range values {
		result[value] = true
	}
	return result
}

func value(academic *Academic, getter func(*Academic) string) string {
	if academic == nil {
		return ""
	}
	return getter(academic)
}

func yesNo(value bool) string {
	if value {
		return "Y"
	}
	return "N"
}

func contains(values []string, wanted string) bool {
	for _, value := range values {
		if value == wanted {
			return true
		}
	}
	return false
}
