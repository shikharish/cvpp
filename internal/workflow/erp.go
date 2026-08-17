package workflow

import (
	"context"
	"fmt"
	"path/filepath"
	"time"

	"erp-cv-portal/internal/erp"
	"erp-cv-portal/internal/progress"
	"erp-cv-portal/internal/resumedata"
)

type ERPOptions struct {
	Variant      int
	JSONPath     string
	Output       string
	SecretsDir   string
	BaseURL      string
	DownloadOnly bool
	FreshLogin   bool
}

type ERPBrowserOptions struct {
	SecretsDir string
	BaseURL    string
	FreshLogin bool
	OpenURL    func(string) error
}

func RunERP(ctx context.Context, repoRoot string, options ERPOptions) error {
	resolve := func(path string) string {
		if filepath.IsAbs(path) {
			return path
		}
		return filepath.Join(repoRoot, path)
	}

	var fields map[string]string
	entryCount := 0
	if !options.DownloadOnly {
		progress.Logf("ERP CV: loading and validating %s", resolve(options.JSONPath))
		resume, err := resumedata.Load(resolve(options.JSONPath))
		if err != nil {
			return err
		}
		fields = resume.PortalFields()
		entryCount = len(resume.Entries)
		progress.Logf("ERP CV: mapped %d entries to ERP fields", entryCount)
	}

	secretsDir := resolve(options.SecretsDir)
	client, err := erp.New(options.BaseURL, secretsDir)
	if err != nil {
		return err
	}

	ctx, cancel := context.WithTimeout(ctx, 5*time.Minute)
	defer cancel()

	progress.Logf("ERP CV: authenticating with ERP")
	var authErr error
	if options.FreshLogin {
		authErr = client.AuthenticateFresh(ctx)
	} else {
		authErr = client.Authenticate(ctx)
	}
	if authErr != nil {
		return authErr
	}

	if !options.DownloadOnly {
		progress.Logf("ERP CV: synchronizing %d entries across CV1, CV2, and CV3", entryCount)
		if err := client.SyncResume(ctx, fields); err != nil {
			return err
		}
	}

	progress.Logf("ERP CV: downloading CV%d", options.Variant)
	pdf, err := client.DownloadPDF(ctx, options.Variant)
	if err != nil {
		return err
	}

	output := options.Output
	if output == "" {
		output = fmt.Sprintf("pdf/resume-erp-cv%d.pdf", options.Variant)
	}
	return erp.WritePDF(resolve(output), pdf)
}

func OpenERPBrowser(ctx context.Context, repoRoot string, options ERPBrowserOptions) error {
	if options.OpenURL == nil {
		return fmt.Errorf("open ERP browser: no browser opener configured")
	}
	resolve := func(path string) string {
		if filepath.IsAbs(path) {
			return path
		}
		return filepath.Join(repoRoot, path)
	}
	secretsDir := resolve(options.SecretsDir)
	client, err := erp.New(options.BaseURL, secretsDir)
	if err != nil {
		return err
	}

	ctx, cancel := context.WithTimeout(ctx, 5*time.Minute)
	defer cancel()
	if options.FreshLogin {
		progress.Logf("ERP browser auth: discarding the saved CLI session")
		if err := client.DiscardSavedSession(); err != nil {
			return err
		}
	}
	progress.Logf("ERP CV: preparing a fresh ERP browser handoff")
	browserURL, err := client.BrowserLoginURL(ctx)
	if err != nil {
		return err
	}
	progress.Logf("ERP CV: opening the fresh ERP session in the browser")
	if err := options.OpenURL(browserURL); err != nil {
		return fmt.Errorf("open ERP in browser: %w", err)
	}
	progress.Logf("ERP CV: browser open command completed")
	return nil
}
