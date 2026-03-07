// Package domain defines pure data structures for StagentHand.
package domain

import (
	"fmt"
	"strings"
	"unicode"
)

// ValidationError represents a structured validation failure.
type ValidationError struct {
	Field   string
	Message string
}

func (e *ValidationError) Error() string {
	return fmt.Sprintf("validation error: field=%q %s", e.Field, e.Message)
}

// ValidateProjectID checks a project ID for path traversal and control characters.
func ValidateProjectID(id string) error {
	if id == "" {
		return &ValidationError{Field: "project_id", Message: "must not be empty"}
	}
	if err := checkPathTraversal("project_id", id); err != nil {
		return err
	}
	return checkControlChars("project_id", id)
}

// ValidateJobID checks a job ID for path traversal and control characters.
func ValidateJobID(id string) error {
	if id == "" {
		return &ValidationError{Field: "job_id", Message: "must not be empty"}
	}
	if err := checkPathTraversal("job_id", id); err != nil {
		return err
	}
	return checkControlChars("job_id", id)
}

// ValidateCharacterRefs checks character reference paths for path traversal and control chars.
func ValidateCharacterRefs(refs []string) error {
	for i, ref := range refs {
		field := fmt.Sprintf("character_refs[%d]", i)
		if err := checkPathTraversal(field, ref); err != nil {
			return err
		}
		if err := checkControlChars(field, ref); err != nil {
			return err
		}
	}
	return nil
}

// ValidatePanel validates a single Panel's user-supplied fields.
func ValidatePanel(p Panel) error {
	if err := ValidateCharacterRefs(p.CharacterRefs); err != nil {
		return err
	}
	if err := checkControlChars("description", p.Description); err != nil {
		return err
	}
	return checkControlChars("dialogue", p.Dialogue)
}

// checkPathTraversal detects "../", "..\", and URL-encoded variants (%2e%2e%2f).
func checkPathTraversal(field, value string) error {
	lower := strings.ToLower(value)
	if strings.Contains(lower, "../") || strings.Contains(lower, `..\ `) {
		return &ValidationError{Field: field, Message: `contains path traversal sequence "../"`}
	}
	// Decode common URL encodings and re-check.
	decoded := strings.ReplaceAll(lower, "%2e", ".")
	decoded = strings.ReplaceAll(decoded, "%2f", "/")
	decoded = strings.ReplaceAll(decoded, "%5c", `\`)
	if strings.Contains(decoded, "../") || strings.Contains(decoded, `..\ `) {
		return &ValidationError{Field: field, Message: "contains URL-encoded path traversal sequence"}
	}
	return nil
}

// checkControlChars rejects strings containing ASCII control characters (0x00–0x1F, 0x7F)
// except tab, newline, and carriage return which are valid in text content.
func checkControlChars(field, value string) error {
	for i, r := range value {
		if unicode.IsControl(r) && r != '\t' && r != '\n' && r != '\r' {
			return &ValidationError{
				Field:   field,
				Message: fmt.Sprintf("contains control character 0x%02X at index %d", r, i),
			}
		}
	}
	return nil
}
