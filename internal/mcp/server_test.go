package mcp

import (
	"bytes"
	"context"
	"encoding/json"
	"strings"
	"testing"
)

func runOne(t *testing.T, srv *Server, payload string) map[string]any {
	t.Helper()
	var out bytes.Buffer
	if err := srv.Serve(context.Background(), strings.NewReader(payload+"\n"), &out); err != nil {
		t.Fatalf("Serve: %v", err)
	}
	var got map[string]any
	if err := json.Unmarshal(out.Bytes(), &got); err != nil {
		t.Fatalf("unmarshal: %v (raw=%s)", err, out.String())
	}
	return got
}

func TestInitialize(t *testing.T) {
	srv := New(nil)
	got := runOne(t, srv, `{"jsonrpc":"2.0","id":1,"method":"initialize"}`)
	result, ok := got["result"].(map[string]any)
	if !ok {
		t.Fatalf("missing result: %+v", got)
	}
	if _, ok := result["protocolVersion"].(string); !ok {
		t.Errorf("missing protocolVersion: %+v", result)
	}
}

func TestToolsList(t *testing.T) {
	srv := New([]Tool{
		{Name: "echo", Description: "echoes", InputSchema: map[string]any{"type": "object"}},
	})
	got := runOne(t, srv, `{"jsonrpc":"2.0","id":2,"method":"tools/list"}`)
	result := got["result"].(map[string]any)
	tools := result["tools"].([]any)
	if len(tools) != 1 {
		t.Fatalf("want 1 tool, got %d", len(tools))
	}
}

func TestToolsCallSuccess(t *testing.T) {
	srv := New([]Tool{{
		Name: "echo",
		Handler: func(ctx context.Context, args map[string]any) (any, error) {
			return args["msg"], nil
		},
	}})
	got := runOne(t, srv, `{"jsonrpc":"2.0","id":3,"method":"tools/call","params":{"name":"echo","arguments":{"msg":"hi"}}}`)
	result := got["result"].(map[string]any)
	content := result["content"].([]any)[0].(map[string]any)
	if content["text"] != "hi" {
		t.Errorf("got %+v", content)
	}
}

func TestToolsCallUnknown(t *testing.T) {
	srv := New([]Tool{})
	got := runOne(t, srv, `{"jsonrpc":"2.0","id":4,"method":"tools/call","params":{"name":"nope"}}`)
	if _, ok := got["error"]; !ok {
		t.Errorf("expected error, got %+v", got)
	}
}

func TestUnknownMethod(t *testing.T) {
	srv := New(nil)
	got := runOne(t, srv, `{"jsonrpc":"2.0","id":5,"method":"bogus"}`)
	if _, ok := got["error"]; !ok {
		t.Errorf("expected error, got %+v", got)
	}
}
