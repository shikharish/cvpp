package appdata

import (
	"crypto/rand"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"os"
	"path/filepath"
	"runtime"
	"strings"
	"time"
)

// Paths is the complete on-disk layout used by the portable application.  A
// caller may provide a custom root for tests and advanced CLI workflows; the
// normal app root is os.UserConfigDir()/cvpp.
type Paths struct {
	Root         string
	DataDir      string
	SecretsDir   string
	PDFDir       string
	BackupsDir   string
	RuntimeDir   string
	LogsDir      string
	ResumeJSON   string
	Credentials  string
	Session      string
	RuntimeState string
}

func DefaultRoot() (string, error) {
	config, err := os.UserConfigDir()
	if err != nil {
		return "", fmt.Errorf("resolve user config directory: %w", err)
	}
	return filepath.Join(config, "cvpp"), nil
}

func Resolve(root string) (Paths, error) {
	if strings.TrimSpace(root) == "" {
		var err error
		root, err = DefaultRoot()
		if err != nil {
			return Paths{}, err
		}
	}
	root, err := filepath.Abs(root)
	if err != nil {
		return Paths{}, fmt.Errorf("resolve app data path: %w", err)
	}
	p := Paths{
		Root:       root,
		DataDir:    filepath.Join(root, "data"),
		SecretsDir: filepath.Join(root, "secrets"),
		PDFDir:     filepath.Join(root, "pdf"),
		BackupsDir: filepath.Join(root, "backups"),
		RuntimeDir: filepath.Join(root, "runtime"),
		LogsDir:    filepath.Join(root, "logs"),
	}
	p.ResumeJSON = filepath.Join(p.DataDir, "resume.json")
	p.Credentials = filepath.Join(p.SecretsDir, "erpcreds.json")
	p.Session = filepath.Join(p.SecretsDir, ".session")
	p.RuntimeState = filepath.Join(p.RuntimeDir, "state.json")
	return p, nil
}

func (p Paths) Ensure() error {
	for _, dir := range []string{p.Root, p.DataDir, p.SecretsDir, p.PDFDir, p.BackupsDir, p.RuntimeDir, p.LogsDir} {
		if err := os.MkdirAll(dir, 0o700); err != nil {
			return err
		}
		if runtime.GOOS != "windows" {
			_ = os.Chmod(dir, 0o700)
		}
	}
	return nil
}

func (p Paths) PDF(variant int) string {
	return filepath.Join(p.PDFDir, fmt.Sprintf("resume-erp-cv%d.pdf", variant))
}

func (p Paths) BackupName(now time.Time) string {
	return filepath.Join(p.BackupsDir, fmt.Sprintf("resume-%s.json", now.UTC().Format("20060102-150405.000000000Z")))
}

func AtomicWrite(path string, data []byte, mode os.FileMode) error {
	if err := os.MkdirAll(filepath.Dir(path), 0o700); err != nil {
		return err
	}
	tmp, err := os.CreateTemp(filepath.Dir(path), ".cvpp-write-*")
	if err != nil {
		return err
	}
	tmpPath := tmp.Name()
	defer os.Remove(tmpPath)
	if err := tmp.Chmod(mode); err != nil {
		_ = tmp.Close()
		return err
	}
	if _, err := tmp.Write(data); err != nil {
		_ = tmp.Close()
		return err
	}
	if err := tmp.Sync(); err != nil {
		_ = tmp.Close()
		return err
	}
	if err := tmp.Close(); err != nil {
		return err
	}
	if err := os.Rename(tmpPath, path); err != nil {
		return err
	}
	if runtime.GOOS != "windows" {
		_ = os.Chmod(path, mode)
	}
	return nil
}

func RandomToken(bytes int) (string, error) {
	if bytes < 16 {
		bytes = 16
	}
	data := make([]byte, bytes)
	if _, err := io.ReadFull(rand.Reader, data); err != nil {
		return "", err
	}
	return hex.EncodeToString(data), nil
}

type RuntimeState struct {
	PID       int       `json:"pid"`
	Port      int       `json:"port"`
	StartedAt time.Time `json:"startedAt"`
	URL       string    `json:"url"`
}

func ReadRuntimeState(path string) (RuntimeState, error) {
	data, err := os.ReadFile(path)
	if err != nil {
		return RuntimeState{}, err
	}
	var state RuntimeState
	if err := json.Unmarshal(data, &state); err != nil {
		return RuntimeState{}, err
	}
	if state.PID <= 0 || state.Port <= 0 {
		return RuntimeState{}, errors.New("runtime state is incomplete")
	}
	return state, nil
}
