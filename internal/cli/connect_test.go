package cli

import (
	"encoding/json"
	"strings"
	"testing"
)

func TestOpenCodeEntryIsVersionAware(t *testing.T) {
	cmd := []string{"thoth-lite", "mcp-stdio"}
	for _, tc := range []struct {
		name, input, version, schema, err string
	}{
		{"legacy any version", `{"model":"custom/model","mcp":{"other":{"type":"local","command":["safe"]}}}`, "1.17.13", "legacy", ""},
		{"legacy unknown version", `{"mcp":{}}`, "", "legacy", ""},
		{"jsonc", "{ // comment\n \"model\":\"https://example.com/a//b\",\n \"mcp\":{},\n}", "1.19.5", "legacy", ""},
		{"v2 on 1.18.24", `{"providers":{"local":{}},"mcp":{"servers":{"other":{"type":"remote","url":"http://localhost:123"}}}}`, "1.18.24", "v2", ""},
		{"v2 on 1.19.5", `{"mcp":{"servers":{}}}`, "opencode 1.19.5", "v2", ""},
		{"v2 on 1.18.23", `{"mcp":{"servers":{}}}`, "1.18.23", "", "rejects"},
		{"v2 on 1.17.13", `{"providers":{},"mcp":{"servers":{}}}`, "1.17.13", "", "rejects"},
		{"v2 unknown version", `{"mcp":{"servers":{}}}`, "", "", "version is unknown"},
	} {
		t.Run(tc.name, func(t *testing.T) {
			m, e := JSONC([]byte(tc.input))
			if e != nil {
				t.Fatal(e)
			}
			schema, keys, entry, e := OpenCodeEntry(m, tc.version, cmd)
			if tc.err != "" {
				if e == nil || !strings.Contains(e.Error(), tc.err) {
					t.Fatalf("want error %q, got %v", tc.err, e)
				}
				return
			}
			if e != nil || schema != tc.schema {
				t.Fatal(schema, e)
			}
			if schema == "v2" && (strings.Join(keys, ".") != "mcp.servers" || entry["disabled"] != false) {
				t.Fatal("v2 shape", keys, entry)
			}
			if schema == "legacy" && (strings.Join(keys, ".") != "mcp" || entry["enabled"] != true) {
				t.Fatal("legacy shape", keys, entry)
			}
			b, _ := json.Marshal(entry)
			if !strings.Contains(string(b), `"type":"local"`) {
				t.Fatal(string(b))
			}
		})
	}
}
func TestJSONCDoesNotCorruptStrings(t *testing.T) {
	m, e := JSONC([]byte(`{"url":"https://host/path/*a*/","quoted":"a\"//b",/* hi */"tail":[1,2,],}`))
	if e != nil {
		t.Fatal(e)
	}
	if m["url"] != "https://host/path/*a*/" {
		t.Fatal(m)
	}
}

func TestCodexQuotedAndInlineRegistration(t *testing.T) {
	for _, input := range []string{"# keep this comment\n[mcp_servers.\"thothmemory\"]\ncommand='thothmemory'\nargs=['bridge','--config','client.json']\nstartup_timeout_sec=30\n", "mcp_servers = {thothmemory = {command='thothmemory', args=['bridge','--config','client.json']}}\n"} {
		out, e := MergeCodex([]byte(input), "thothmemory", "thothmemory", []string{"bridge", "--config", "client.json"})
		if e != nil || string(out) != input {
			t.Fatal("existing TOML registration not preserved", e, string(out))
		}
	}
	original := "# preserve comment\nmodel='custom'\n[mcp_servers.other]\ncommand='other'\n"
	out, e := MergeCodex([]byte(original), "thothmemory", "thothmemory", []string{"bridge", "--config", "client.json"})
	if e != nil || !strings.HasPrefix(string(out), original) {
		t.Fatal(e)
	}
	again, e := MergeCodex(out, "thothmemory", "thothmemory", []string{"bridge", "--config", "client.json"})
	if e != nil || string(again) != string(out) {
		t.Fatal("not idempotent", e)
	}
	if _, e = MergeCodex(out, "thothmemory", "different", []string{"bridge", "--config", "client.json"}); e == nil {
		t.Fatal("conflicting registration overwritten")
	}
}

func TestCodexExtendsInlineOtherServer(t *testing.T) {
	out, e := MergeCodex([]byte("model='custom'\nmcp_servers={other={command='safe'}}\n"), "thothmemory", "thothmemory", []string{"bridge", "--config", "client.json"})
	if e != nil || !strings.Contains(string(out), "safe") || !strings.Contains(string(out), "thothmemory") || !strings.Contains(string(out), "custom") {
		t.Fatal(e, string(out))
	}
}

func TestOpenCodeNewFileUsesV2OnOpenCode2(t *testing.T) {
	schema, keys, _, e := OpenCodeEntry(map[string]any{}, "opencode v2.2.7", []string{"x"})
	if e != nil || schema != "v2" || strings.Join(keys, ".") != "mcp.servers" {
		t.Fatal(schema, keys, e)
	}
	schema, _, _, _ = OpenCodeEntry(map[string]any{"model": "a"}, "2.2.7", []string{"x"})
	if schema != "legacy" {
		t.Fatal("existing legacy file must keep its shape", schema)
	}
}
