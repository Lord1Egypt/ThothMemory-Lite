package cli

import (
	"os"
	"path/filepath"
	"regexp"
	"strings"
	"testing"

	thothmemory "github.com/Lord1Egypt/ThothMemory-Lite"
	"github.com/Lord1Egypt/ThothMemory-Lite/internal/mcptools"
)

func TestInstructionsPreserveExistingTextAndRefresh(t *testing.T) {
	dir := t.TempDir()
	original := "# My rules\nAlways use tabs.\n\n## Notes\nKeep this exactly."
	if err := os.WriteFile(filepath.Join(dir, "AGENTS.md"), []byte(original), 0600); err != nil {
		t.Fatal(err)
	}
	if err := Instructions([]string{"--dir", dir, "--yes"}); err != nil {
		t.Fatal(err)
	}
	agents, _ := os.ReadFile(filepath.Join(dir, "AGENTS.md"))
	claude, _ := os.ReadFile(filepath.Join(dir, "CLAUDE.md"))
	proto, _ := os.ReadFile(filepath.Join(dir, "THOTH_LITE_AGENT_PROTOCOL.md"))
	if !strings.HasPrefix(string(agents), original) {
		t.Fatalf("existing instructions altered:\n%s", agents)
	}
	if string(proto) != thothmemory.AgentProtocol || !strings.Contains(string(proto), "record_idea") {
		t.Fatal("protocol file not installed verbatim")
	}
	if strings.Contains(string(agents), "@THOTH_LITE") || !strings.Contains(string(claude), "@THOTH_LITE_AGENT_PROTOCOL.md") {
		t.Fatal("only CLAUDE.md should use the @import")
	}
	first := string(agents)
	if err := Instructions([]string{"--dir", dir, "--yes"}); err != nil {
		t.Fatal(err)
	}
	again, _ := os.ReadFile(filepath.Join(dir, "AGENTS.md"))
	if string(again) != first || strings.Count(string(again), blockBegin) != 1 {
		t.Fatal("repeat install is not idempotent")
	}
	// A hand edit outside the block survives a refresh; the block itself is replaced.
	edited := strings.Replace(first, "Keep this exactly.", "Keep this exactly.\nUser added line.", 1)
	edited = strings.Replace(edited, "Never store secrets.", "STALE TEXT", 1)
	if err := os.WriteFile(filepath.Join(dir, "AGENTS.md"), []byte(edited), 0600); err != nil {
		t.Fatal(err)
	}
	if err := Instructions([]string{"--dir", dir, "--yes"}); err != nil {
		t.Fatal(err)
	}
	refreshed, _ := os.ReadFile(filepath.Join(dir, "AGENTS.md"))
	if !strings.Contains(string(refreshed), "User added line.") || strings.Contains(string(refreshed), "STALE TEXT") || strings.Count(string(refreshed), blockBegin) != 1 {
		t.Fatalf("refresh damaged or kept stale block:\n%s", refreshed)
	}
}

func TestProtocolMatchesRepositoryFile(t *testing.T) {
	b, err := os.ReadFile("../../THOTH_LITE_AGENT_PROTOCOL.md")
	if err != nil || string(b) != thothmemory.AgentProtocol {
		t.Fatal("embedded protocol differs from THOTH_LITE_AGENT_PROTOCOL.md")
	}
	for _, tool := range []string{"memory_record_idea", "memory_record_note", "memory_get_project_context", "memory_start_session", "memory_checkpoint_session", "memory_finish_session", "memory_search", "memory_release_task", "memory_record_test_result"} {
		if !strings.Contains(thothmemory.AgentProtocol, tool) {
			t.Errorf("protocol never mentions %s", tool)
		}
	}
}

// The protocol may only name tools that exist: a fictional tool name would
// teach every agent a call that fails.
func TestProtocolNamesOnlyRealTools(t *testing.T) {
	real := map[string]bool{}
	for _, c := range mcptools.Contracts {
		real["memory_"+c.Name] = true
	}
	for _, name := range regexp.MustCompile("memory_[a-z_]+").FindAllString(thothmemory.AgentProtocol, -1) {
		if !real[name] {
			t.Errorf("protocol names %s, which is not a tool", name)
		}
	}
}

// Every client gets the protocol where it reads instructions, and the
// recording rules are present even where only the managed block is read.
func TestInstructionsReachEveryClient(t *testing.T) {
	dir := t.TempDir()
	if err := Instructions([]string{"--dir", dir, "--yes"}); err != nil {
		t.Fatal(err)
	}
	for _, f := range []string{"AGENTS.md", "CLAUDE.md", "GEMINI.md"} {
		b, _ := os.ReadFile(filepath.Join(dir, f))
		if !strings.Contains(string(b), "after every step that changes the state of the work") || !strings.Contains(string(b), "THOTH_LITE_AGENT_PROTOCOL.md") {
			t.Errorf("%s lacks the recording essentials", f)
		}
	}
	for _, f := range []string{".clinerules/thoth-lite.md", ".roo/rules/thoth-lite.md", ".cursor/rules/thoth-lite.mdc", ".continue/rules/thoth-lite.md"} {
		b, err := os.ReadFile(filepath.Join(dir, filepath.FromSlash(f)))
		if err != nil || !strings.Contains(string(b), thothmemory.AgentProtocol) {
			t.Errorf("%s does not carry the full protocol: %v", f, err)
		}
	}
	cursor, _ := os.ReadFile(filepath.Join(dir, ".cursor", "rules", "thoth-lite.mdc"))
	if !strings.HasPrefix(string(cursor), "---\n") || !strings.Contains(string(cursor), "alwaysApply: true") {
		t.Error("Cursor rule is not always applied")
	}
	// A rule file the user wrote is never overwritten.
	mine := filepath.Join(t.TempDir(), "proj")
	if err := os.MkdirAll(filepath.Join(mine, ".clinerules"), 0700); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(mine, ".clinerules", "thoth-lite.md"), []byte("my own rules"), 0600); err != nil {
		t.Fatal(err)
	}
	if err := Instructions([]string{"--dir", mine, "--clients", "cline", "--yes"}); err == nil {
		t.Fatal("overwrote a user-written rule file")
	}
	if b, _ := os.ReadFile(filepath.Join(mine, ".clinerules", "thoth-lite.md")); string(b) != "my own rules" {
		t.Fatal("user rule file changed")
	}
}

// Lite's managed block uses its own markers, so UltraLite's block survives.
func TestInstructionsCoexistWithUltraLiteBlock(t *testing.T) {
	dir := t.TempDir()
	ultra := "<!-- thothmemory:begin protocol v1 -->\nUltraLite block\n<!-- thothmemory:end protocol v1 -->\n"
	if err := os.WriteFile(filepath.Join(dir, "AGENTS.md"), []byte(ultra), 0600); err != nil {
		t.Fatal(err)
	}
	if err := Instructions([]string{"--dir", dir, "--clients", "codex", "--yes"}); err != nil {
		t.Fatal(err)
	}
	b, _ := os.ReadFile(filepath.Join(dir, "AGENTS.md"))
	if !strings.HasPrefix(string(b), ultra) || !strings.Contains(string(b), blockBegin) {
		t.Fatalf("UltraLite block not preserved:\n%s", b)
	}
}
