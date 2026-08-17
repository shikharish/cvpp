package main

import (
	"bufio"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net/http"
	"net/url"
	"os"
	"os/exec"
	"path/filepath"
	"runtime"
	"sort"
	"strings"
	"time"
)

const erpBaseURL = "https://erp.iitkgp.ac.in"

type credentials struct {
	RollNumber string            `json:"roll_number"`
	Password   string            `json:"password"`
	Answers    map[string]string `json:"answers"`
}

func main() {
	if err := run(); err != nil {
		fmt.Fprintln(os.Stderr, "setup error:", err)
		os.Exit(1)
	}
}

func run() error {
	root, err := os.Getwd()
	if err != nil {
		return err
	}
	reader := bufio.NewReader(os.Stdin)

	if err := ensureDir(filepath.Join(root, "data")); err != nil {
		return err
	}
	if err := ensureDir(filepath.Join(root, ".erp-cv-secrets")); err != nil {
		return err
	}
	if err := ensureDir(filepath.Join(root, "pdf")); err != nil {
		return err
	}

	if err := ensureResumeJSON(root, reader); err != nil {
		return err
	}
	if err := ensureCredentials(root, reader); err != nil {
		return err
	}
	if err := buildBinary(root); err != nil {
		return err
	}

	fmt.Println()
	fmt.Println("Setup complete.")
	fmt.Println()
	fmt.Println("Recommended workflow:")
	fmt.Println("  Terminal 1:")
	fmt.Println("    ./scripts/watch-cv1.sh")
	fmt.Println()
	fmt.Println("  Terminal 2:")
	fmt.Println("    ./erp-cv-portal editor")
	fmt.Println()
	fmt.Println("Keep Terminal 1 open. Every saved change to data/resume.json will sync CV1 and download pdf/resume-erp-cv1.pdf.")
	fmt.Println("The ERP OTP prompt will appear in Terminal 1.")
	return nil
}

func ensureDir(path string) error {
	if err := os.MkdirAll(path, 0o755); err != nil {
		return err
	}
	return nil
}

func ensureResumeJSON(root string, reader *bufio.Reader) error {
	target := filepath.Join(root, "data", "resume.json")
	if _, err := os.Stat(target); err == nil {
		fmt.Println("Keeping existing data/resume.json")
		return nil
	} else if !errors.Is(err, os.ErrNotExist) {
		return err
	}

	source := filepath.Join(root, "data", "resume.example.json")
	if err := copyFile(source, target, 0o644); err != nil {
		return err
	}
	fmt.Println("Created data/resume.json from data/resume.example.json")
	return nil
}

func ensureCredentials(root string, reader *bufio.Reader) error {
	path := filepath.Join(root, ".erp-cv-secrets", "erpcreds.json")
	existing, err := readCredentials(path)
	if err != nil && !errors.Is(err, os.ErrNotExist) {
		return err
	}

	needsInput := errors.Is(err, os.ErrNotExist) || credentialsNeedInput(existing)
	if !needsInput {
		update, err := promptYesNo(reader, "ERP credentials already exist. Update them now? [y/N]: ", false)
		if err != nil {
			return err
		}
		if !update {
			fmt.Println("Keeping existing .erp-cv-secrets/erpcreds.json")
			return nil
		}
	}

	fmt.Println()
	fmt.Println("ERP credentials")
	fmt.Println("These are stored only in .erp-cv-secrets/erpcreds.json, which is ignored by git.")

	creds := existing
	if creds.Answers == nil {
		creds.Answers = map[string]string{}
	}

	roll, err := promptRequired(reader, "ERP roll number", cleanPlaceholder(creds.RollNumber))
	if err != nil {
		return err
	}
	creds.RollNumber = roll

	detectedQuestion, err := fetchSecurityQuestion(roll)
	if err != nil {
		fmt.Printf("Could not fetch the ERP security question automatically: %v\n", err)
		fmt.Println("You can enter the question text manually.")
	} else {
		fmt.Println("Fetched ERP security question.")
	}

	password, err := promptSecret("ERP password", cleanPlaceholder(creds.Password), reader)
	if err != nil {
		return err
	}
	creds.Password = password

	answers, err := promptSecurityAnswers(reader, creds.Answers, detectedQuestion)
	if err != nil {
		return err
	}
	creds.Answers = answers

	if err := writeCredentials(path, creds); err != nil {
		return err
	}
	fmt.Println("Saved .erp-cv-secrets/erpcreds.json")
	return nil
}

func promptSecurityAnswers(reader *bufio.Reader, existing map[string]string, detectedQuestion string) (map[string]string, error) {
	answers := map[string]string{}
	keys := make([]string, 0, len(existing))
	for key := range existing {
		if cleanPlaceholder(key) != "" {
			keys = append(keys, key)
		}
	}
	sort.Strings(keys)

	defaultQuestion := strings.TrimSpace(detectedQuestion)
	for _, key := range keys {
		if defaultQuestion == "" {
			defaultQuestion = key
		}
		break
	}

	for {
		question, err := promptRequired(reader, "ERP security question text", defaultQuestion)
		if err != nil {
			return nil, err
		}
		currentAnswer := cleanPlaceholder(existing[question])
		answer, err := promptSecret("Answer for that question", currentAnswer, reader)
		if err != nil {
			return nil, err
		}
		answers[question] = answer

		more, err := promptYesNo(reader, "Add another possible ERP security question? [y/N]: ", false)
		if err != nil {
			return nil, err
		}
		if !more {
			break
		}
		defaultQuestion = ""
	}
	return answers, nil
}

func fetchSecurityQuestion(rollNumber string) (string, error) {
	client := http.Client{Timeout: 20 * time.Second}
	response, err := client.PostForm(erpBaseURL+"/SSOAdministration/getSecurityQues.htm", url.Values{
		"user_id": {rollNumber},
	})
	if err != nil {
		return "", err
	}
	defer response.Body.Close()
	if response.StatusCode < 200 || response.StatusCode >= 300 {
		return "", fmt.Errorf("ERP returned %s", response.Status)
	}
	data, err := io.ReadAll(io.LimitReader(response.Body, 1<<20))
	if err != nil {
		return "", err
	}
	question := strings.TrimSpace(string(data))
	if question == "" {
		return "", fmt.Errorf("ERP returned an empty question")
	}
	return question, nil
}

func buildBinary(root string) error {
	fmt.Println()
	fmt.Println("Building ./erp-cv-portal")
	command := exec.Command("go", "build", "-o", "erp-cv-portal", "./cmd/erp-cv-portal")
	command.Dir = root
	command.Stdout = os.Stdout
	command.Stderr = os.Stderr
	if err := command.Run(); err != nil {
		return fmt.Errorf("go build failed: %w", err)
	}
	return nil
}

func readCredentials(path string) (credentials, error) {
	var creds credentials
	data, err := os.ReadFile(path)
	if err != nil {
		return creds, err
	}
	if err := json.Unmarshal(data, &creds); err != nil {
		return creds, fmt.Errorf("parse %s: %w", path, err)
	}
	return creds, nil
}

func writeCredentials(path string, creds credentials) error {
	data, err := json.MarshalIndent(creds, "", "  ")
	if err != nil {
		return err
	}
	data = append(data, '\n')
	if err := os.WriteFile(path, data, 0o600); err != nil {
		return err
	}
	if runtime.GOOS != "windows" {
		_ = os.Chmod(path, 0o600)
	}
	return nil
}

func credentialsNeedInput(creds credentials) bool {
	if cleanPlaceholder(creds.RollNumber) == "" || cleanPlaceholder(creds.Password) == "" {
		return true
	}
	if len(creds.Answers) == 0 {
		return true
	}
	for question, answer := range creds.Answers {
		if cleanPlaceholder(question) == "" || cleanPlaceholder(answer) == "" {
			return true
		}
	}
	return false
}

func cleanPlaceholder(value string) string {
	value = strings.TrimSpace(value)
	upper := strings.ToUpper(value)
	if value == "" || strings.Contains(upper, "YOUR_") || strings.Contains(upper, "EXACT SECURITY QUESTION") || value == "23XX00000" {
		return ""
	}
	return value
}

func promptRequired(reader *bufio.Reader, label, current string) (string, error) {
	for {
		value, err := prompt(reader, label, current)
		if err != nil {
			return "", err
		}
		if strings.TrimSpace(value) != "" {
			return strings.TrimSpace(value), nil
		}
		fmt.Println("This value is required.")
	}
}

func prompt(reader *bufio.Reader, label, current string) (string, error) {
	if current != "" {
		fmt.Printf("%s [%s]: ", label, current)
	} else {
		fmt.Printf("%s: ", label)
	}
	line, err := reader.ReadString('\n')
	if err != nil && !errors.Is(err, io.EOF) {
		return "", err
	}
	value := strings.TrimSpace(line)
	if value == "" {
		return current, nil
	}
	return value, nil
}

func promptSecret(label, current string, reader *bufio.Reader) (string, error) {
	for {
		if current != "" {
			fmt.Printf("%s [press Enter to keep existing]: ", label)
		} else {
			fmt.Printf("%s: ", label)
		}
		value, err := readSecretLine(reader)
		if err != nil {
			return "", err
		}
		value = strings.TrimSpace(value)
		if value == "" {
			value = current
		}
		if value != "" {
			return value, nil
		}
		fmt.Println("This value is required.")
	}
}

func readSecretLine(reader *bufio.Reader) (string, error) {
	if runtime.GOOS != "windows" {
		disableEcho := exec.Command("stty", "-echo")
		disableEcho.Stdin = os.Stdin
		_ = disableEcho.Run()
		defer func() {
			enableEcho := exec.Command("stty", "echo")
			enableEcho.Stdin = os.Stdin
			_ = enableEcho.Run()
			fmt.Println()
		}()
	}
	line, err := reader.ReadString('\n')
	if err != nil && !errors.Is(err, io.EOF) {
		return "", err
	}
	return line, nil
}

func promptYesNo(reader *bufio.Reader, question string, defaultYes bool) (bool, error) {
	for {
		fmt.Print(question)
		line, err := reader.ReadString('\n')
		if err != nil && !errors.Is(err, io.EOF) {
			return false, err
		}
		value := strings.ToLower(strings.TrimSpace(line))
		if value == "" {
			return defaultYes, nil
		}
		switch value {
		case "y", "yes":
			return true, nil
		case "n", "no":
			return false, nil
		default:
			fmt.Println("Enter y or n.")
		}
	}
}

func copyFile(source, target string, mode os.FileMode) error {
	input, err := os.Open(source)
	if err != nil {
		return err
	}
	defer input.Close()

	output, err := os.OpenFile(target, os.O_WRONLY|os.O_CREATE|os.O_EXCL, mode)
	if err != nil {
		return err
	}
	defer output.Close()

	if _, err := io.Copy(output, input); err != nil {
		return err
	}
	return output.Close()
}
