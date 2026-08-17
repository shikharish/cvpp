package main

import (
	"context"
	"flag"
	"fmt"
	"os"
	"path/filepath"
	"runtime"

	"erp-cv-portal/internal/editorserver"
	"erp-cv-portal/internal/erp"
	"erp-cv-portal/internal/workflow"
)

func usage() {
	fmt.Fprintln(os.Stderr, "Usage:")
	fmt.Fprintln(os.Stderr, "  ./erp-cv-portal editor [--addr 127.0.0.1:0] [--no-open]")
	fmt.Fprintln(os.Stderr, "  ./erp-cv-portal erp [--cv 1|2|3] [--json PATH] [--out PATH] [--download-only] [--fresh-login] [--secrets-dir PATH]")
	fmt.Fprintln(os.Stderr, "  ./erp-cv-portal erp --open [--fresh-login] [--secrets-dir PATH]")
}

func main() {
	if len(os.Args) < 2 {
		usage()
		os.Exit(2)
	}
	repoRoot, err := os.Getwd()
	if err != nil {
		fatal(err)
	}
	repoRoot, err = filepath.Abs(repoRoot)
	if err != nil {
		fatal(err)
	}

	switch os.Args[1] {
	case "editor":
		options := parseEditorFlags(os.Args[2:])
		fatal(runEditor(repoRoot, options))
	case "erp":
		options := parseERPFlags("erp", os.Args[2:])
		fatal(runERP(repoRoot, options))
	case "help", "-h", "--help":
		usage()
	default:
		usage()
		os.Exit(2)
	}
}

type editorOptions struct {
	addr       string
	jsonPath   string
	secretsDir string
	baseURL    string
	open       bool
}

type erpOptions struct {
	variant      int
	jsonPath     string
	output       string
	secretsDir   string
	baseURL      string
	downloadOnly bool
	freshLogin   bool
	open         bool
}

func parseEditorFlags(args []string) editorOptions {
	flags := flag.NewFlagSet("editor", flag.ExitOnError)
	options := editorOptions{open: true}
	flags.StringVar(&options.addr, "addr", "127.0.0.1:0", "local editor server address")
	flags.StringVar(&options.jsonPath, "json", "data/resume.json", "resume JSON path")
	flags.StringVar(&options.secretsDir, "secrets-dir", ".erp-cv-secrets", "ERP credentials and session directory")
	flags.StringVar(&options.baseURL, "base-url", erp.DefaultBaseURL, "ERP base URL")
	flags.BoolVar(&options.open, "open", true, "open the editor in the default browser")
	noOpen := flags.Bool("no-open", false, "print the editor URL without opening a browser")
	_ = flags.Parse(args)
	if *noOpen {
		options.open = false
	}
	return options
}

func parseERPFlags(name string, args []string) erpOptions {
	flags := flag.NewFlagSet(name, flag.ExitOnError)
	options := erpOptions{}
	flags.IntVar(&options.variant, "cv", 1, "ERP CV number to download (1, 2, or 3)")
	flags.StringVar(&options.jsonPath, "json", "data/resume.json", "resume JSON path")
	flags.StringVar(&options.output, "out", "", "ERP PDF output path")
	flags.StringVar(&options.secretsDir, "secrets-dir", ".erp-cv-secrets", "ERP credentials and session directory")
	flags.StringVar(&options.baseURL, "base-url", erp.DefaultBaseURL, "ERP base URL")
	flags.BoolVar(&options.downloadOnly, "download-only", false, "download the currently saved ERP CV without synchronizing JSON")
	flags.BoolVar(&options.freshLogin, "fresh-login", false, "discard the saved ERP session and authenticate again")
	flags.BoolVar(&options.open, "open", false, "open ERP in the browser with a fresh unconsumed login token")
	_ = flags.Parse(args)
	if options.variant < 1 || options.variant > 3 {
		fmt.Fprintln(os.Stderr, "error: --cv must be 1, 2, or 3")
		os.Exit(2)
	}
	if options.open && name != "erp" {
		fmt.Fprintln(os.Stderr, "error: --open is only supported by the erp command")
		os.Exit(2)
	}
	if options.open && options.downloadOnly {
		fmt.Fprintln(os.Stderr, "error: --open and --download-only cannot be used together")
		os.Exit(2)
	}
	if options.open && options.output != "" {
		fmt.Fprintln(os.Stderr, "error: --out cannot be used with --open")
		os.Exit(2)
	}
	return options
}

func runEditor(repoRoot string, options editorOptions) error {
	serverOptions := editorserver.Options{
		RepoRoot:   repoRoot,
		Addr:       options.addr,
		JSONPath:   options.jsonPath,
		SecretsDir: options.secretsDir,
		BaseURL:    options.baseURL,
		OpenERPURL: func(url string) error {
			return openBrowser(runtime.GOOS, url)
		},
	}
	if options.open {
		serverOptions.OpenURL = func(url string) error {
			return openEditorBrowser(runtime.GOOS, url)
		}
	}
	return editorserver.Serve(context.Background(), serverOptions)
}

func runERP(repoRoot string, options erpOptions) error {
	if !options.open {
		return workflow.RunERP(context.Background(), repoRoot, workflow.ERPOptions{
			Variant:      options.variant,
			JSONPath:     options.jsonPath,
			Output:       options.output,
			SecretsDir:   options.secretsDir,
			BaseURL:      options.baseURL,
			DownloadOnly: options.downloadOnly,
			FreshLogin:   options.freshLogin,
		})
	}
	return workflow.OpenERPBrowser(context.Background(), repoRoot, workflow.ERPBrowserOptions{
		SecretsDir: options.secretsDir,
		BaseURL:    options.baseURL,
		FreshLogin: options.freshLogin,
		OpenURL: func(url string) error {
			return openBrowser(runtime.GOOS, url)
		},
	})
}

func fatal(err error) {
	if err == nil {
		return
	}
	fmt.Fprintln(os.Stderr, "error:", err)
	os.Exit(1)
}
