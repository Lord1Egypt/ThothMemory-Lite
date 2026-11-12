package capture

import (
	"os"
	"testing"
	"time"
)

// A launcher that leaves stdin open and silent must not hang a lifecycle event.
func TestReadStdinGivesUpOnASilentOpenPipe(t *testing.T) {
	stdinWait = 100 * time.Millisecond
	t.Cleanup(func() { stdinWait = 2 * time.Second })
	r, w, e := os.Pipe()
	if e != nil {
		t.Fatal(e)
	}
	defer w.Close()
	start := time.Now()
	if b := ReadStdin(r); b != nil {
		t.Fatal("unexpected data", b)
	}
	if time.Since(start) > time.Second {
		t.Fatal("ReadStdin blocked on an open pipe")
	}
	r2, w2, _ := os.Pipe()
	_, _ = w2.Write([]byte(" {\"prompt\":\"x\"} \n"))
	w2.Close()
	if b := ReadStdin(r2); string(b) != `{"prompt":"x"}` {
		t.Fatalf("payload %q", b)
	}
}
