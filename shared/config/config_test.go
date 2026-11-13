package config

import (
	"strings"
	"testing"
)

func TestEnvOrDefaultReturnsValueWhenSet(t *testing.T) {
	t.Setenv("SHARED_CONFIG_TEST_VALUE", "configured")

	if got := EnvOrDefault("SHARED_CONFIG_TEST_VALUE", "fallback"); got != "configured" {
		t.Fatalf("EnvOrDefault() = %q, want %q", got, "configured")
	}
}

func TestEnvOrDefaultReturnsFallbackWhenEmpty(t *testing.T) {
	t.Setenv("SHARED_CONFIG_TEST_VALUE", "")

	if got := EnvOrDefault("SHARED_CONFIG_TEST_VALUE", "fallback"); got != "fallback" {
		t.Fatalf("EnvOrDefault() = %q, want %q", got, "fallback")
	}
}

func TestRequireReportsAllMissingVariables(t *testing.T) {
	t.Setenv("SHARED_CONFIG_PRESENT", "value")
	t.Setenv("SHARED_CONFIG_MISSING_A", "")
	t.Setenv("SHARED_CONFIG_MISSING_B", "")

	err := Require("SHARED_CONFIG_PRESENT", "SHARED_CONFIG_MISSING_A", "SHARED_CONFIG_MISSING_B")
	if err == nil {
		t.Fatal("Require() succeeded with missing variables")
	}

	message := err.Error()
	for _, name := range []string{"SHARED_CONFIG_MISSING_A", "SHARED_CONFIG_MISSING_B"} {
		if !strings.Contains(message, name) {
			t.Fatalf("error %q does not mention %q", message, name)
		}
	}
	if strings.Contains(message, "SHARED_CONFIG_PRESENT") {
		t.Fatalf("error %q mentions a present variable", message)
	}
}

func TestRequireSucceedsWhenAllVariablesAreSet(t *testing.T) {
	t.Setenv("SHARED_CONFIG_PRESENT_A", "value")
	t.Setenv("SHARED_CONFIG_PRESENT_B", "value")

	if err := Require("SHARED_CONFIG_PRESENT_A", "SHARED_CONFIG_PRESENT_B"); err != nil {
		t.Fatalf("Require() = %v, want nil", err)
	}
}
