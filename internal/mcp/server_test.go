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
	if out.Len() == 0 {
		return nil
	}
	var got map[string]any
	if err := json.Unmarshal(out.Bytes(), &got); err != nil {
		t.Fatalf("unmarshal: %v (raw=%s)", err, out.String())
	}
	return got
}

func TestInitialize(t *testing.T) {
	srv := New("test", "1.0")
	got := runOne(t, srv, `{"jsonrpc":"2.0","id":1,"method":"initialize"}`)
	result := got["result"].(map[string]any)
	if result["protocolVersion"].(string) != ProtocolVersion {
		t.Errorf("protocolVersion = %v, want %s", result["protocolVersion"], ProtocolVersion)
	}
	caps := result["capabilities"].(map[string]any)
	for _, k := range []string{"tools", "resources", "prompts", "logging"} {
		if _, ok := caps[k]; !ok {
			t.Errorf("missing capability %q", k)
		}
	}
	info := result["serverInfo"].(map[string]any)
	if info["name"] != "test" || info["version"] != "1.0" {
		t.Errorf("serverInfo = %+v", info)
	}
}

func TestInitializedNotificationSuppressed(t *testing.T) {
	srv := New("test", "1.0")
	var out bytes.Buffer
	if err := srv.Serve(context.Background(), strings.NewReader(`{"jsonrpc":"2.0","method":"notifications/initialized"}`+"\n"), &out); err != nil {
		t.Fatalf("Serve: %v", err)
	}
	if out.Len() != 0 {
		t.Errorf("notifications should produce no output, got %q", out.String())
	}
}

func TestPing(t *testing.T) {
	srv := New("test", "1.0")
	got := runOne(t, srv, `{"jsonrpc":"2.0","id":2,"method":"ping"}`)
	if _, ok := got["result"]; !ok {
		t.Errorf("ping should return empty result, got %+v", got)
	}
}

func TestToolsListAndCall(t *testing.T) {
	srv := New("test", "1.0").WithTools(Tool{
		Name:        "echo",
		Description: "echoes",
		InputSchema: map[string]any{"type": "object"},
		Handler: func(ctx context.Context, args map[string]any) (any, error) {
			return args["msg"], nil
		},
	})
	listed := runOne(t, srv, `{"jsonrpc":"2.0","id":3,"method":"tools/list"}`)
	tools := listed["result"].(map[string]any)["tools"].([]any)
	if len(tools) != 1 {
		t.Fatalf("expected 1 tool, got %d", len(tools))
	}
	called := runOne(t, srv, `{"jsonrpc":"2.0","id":4,"method":"tools/call","params":{"name":"echo","arguments":{"msg":"hi"}}}`)
	content := called["result"].(map[string]any)["content"].([]any)[0].(map[string]any)
	if content["text"] != "hi" {
		t.Errorf("got %+v", content)
	}
}

func TestResourcesListAndRead(t *testing.T) {
	srv := New("test", "1.0").WithResources(Resource{
		URI: "mkt://config", Name: "Config", Description: "current config",
		MimeType: "text/plain",
		Handler:  func(ctx context.Context) (string, error) { return "hello config", nil },
	})
	listed := runOne(t, srv, `{"jsonrpc":"2.0","id":5,"method":"resources/list"}`)
	rs := listed["result"].(map[string]any)["resources"].([]any)
	if len(rs) != 1 {
		t.Fatalf("expected 1 resource, got %d", len(rs))
	}
	read := runOne(t, srv, `{"jsonrpc":"2.0","id":6,"method":"resources/read","params":{"uri":"mkt://config"}}`)
	contents := read["result"].(map[string]any)["contents"].([]any)[0].(map[string]any)
	if contents["text"] != "hello config" {
		t.Errorf("got %+v", contents)
	}
}

func TestResourcesReadUnknown(t *testing.T) {
	srv := New("test", "1.0")
	got := runOne(t, srv, `{"jsonrpc":"2.0","id":7,"method":"resources/read","params":{"uri":"mkt://nope"}}`)
	if _, ok := got["error"]; !ok {
		t.Errorf("expected error, got %+v", got)
	}
}

func TestPromptsListAndGet(t *testing.T) {
	srv := New("test", "1.0").WithPrompts(Prompt{
		Name:        "greet",
		Description: "greet a name",
		Arguments:   []PromptArg{{Name: "name", Required: true}},
		Handler: func(ctx context.Context, args map[string]string) (string, error) {
			return "Hello, " + args["name"], nil
		},
	})
	listed := runOne(t, srv, `{"jsonrpc":"2.0","id":8,"method":"prompts/list"}`)
	ps := listed["result"].(map[string]any)["prompts"].([]any)
	if len(ps) != 1 {
		t.Fatalf("expected 1 prompt, got %d", len(ps))
	}
	got := runOne(t, srv, `{"jsonrpc":"2.0","id":9,"method":"prompts/get","params":{"name":"greet","arguments":{"name":"World"}}}`)
	msgs := got["result"].(map[string]any)["messages"].([]any)
	content := msgs[0].(map[string]any)["content"].(map[string]any)
	if content["text"] != "Hello, World" {
		t.Errorf("got %+v", content)
	}
}

func TestUnknownMethod(t *testing.T) {
	srv := New("test", "1.0")
	got := runOne(t, srv, `{"jsonrpc":"2.0","id":10,"method":"bogus"}`)
	if _, ok := got["error"]; !ok {
		t.Errorf("expected error, got %+v", got)
	}
}

func TestUnknownNotificationSuppressed(t *testing.T) {
	srv := New("test", "1.0")
	var out bytes.Buffer
	if err := srv.Serve(context.Background(), strings.NewReader(`{"jsonrpc":"2.0","method":"notifications/whatever"}`+"\n"), &out); err != nil {
		t.Fatalf("Serve: %v", err)
	}
	if out.Len() != 0 {
		t.Errorf("unknown notification should be silent, got %q", out.String())
	}
}

func TestLoggingSetLevelAck(t *testing.T) {
	srv := New("test", "1.0")
	got := runOne(t, srv, `{"jsonrpc":"2.0","id":11,"method":"logging/setLevel","params":{"level":"info"}}`)
	if _, ok := got["result"]; !ok {
		t.Errorf("setLevel should return ack, got %+v", got)
	}
}
