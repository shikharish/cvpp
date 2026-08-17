package pdfutil

import (
	"bytes"
	"fmt"
	"os"
)

const minimumPDFSize = 64

func Validate(data []byte) error {
	if len(data) < minimumPDFSize {
		return fmt.Errorf("PDF response is too small (%d bytes)", len(data))
	}
	if !bytes.HasPrefix(data, []byte("%PDF-")) {
		return fmt.Errorf("response does not start with a PDF header")
	}
	if !bytes.Contains(data[len(data)/2:], []byte("%%EOF")) {
		return fmt.Errorf("response does not contain a PDF end marker")
	}
	return nil
}

func ValidateFile(path string) error {
	data, err := os.ReadFile(path)
	if err != nil {
		return err
	}
	return Validate(data)
}
