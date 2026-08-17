package pdfutil

import "testing"

func TestValidate(t *testing.T) {
	valid := append([]byte("%PDF-1.4\n"), make([]byte, 80)...)
	valid = append(valid, []byte("\n%%EOF\n")...)
	if err := Validate(valid); err != nil {
		t.Fatalf("Validate(valid) returned %v", err)
	}

	for name, data := range map[string][]byte{
		"html":   []byte("<html><body>login</body></html>"),
		"short":  []byte("%PDF-1.4\n%%EOF"),
		"no EOF": append([]byte("%PDF-1.4\n"), make([]byte, 80)...),
	} {
		t.Run(name, func(t *testing.T) {
			if err := Validate(data); err == nil {
				t.Fatal("Validate() accepted invalid data")
			}
		})
	}
}
