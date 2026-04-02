package app

import (
	"testing"

	"github.com/yuanyp8/bootstrapctl/internal/ui"
)

func TestParseLifecycleFlagsSupportsShortAliases(t *testing.T) {
	options, ok := parseLifecycleFlags(ui.NewConsole(), "plan", []string{"-i", "inventory.yaml", "-p", "profile.yaml", "-t", "20s"})
	if !ok {
		t.Fatalf("expected short aliases to parse successfully")
	}
	if options.InventoryPath != "inventory.yaml" {
		t.Fatalf("unexpected inventory path: %q", options.InventoryPath)
	}
	if options.ProfilePath != "profile.yaml" {
		t.Fatalf("unexpected profile path: %q", options.ProfilePath)
	}
	if options.Timeout.String() != "20s" {
		t.Fatalf("unexpected timeout: %s", options.Timeout)
	}
}

func TestParseScanFlagsSupportsAbbreviatedInventory(t *testing.T) {
	options, ok := parseScanFlags(ui.NewConsole(), []string{"--inv", "inventory.yaml"})
	if !ok {
		t.Fatalf("expected abbreviated inventory alias to parse successfully")
	}
	if options.InventoryPath != "inventory.yaml" {
		t.Fatalf("unexpected inventory path: %q", options.InventoryPath)
	}
}

func TestParseScanFlagsAcceptsOptionalProfileAlias(t *testing.T) {
	options, ok := parseScanFlags(ui.NewConsole(), []string{"-i", "inventory.yaml", "-p", "profile.yaml", "-t", "20s"})
	if !ok {
		t.Fatalf("expected scan flags with optional profile to parse successfully")
	}
	if options.InventoryPath != "inventory.yaml" {
		t.Fatalf("unexpected inventory path: %q", options.InventoryPath)
	}
	if options.ProfilePath != "profile.yaml" {
		t.Fatalf("unexpected profile path: %q", options.ProfilePath)
	}
	if options.Timeout.String() != "20s" {
		t.Fatalf("unexpected timeout: %s", options.Timeout)
	}
}
