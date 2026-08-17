package workflow

import (
	"context"
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"time"

	"cvpp/internal/appdata"
	"cvpp/internal/erp"
	"cvpp/internal/progress"
	"cvpp/internal/resumedata"
)

type ERPOptions struct {
	Variant      int
	JSONPath     string
	Output       string
	SecretsDir   string
	BaseURL      string
	DownloadOnly bool
	FreshLogin   bool
	OTP          erp.OTPProvider
	Phase        func(string)
}

type ImportOptions struct {
	Paths      appdata.Paths
	BaseURL    string
	FreshLogin bool
	OTP        erp.OTPProvider
	Phase      func(string)
}

type ERPBrowserOptions struct {
	SecretsDir string
	BaseURL    string
	FreshLogin bool
	OpenURL    func(string) error
}

func RunERP(ctx context.Context, repoRoot string, options ERPOptions) error {
	phase := func(value string) {
		if options.Phase != nil {
			options.Phase(value)
		}
	}
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
	if options.OTP != nil {
		client.OTP = options.OTP
	}
	client.Phase = options.Phase

	ctx, cancel := context.WithTimeout(ctx, 5*time.Minute)
	defer cancel()

	progress.Logf("ERP CV: authenticating with ERP")
	phase("authenticating")
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
		phase("saving")
		progress.Logf("ERP CV: synchronizing %d entries across CV1, CV2, and CV3", entryCount)
		if err := client.SyncResume(ctx, fields); err != nil {
			return err
		}
	}

	phase("downloading")
	progress.Logf("ERP CV: downloading CV%d", options.Variant)
	pdf, err := client.DownloadPDF(ctx, options.Variant)
	if err != nil {
		return err
	}

	output := options.Output
	if output == "" {
		output = fmt.Sprintf("pdf/resume-erp-cv%d.pdf", options.Variant)
	}
	if err := erp.WritePDF(resolve(output), pdf); err != nil {
		return err
	}
	phase("pdf-updated")
	phase("done")
	return nil
}

// ImportPortal authenticates, reads StudentForm.jsp, converts it locally, and
// downloads CV1. It intentionally never calls SyncResume, so onboarding cannot
// overwrite a student's existing ERP content.
func ImportPortal(ctx context.Context, options ImportOptions) error {
	if err := options.Paths.Ensure(); err != nil {
		return err
	}
	phase := func(value string) {
		if options.Phase != nil {
			options.Phase(value)
		}
	}
	client, err := erp.New(options.BaseURL, options.Paths.SecretsDir)
	if err != nil {
		return err
	}
	if options.OTP != nil {
		client.OTP = options.OTP
	}
	client.Phase = options.Phase
	ctx, cancel := context.WithTimeout(ctx, 5*time.Minute)
	defer cancel()
	phase("authenticating")
	if options.FreshLogin {
		if err := client.AuthenticateFresh(ctx); err != nil {
			return err
		}
	} else if err := client.Authenticate(ctx); err != nil {
		return err
	}
	phase("importing")
	snapshot, err := client.FetchResumeForm(ctx)
	if err != nil {
		return err
	}
	resume, err := resumedata.ConvertPortalForm(snapshot.Values)
	if err != nil {
		return fmt.Errorf("convert ERP resume: %w", err)
	}
	data, err := json.MarshalIndent(resume, "", "  ")
	if err != nil {
		return err
	}
	data = append(data, '\n')
	// Download CV1 before changing the local JSON. An ERP outage or an empty
	// PDF therefore leaves the previous resume and recovery path untouched.
	phase("downloading")
	pdf, err := client.DownloadPDF(ctx, 1)
	if err != nil {
		return err
	}
	if err := appdata.AtomicWrite(options.Paths.PDF(1), pdf, 0o600); err != nil {
		return fmt.Errorf("save ERP PDF: %w", err)
	}
	if existing, readErr := os.ReadFile(options.Paths.ResumeJSON); readErr == nil {
		phase("saving")
		if err := appdata.AtomicWrite(options.Paths.BackupName(time.Now()), existing, 0o600); err != nil {
			return fmt.Errorf("backup existing resume: %w", err)
		}
	} else if !os.IsNotExist(readErr) {
		return fmt.Errorf("read existing resume for backup: %w", readErr)
	}
	phase("saving")
	if err := appdata.AtomicWrite(options.Paths.ResumeJSON, data, 0o600); err != nil {
		return fmt.Errorf("save imported resume: %w", err)
	}
	phase("pdf-updated")
	phase("done")
	return nil
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
