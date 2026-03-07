package domain_test

import (
	"strings"
	"testing"

	"github.com/baochen10luo/stagenthand/internal/domain"
)

// --- ValidateProjectID ---

func TestValidateProjectID_Valid(t *testing.T) {
	cases := []string{"proj-001", "my_project", "abc123", "PROJECT"}
	for _, id := range cases {
		if err := domain.ValidateProjectID(id); err != nil {
			t.Errorf("ValidateProjectID(%q) unexpected error: %v", id, err)
		}
	}
}

func TestValidateProjectID_Empty(t *testing.T) {
	err := domain.ValidateProjectID("")
	if err == nil {
		t.Fatal("expected error for empty project_id")
	}
	if !strings.Contains(err.Error(), "project_id") {
		t.Errorf("error should mention field name, got: %v", err)
	}
}

func TestValidateProjectID_PathTraversal(t *testing.T) {
	cases := []string{
		"../secret",
		"../../etc/passwd",
		"foo/../bar",
	}
	for _, id := range cases {
		err := domain.ValidateProjectID(id)
		if err == nil {
			t.Errorf("ValidateProjectID(%q) expected error, got nil", id)
		}
	}
}

func TestValidateProjectID_URLEncodedTraversal(t *testing.T) {
	cases := []string{
		"%2e%2e%2fetc%2fpasswd",
		"%2E%2E/secret",
		"foo%2f%2e%2e%2fbar",
	}
	for _, id := range cases {
		err := domain.ValidateProjectID(id)
		if err == nil {
			t.Errorf("ValidateProjectID(%q) expected error for URL-encoded traversal", id)
		}
	}
}

func TestValidateProjectID_ControlChars(t *testing.T) {
	cases := []string{
		"proj\x00null",
		"proj\x01soh",
		"proj\x1funit-sep",
		"proj\x7fdel",
	}
	for _, id := range cases {
		err := domain.ValidateProjectID(id)
		if err == nil {
			t.Errorf("ValidateProjectID(%q) expected error for control char", id)
		}
	}
}

// Tab/newline/CR are allowed inside content strings but not usually in IDs.
// ValidateProjectID currently allows them (content permissive policy). This test
// documents the current behaviour; tighten if IDs require stricter rules.
func TestValidateProjectID_AllowsNewlineTabCR(t *testing.T) {
	// These don't contain path traversal or non-tab control chars, so pass.
	if err := domain.ValidateProjectID("proj\ttab"); err != nil {
		t.Errorf("unexpected error for tab in project_id: %v", err)
	}
}

// --- ValidateCharacterRefs ---

func TestValidateCharacterRefs_Valid(t *testing.T) {
	refs := []string{"characters/hero.png", "chars/villain.jpg"}
	if err := domain.ValidateCharacterRefs(refs); err != nil {
		t.Errorf("unexpected error: %v", err)
	}
}

func TestValidateCharacterRefs_PathTraversal(t *testing.T) {
	refs := []string{"chars/hero.png", "../etc/passwd"}
	err := domain.ValidateCharacterRefs(refs)
	if err == nil {
		t.Fatal("expected error for path traversal in refs")
	}
	if !strings.Contains(err.Error(), "character_refs[1]") {
		t.Errorf("error should identify offending index, got: %v", err)
	}
}

func TestValidateCharacterRefs_Empty(t *testing.T) {
	if err := domain.ValidateCharacterRefs(nil); err != nil {
		t.Errorf("nil refs should pass: %v", err)
	}
	if err := domain.ValidateCharacterRefs([]string{}); err != nil {
		t.Errorf("empty refs should pass: %v", err)
	}
}

// --- ValidatePanel ---

func TestValidatePanel_Valid(t *testing.T) {
	p := domain.Panel{
		Description:   "A hero stands at the gate.",
		Dialogue:      "I will protect them.",
		CharacterRefs: []string{"chars/hero.png"},
	}
	if err := domain.ValidatePanel(p); err != nil {
		t.Errorf("unexpected error: %v", err)
	}
}

func TestValidatePanel_MaliciousDescription(t *testing.T) {
	p := domain.Panel{
		Description: "Normal text\x00with null byte",
	}
	if err := domain.ValidatePanel(p); err == nil {
		t.Error("expected error for control char in description")
	}
}

func TestValidatePanel_MaliciousDialogue(t *testing.T) {
	p := domain.Panel{
		Dialogue: "Hello\x01world",
	}
	if err := domain.ValidatePanel(p); err == nil {
		t.Error("expected error for control char in dialogue")
	}
}

func TestValidatePanel_TraversalInRef(t *testing.T) {
	p := domain.Panel{
		CharacterRefs: []string{"../../evil.png"},
	}
	if err := domain.ValidatePanel(p); err == nil {
		t.Error("expected error for path traversal in character_refs")
	}
}

// --- ValidationError ---

func TestValidationError_Error(t *testing.T) {
	err := &domain.ValidationError{Field: "project_id", Message: "must not be empty"}
	msg := err.Error()
	if !strings.Contains(msg, "project_id") || !strings.Contains(msg, "must not be empty") {
		t.Errorf("Error() = %q, want field and message included", msg)
	}
}
