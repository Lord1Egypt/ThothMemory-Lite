package cli

import (
	"os"
	"os/exec"
	"path/filepath"
	"runtime"
	"testing"
)

func TestOpenCodeLifecycleFailureReported(t *testing.T) {
	node, err := exec.LookPath("node")
	if err != nil {
		t.Skip("Node unavailable; generated OpenCode plugin failure callback NOT executed")
	}
	root := t.TempDir()
	plugin := filepath.Join(root, "plugin.mjs")
	if err = os.WriteFile(plugin, []byte(OpenCodePluginArgs(filepath.Join(root, "missing-binary"), []string{"--data", "synthetic-data-dir"})), 0600); err != nil {
		t.Fatal(err)
	}
	harness := `import { pathToFileURL } from 'node:url';
import assert from 'node:assert/strict';
const {ThothMemory}=await import(pathToFileURL(process.argv[1]).href);
const logs=[];
const hooks=await ThothMemory({directory:process.argv[2],client:{app:{log:async item=>logs.push(item)}}});
await hooks.event({event:{type:'session.idle'}});
assert.equal(logs.length,1,'failed lifecycle metadata write was silently ignored');
assert.equal(logs[0].body.level,'warn');
assert.match(logs[0].body.message,/unsynced/i);
assert.doesNotMatch(JSON.stringify(logs),/missing-binary|synthetic-data-dir/);
console.log('OpenCode failed lifecycle callback reports sanitized unsynced warning: PASS');`
	out, err := exec.Command(node, "--input-type=module", "-e", harness, plugin, root).CombinedOutput()
	if err != nil {
		t.Fatalf("plugin failure reporting: %v\n%s", err, out)
	}
	t.Log(string(out))
}

// The OpenCode 2 plugin: correct module shape, never throws when memory is
// unavailable, fetches context once per session and records events, and the
// lifecycle child gets a closed stdin so it cannot hang (a fake lifecycle
// script below blocks on stdin and would time out otherwise).
func TestOpenCodeV2PluginContract(t *testing.T) {
	node, err := exec.LookPath("node")
	if err != nil {
		t.Skip("Node unavailable; generated OpenCode 2 plugin NOT executed")
	}
	if runtime.GOOS == "windows" {
		t.Skip("fake lifecycle script is POSIX sh")
	}
	root := t.TempDir()
	calls := filepath.Join(root, "calls.txt")
	fake := filepath.Join(root, "fake-thoth-lite")
	// Reads stdin to EOF (hangs if stdin stays open), then records its phase.
	if err = os.WriteFile(fake, []byte("#!/bin/sh\ncat >/dev/null\necho \"$3\" >> "+calls+"\necho CONTEXT-$3\n"), 0700); err != nil {
		t.Fatal(err)
	}
	plugin := filepath.Join(root, "index.mjs")
	if err = os.WriteFile(plugin, []byte(OpenCodePluginV2(fake, []string{"--data", "x"})), 0600); err != nil {
		t.Fatal(err)
	}
	missing := filepath.Join(root, "missing.mjs")
	if err = os.WriteFile(missing, []byte(OpenCodePluginV2(filepath.Join(root, "absent"), nil)), 0600); err != nil {
		t.Fatal(err)
	}
	harness := `import { pathToFileURL } from 'node:url';
import assert from 'node:assert/strict';
async function load(file) {
  const def = (await import(pathToFileURL(file).href)).default;
  assert.equal(typeof def.id, 'string'); assert.equal(typeof def.setup, 'function');
  const hooks = {}; let push;
  const events = { async *[Symbol.asyncIterator]() { while (true) { const e = await new Promise(r => push = r); if (!e) return; yield e; } } };
  const ctx = { location: { directory: process.argv[2] }, session: { hook: async (n, f) => { hooks[n] = f; } }, event: { subscribe: () => events } };
  const cleanup = await def.setup(ctx);
  assert.equal(typeof cleanup, 'function');
  return { hooks, emit: async (e) => { push(e); await new Promise(r => setTimeout(r, 300)); }, cleanup };
}
const p = await load(process.argv[1]);
const ev = { sessionID: 's1', system: [] };
await p.hooks.context(ev); await p.hooks.context({ sessionID: 's1', system: [] });
assert.match(ev.system[0].text, /CONTEXT-start/);
await p.hooks.compaction({ system: [] });
await p.emit({ type: 'session.execution.succeeded' });
p.cleanup();
const m = await load(process.argv[3]);
const ev2 = { sessionID: 's2', system: [] };
await m.hooks.context(ev2);
assert.match(ev2.system[0].text, /unavailable/);
await m.hooks.compaction({ system: [] });
m.cleanup();
console.log('OpenCode 2 plugin contract: PASS');
process.exit(0);`
	cmd := exec.Command(node, "--input-type=module", "-e", harness, plugin, root, missing)
	out, err := cmd.CombinedOutput()
	if err != nil {
		t.Fatalf("plugin contract: %v\n%s", err, out)
	}
	b, _ := os.ReadFile(calls)
	if string(b) != "start\ncheckpoint\nidle\n" {
		t.Fatalf("lifecycle phases recorded %q; want start once, checkpoint, idle", b)
	}
}
