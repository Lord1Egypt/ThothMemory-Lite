package cli

import (
	"context"
	"crypto/rand"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"

	"github.com/Lord1Egypt/ThothMemory-Lite/internal/capture"
	"github.com/modelcontextprotocol/go-sdk/mcp"
)

func call(ctx context.Context, c *mcp.ClientSession, name string, args any) (any, error) {
	r, e := c.CallTool(ctx, &mcp.CallToolParams{Name: "memory_" + name, Arguments: args})
	if e != nil {
		return nil, e
	}
	if r.IsError {
		return nil, errors.New(r.Content[0].(*mcp.TextContent).Text)
	}
	var out struct {
		Data any `json:"data"`
	}
	for _, v := range r.Content {
		if text, ok := v.(*mcp.TextContent); ok {
			if e = json.Unmarshal([]byte(text.Text), &out); e == nil {
				return out.Data, nil
			}
		}
	}
	return nil, errors.New("missing tool data")
}
func operation() string { var b [16]byte; _, _ = rand.Read(b[:]); return hex.EncodeToString(b[:]) }

// CaptureCommand implements `capture status|set MODE|pending|retry` for any edition.
func CaptureCommand(d capture.Dir, args []string, connect func() (capture.Caller, func(), error)) error {
	if len(args) == 0 {
		args = []string{"status"}
	}
	switch args[0] {
	case "status":
		mode, e := d.Mode()
		if e != nil {
			return e
		}
		pending, rejected := capture.Pending(d)
		fmt.Printf("capture mode: %s\n  off       record nothing\n  metadata  lifecycle facts only (default once hooks are installed)\n  prompts   also store your request text as notes (opt-in)\npending writes: %d, rejected: %d (%s)\n", mode, pending, rejected, d)
		return nil
	case "set":
		if len(args) < 2 {
			return errors.New("capture set off|metadata|prompts [--yes]")
		}
		if args[1] == capture.ModePrompts && !(len(args) == 3 && args[2] == "--yes") {
			return errors.New("prompts mode stores the text you type to your agents (credential-looking prompts are skipped). Rerun with --yes to consent")
		}
		if e := d.SetMode(args[1]); e != nil {
			return e
		}
		fmt.Println("capture mode set to " + args[1] + ". Existing memory is untouched.")
		return nil
	case "retry", "pending":
		pending, rejected := capture.Pending(d)
		if args[0] == "pending" || pending == 0 {
			fmt.Printf("pending writes: %d, rejected: %d\n", pending, rejected)
			return nil
		}
		c, closeFn, e := connect()
		if e != nil {
			return e
		}
		defer closeFn()
		sent, left, e := capture.Drain(context.Background(), c, d)
		fmt.Printf("replayed %d, still pending %d\n", sent, left)
		return e
	}
	return errors.New("capture status|set MODE|pending|retry")
}
