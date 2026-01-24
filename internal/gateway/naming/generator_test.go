package naming

import (
	"regexp"
	"testing"
)

func TestGenerateSandboxName(t *testing.T) {
	name := GenerateSandboxName()

	// Check length
	if len(name) != 12 {
		t.Errorf("expected length 12, got %d", len(name))
	}

	// Check DNS-safe (lowercase alphanumeric)
	if !regexp.MustCompile(`^[0-9a-z]+$`).MatchString(name) {
		t.Errorf("name contains invalid characters: %s", name)
	}
}

func TestGenerateSandboxNameUniqueness(t *testing.T) {
	seen := make(map[string]bool)
	iterations := 10000

	for i := 0; i < iterations; i++ {
		name := GenerateSandboxName()
		if seen[name] {
			t.Errorf("duplicate name generated: %s", name)
		}
		seen[name] = true
	}
}

func TestGenerateSandboxNameDNSCompliance(t *testing.T) {
	// DNS-1123 label: lowercase, alphanumeric, hyphens (not at start/end), max 63 chars
	dnsLabel := regexp.MustCompile(`^[a-z0-9]([a-z0-9-]*[a-z0-9])?$`)

	for i := 0; i < 100; i++ {
		name := GenerateSandboxName()
		if !dnsLabel.MatchString(name) {
			t.Errorf("name is not DNS-1123 compliant: %s", name)
		}
		if len(name) > 63 {
			t.Errorf("name exceeds 63 characters: %s", name)
		}
	}
}
