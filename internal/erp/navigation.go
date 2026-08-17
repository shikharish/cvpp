package erp

import (
	"context"
	"fmt"
	"net/url"
	"strings"

	"erp-cv-portal/internal/progress"

	xhtml "golang.org/x/net/html"
)

const (
	cdcModuleID         = "26"
	placementMenuID     = "11"
	placementEntryPoint = "/TrainingPlacementSSO/TPStudent.jsp"
)

func (c *Client) ensureTrainingPlacementSession(ctx context.Context) error {
	if c.trainingPlacementReady {
		return nil
	}
	return c.validateTrainingPlacementAccess(ctx)
}

func (c *Client) validateTrainingPlacementAccess(ctx context.Context) error {
	progress.Logf("ERP navigation: opening the CDC resume menu")
	entryURL := c.BaseURL + placementEntryPoint
	response, err := c.PostForm(ctx, "/IIT_ERP3/showmenu.htm", url.Values{
		"module_id": {cdcModuleID},
		"menu_id":   {placementMenuID},
		"link":      {entryURL},
	})
	if err != nil {
		return fmt.Errorf("open ERP CDC menu: %w", err)
	}
	body, err := readLimited(response.Body, 4<<20)
	response.Body.Close()
	if err != nil {
		return err
	}
	if response.StatusCode < 200 || response.StatusCode >= 300 || isAuthURL(response.Request.URL) {
		return fmt.Errorf("open ERP CDC menu: server returned %s", response.Status)
	}
	if isDeniedPage(body) {
		return fmt.Errorf("%w: ERP rejected the CDC resume menu request", errSessionRejected)
	}

	action, values, err := parseFormByID(body, "menuform")
	if err != nil {
		return fmt.Errorf("open ERP CDC menu: %w", err)
	}
	actionURL, err := resolveTrustedAction(c.BaseURL+"/IIT_ERP3/showmenu.htm", action, c.BaseURL)
	if err != nil {
		return fmt.Errorf("open ERP CDC menu: %w", err)
	}
	if actionURL.Path != placementEntryPoint {
		return fmt.Errorf("open ERP CDC menu: unexpected destination %s", actionURL.Path)
	}

	progress.Logf("ERP navigation: entering the Training Placement application")
	response, err = c.PostForm(ctx, actionURL.RequestURI(), values)
	if err != nil {
		return fmt.Errorf("enter Training Placement application: %w", err)
	}
	body, err = readLimited(response.Body, 4<<20)
	response.Body.Close()
	if err != nil {
		return err
	}
	if response.StatusCode < 200 || response.StatusCode >= 300 || isAuthURL(response.Request.URL) {
		return fmt.Errorf("enter Training Placement application: server returned %s", response.Status)
	}
	if isDeniedPage(body) {
		return fmt.Errorf("%w: ERP rejected the Training Placement entry request", errSessionRejected)
	}
	lower := strings.ToLower(string(body))
	if !strings.Contains(lower, "studentform.jsp") || !strings.Contains(lower, "cvgenerate.jsp") {
		return fmt.Errorf("enter Training Placement application: ERP returned an unexpected page")
	}
	c.trainingPlacementReady = true
	progress.Logf("ERP navigation: Training Placement session initialized")
	return nil
}

func parseFormByID(body []byte, id string) (string, url.Values, error) {
	document, err := xhtml.Parse(strings.NewReader(string(body)))
	if err != nil {
		return "", nil, err
	}
	form := findNode(document, func(node *xhtml.Node) bool {
		return node.Type == xhtml.ElementNode && node.Data == "form" && attr(node, "id") == id
	})
	if form == nil {
		return "", nil, fmt.Errorf("ERP navigation form %s was not found", id)
	}
	action := strings.TrimSpace(attr(form, "action"))
	if action == "" {
		return "", nil, fmt.Errorf("ERP navigation form %s has no action", id)
	}
	values := url.Values{}
	walk(form, func(node *xhtml.Node) {
		if node.Type != xhtml.ElementNode || node.Data != "input" || hasAttr(node, "disabled") {
			return
		}
		name := attr(node, "name")
		if name != "" {
			values.Add(name, attr(node, "value"))
		}
	})
	return action, values, nil
}

func resolveTrustedAction(base, action, trustedBase string) (*url.URL, error) {
	baseURL, err := url.Parse(base)
	if err != nil {
		return nil, err
	}
	actionURL, err := url.Parse(action)
	if err != nil {
		return nil, err
	}
	actionURL = baseURL.ResolveReference(actionURL)
	trustedURL, err := url.Parse(trustedBase)
	if err != nil {
		return nil, err
	}
	if !strings.EqualFold(actionURL.Scheme, trustedURL.Scheme) || !strings.EqualFold(actionURL.Host, trustedURL.Host) {
		return nil, fmt.Errorf("refusing untrusted ERP navigation destination %s", actionURL.Host)
	}
	return actionURL, nil
}
