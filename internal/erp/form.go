package erp

import (
	"context"
	"fmt"
	"html"
	"net/http"
	"net/url"
	"os"
	"path/filepath"
	"regexp"
	"sort"
	"strings"

	"erp-cv-portal/internal/progress"

	xhtml "golang.org/x/net/html"
)

var punctuationSpaceRE = regexp.MustCompile(`\s+([,.;:])`)
var entryInclusionRE = regexp.MustCompile(`^([0-9]+)resume[123]$`)
var htmlTagLikeRE = regexp.MustCompile(`<(/?)([A-Za-z][A-Za-z0-9_-]*)([^<>]*)>`)

type FormSnapshot struct {
	Values url.Values
	Action string
}

func (c *Client) FetchResumeForm(ctx context.Context) (*FormSnapshot, error) {
	if err := c.ensureTrainingPlacementSession(ctx); err != nil {
		return nil, err
	}
	response, err := c.Get(ctx, "/TrainingPlacementSSO/StudentForm.jsp")
	if err != nil {
		return nil, err
	}
	defer response.Body.Close()
	if response.StatusCode != http.StatusOK {
		return nil, fmt.Errorf("resume form returned %s", response.Status)
	}
	if isAuthURL(response.Request.URL) {
		return nil, fmt.Errorf("ERP session expired while fetching the resume form")
	}
	body, err := readLimited(response.Body, 12<<20)
	if err != nil {
		return nil, err
	}
	if isDeniedPage(body) {
		return nil, fmt.Errorf("%w: ERP rejected the resume form request", errSessionRejected)
	}
	return ParseForm(body)
}

func (c *Client) SyncResume(ctx context.Context, fields map[string]string) error {
	progress.Logf("ERP sync: fetching the current portal form")
	before, err := c.FetchResumeForm(ctx)
	if err != nil {
		return err
	}
	values := cloneValues(before.Values)
	for name, value := range fields {
		if _, present := before.Values[name]; !present {
			return fmt.Errorf("portal field %s is missing", name)
		}
		values.Set(name, value)
	}
	values.Set("mode", "checkdata")
	action := before.Action
	if action == "" {
		action = "/TrainingPlacementSSO/SFA.jsp"
	}
	progress.Logf("ERP sync: posting %d managed fields", len(fields))
	response, err := c.PostForm(ctx, action, values)
	if err != nil {
		return fmt.Errorf("save ERP resume: %w", err)
	}
	body, readErr := readLimited(response.Body, 2<<20)
	response.Body.Close()
	if readErr != nil {
		return readErr
	}
	if response.StatusCode < 200 || response.StatusCode >= 300 {
		return fmt.Errorf("ERP resume save returned %s", response.Status)
	}
	if isAuthURL(response.Request.URL) {
		return fmt.Errorf("ERP session expired while saving the resume")
	}
	if isDeniedPage(body) {
		return fmt.Errorf("%w: ERP rejected the resume save request", errSessionRejected)
	}

	progress.Logf("ERP sync: reading the saved form back for verification")
	after, err := c.FetchResumeForm(ctx)
	if err != nil {
		return fmt.Errorf("verify ERP resume: %w", err)
	}
	if err := VerifyFields(after.Values, fields); err != nil {
		return fmt.Errorf("ERP save verification failed: %w", err)
	}
	progress.Logf("ERP sync: verified %d managed fields", len(fields))
	return nil
}

func ParseForm(body []byte) (*FormSnapshot, error) {
	document, err := xhtml.Parse(strings.NewReader(string(body)))
	if err != nil {
		return nil, err
	}
	form := findNode(document, func(node *xhtml.Node) bool {
		return node.Type == xhtml.ElementNode && node.Data == "form" && attr(node, "id") == "from2_stu"
	})
	if form == nil {
		return nil, fmt.Errorf("ERP resume form from2_stu was not found")
	}
	values := url.Values{}
	walk(form, func(node *xhtml.Node) {
		if node.Type != xhtml.ElementNode || hasAttr(node, "disabled") {
			return
		}
		name := attr(node, "name")
		if name == "" {
			return
		}
		switch node.Data {
		case "input":
			typeName := strings.ToLower(attr(node, "type"))
			switch typeName {
			case "submit", "button", "reset", "file", "image":
				return
			case "radio", "checkbox":
				if !hasAttr(node, "checked") {
					return
				}
			}
			values.Add(name, attr(node, "value"))
		case "textarea":
			values.Add(name, textContent(node))
		case "select":
			var options []*xhtml.Node
			walk(node, func(option *xhtml.Node) {
				if option.Type == xhtml.ElementNode && option.Data == "option" && !hasAttr(option, "disabled") {
					options = append(options, option)
				}
			})
			selected := false
			for _, option := range options {
				if hasAttr(option, "selected") {
					values.Add(name, optionValue(option))
					selected = true
				}
			}
			if !selected && len(options) > 0 {
				values.Add(name, optionValue(options[0]))
			}
		}
	})
	action := attr(form, "action")
	if action != "" && !strings.HasPrefix(action, "/") {
		action = "/TrainingPlacementSSO/" + action
	}
	return &FormSnapshot{Values: values, Action: action}, nil
}

func VerifyFields(actual url.Values, expected map[string]string) error {
	var mismatches []string
	for name, wanted := range expected {
		if unusedEntryInclusion(name, expected) {
			continue
		}
		gotValues, present := actual[name]
		if !present {
			mismatches = append(mismatches, name+" is missing")
			continue
		}
		got := ""
		if len(gotValues) > 0 {
			got = gotValues[0]
		}
		if isHTMLField(name) {
			if semanticHTML(got) != semanticHTML(wanted) {
				mismatches = append(mismatches, name+" content differs")
			}
		} else if strings.TrimSpace(got) != strings.TrimSpace(wanted) {
			mismatches = append(mismatches, fmt.Sprintf("%s: got %q, wanted %q", name, got, wanted))
		}
	}
	if len(mismatches) > 0 {
		sort.Strings(mismatches)
		if len(mismatches) > 8 {
			mismatches = append(mismatches[:8], fmt.Sprintf("and %d more", len(mismatches)-8))
		}
		return fmt.Errorf("%s", strings.Join(mismatches, "; "))
	}
	return nil
}

func unusedEntryInclusion(name string, expected map[string]string) bool {
	match := entryInclusionRE.FindStringSubmatch(name)
	if len(match) != 2 {
		return false
	}
	slot := match[1]
	return expected["standard"+slot] == "" && expected["university"+slot] == "" && expected["subject"+slot] == ""
}

func WritePDF(path string, data []byte) error {
	if !filepath.IsAbs(path) {
		absolute, err := filepath.Abs(path)
		if err != nil {
			return err
		}
		path = absolute
	}
	if err := atomicWrite(path, data, 0o644); err != nil {
		return err
	}
	info, err := os.Stat(path)
	if err != nil {
		return err
	}
	progress.Logf("ERP PDF: wrote %s (%d bytes)", path, info.Size())
	return nil
}

func semanticHTML(source string) string {
	source = escapeUnknownHTMLTags(source)
	document, err := xhtml.Parse(strings.NewReader("<html><body>" + source + "</body></html>"))
	if err != nil {
		return strings.Join(strings.Fields(source), " ")
	}
	body := findNode(document, func(node *xhtml.Node) bool { return node.Type == xhtml.ElementNode && node.Data == "body" })
	if body == nil {
		return ""
	}
	text := punctuationSpaceRE.ReplaceAllString(strings.Join(strings.Fields(textContent(body)), " "), "$1")
	return text
}

func escapeUnknownHTMLTags(source string) string {
	return htmlTagLikeRE.ReplaceAllStringFunc(source, func(match string) string {
		parts := htmlTagLikeRE.FindStringSubmatch(match)
		if len(parts) < 3 {
			return match
		}
		switch strings.ToLower(parts[2]) {
		case "p", "span", "ul", "ol", "li", "strong", "em", "b", "i", "u", "br", "div":
			return match
		default:
			return html.EscapeString(match)
		}
	})
}

func isHTMLField(name string) bool {
	if strings.HasPrefix(name, "subject") {
		return true
	}
	switch name {
	case "research_area", "skill", "skill2", "skill3", "eaa", "objective", "gymkhana":
		return true
	default:
		return false
	}
}

func optionValue(node *xhtml.Node) string {
	if value, present := attrValue(node, "value"); present {
		return value
	}
	return textContent(node)
}

func cloneValues(values url.Values) url.Values {
	copy := url.Values{}
	for name, items := range values {
		copy[name] = append([]string(nil), items...)
	}
	return copy
}

func findNode(node *xhtml.Node, predicate func(*xhtml.Node) bool) *xhtml.Node {
	if predicate(node) {
		return node
	}
	for child := node.FirstChild; child != nil; child = child.NextSibling {
		if found := findNode(child, predicate); found != nil {
			return found
		}
	}
	return nil
}

func walk(node *xhtml.Node, visit func(*xhtml.Node)) {
	visit(node)
	for child := node.FirstChild; child != nil; child = child.NextSibling {
		walk(child, visit)
	}
}

func textContent(node *xhtml.Node) string {
	if node.Type == xhtml.TextNode {
		return node.Data
	}
	var output strings.Builder
	for child := node.FirstChild; child != nil; child = child.NextSibling {
		output.WriteString(textContent(child))
	}
	return output.String()
}

func attr(node *xhtml.Node, name string) string {
	value, _ := attrValue(node, name)
	return value
}

func attrValue(node *xhtml.Node, name string) (string, bool) {
	for _, attribute := range node.Attr {
		if attribute.Key == name {
			return attribute.Val, true
		}
	}
	return "", false
}

func hasAttr(node *xhtml.Node, name string) bool {
	for _, attribute := range node.Attr {
		if attribute.Key == name {
			return true
		}
	}
	return false
}
