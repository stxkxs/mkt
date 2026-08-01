package mcp

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"io"
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

func TestToolsCallStructuredContent(t *testing.T) {
	// A tool returning a non-string value must yield structuredContent (so
	// an agent can parse it) plus the same JSON serialized in a text block.
	srv := New("test", "1.0").WithTools(Tool{
		Name:        "bar",
		Description: "returns a bar",
		InputSchema: map[string]any{"type": "object"},
		Handler: func(ctx context.Context, args map[string]any) (any, error) {
			return map[string]any{"open": 1.5, "close": 2.0}, nil
		},
	})
	called := runOne(t, srv, `{"jsonrpc":"2.0","id":12,"method":"tools/call","params":{"name":"bar","arguments":{}}}`)
	result := called["result"].(map[string]any)
	sc, ok := result["structuredContent"].(map[string]any)
	if !ok {
		t.Fatalf("missing structuredContent: %+v", result)
	}
	if sc["open"] != 1.5 || sc["close"] != 2.0 {
		t.Errorf("structuredContent = %+v", sc)
	}
	// The text block must carry the same JSON for backward compatibility.
	content := result["content"].([]any)[0].(map[string]any)
	if txt, _ := content["text"].(string); !strings.Contains(txt, `"open":1.5`) {
		t.Errorf("text block should carry JSON, got %q", content["text"])
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

// runStream feeds payload to Serve and decodes every response written, one
// JSON value per line, as a raw any (a batch reply decodes as []any).
func runStream(t *testing.T, srv *Server, payload string) []any {
	t.Helper()
	var out bytes.Buffer
	if err := srv.Serve(context.Background(), strings.NewReader(payload), &out); err != nil {
		t.Fatalf("Serve: %v", err)
	}
	var got []any
	dec := json.NewDecoder(bytes.NewReader(out.Bytes()))
	for {
		var v any
		err := dec.Decode(&v)
		if errors.Is(err, io.EOF) {
			return got
		}
		if err != nil {
			t.Fatalf("decode: %v (raw=%s)", err, out.String())
		}
		got = append(got, v)
	}
}

// errorOf returns the error object of a response, failing if there is none.
func errorOf(t *testing.T, got map[string]any) map[string]any {
	t.Helper()
	e, ok := got["error"].(map[string]any)
	if !ok {
		t.Fatalf("expected an error object, got %+v", got)
	}
	return e
}

func TestToolFailureIsResultWithIsError(t *testing.T) {
	// Per the MCP spec a tool that ran and failed reports through the
	// result with isError, so the calling model can read the message and
	// retry. It must NOT come back as a JSON-RPC protocol error.
	srv := New("test", "1.0").WithTools(Tool{
		Name:        "boom",
		Description: "always fails",
		InputSchema: map[string]any{"type": "object"},
		Handler: func(ctx context.Context, args map[string]any) (any, error) {
			return nil, errors.New("no data for NOPE")
		},
	})
	got := runOne(t, srv, `{"jsonrpc":"2.0","id":1,"method":"tools/call","params":{"name":"boom","arguments":{}}}`)
	if _, bad := got["error"]; bad {
		t.Fatalf("tool failure must not be a protocol error: %+v", got)
	}
	result, ok := got["result"].(map[string]any)
	if !ok {
		t.Fatalf("missing result: %+v", got)
	}
	if result["isError"] != true {
		t.Errorf("isError = %v, want true (result=%+v)", result["isError"], result)
	}
	content := result["content"].([]any)[0].(map[string]any)
	if content["type"] != "text" || content["text"] != "no data for NOPE" {
		t.Errorf("content = %+v, want the handler's message as text", content)
	}
	if _, leaked := result["structuredContent"]; leaked {
		t.Errorf("an error result should carry no structuredContent: %+v", result)
	}
}

func TestSuccessfulToolResultHasNoIsError(t *testing.T) {
	srv := New("test", "1.0").WithTools(Tool{
		Name: "ok", InputSchema: map[string]any{"type": "object"},
		Handler: func(ctx context.Context, args map[string]any) (any, error) { return "fine", nil },
	})
	got := runOne(t, srv, `{"jsonrpc":"2.0","id":1,"method":"tools/call","params":{"name":"ok","arguments":{}}}`)
	result := got["result"].(map[string]any)
	if _, present := result["isError"]; present {
		t.Errorf("success must not set isError: %+v", result)
	}
}

func TestUnknownToolIsProtocolError(t *testing.T) {
	// Asking for a tool that was never listed is a malformed request, so it
	// stays a JSON-RPC error — the other half of the isError contract.
	srv := New("test", "1.0")
	got := runOne(t, srv, `{"jsonrpc":"2.0","id":2,"method":"tools/call","params":{"name":"ghost","arguments":{}}}`)
	if _, ok := got["result"]; ok {
		t.Fatalf("unknown tool must not return a result: %+v", got)
	}
	e := errorOf(t, got)
	if int(e["code"].(float64)) != errInvalidParams {
		t.Errorf("code = %v, want %d", e["code"], errInvalidParams)
	}
	if !strings.Contains(e["message"].(string), "ghost") {
		t.Errorf("message should name the tool, got %q", e["message"])
	}
}

func TestToolsCallInvalidParamsIsProtocolError(t *testing.T) {
	srv := New("test", "1.0")
	for _, payload := range []string{
		`{"jsonrpc":"2.0","id":3,"method":"tools/call"}`,
		`{"jsonrpc":"2.0","id":3,"method":"tools/call","params":"nope"}`,
	} {
		got := runOne(t, srv, payload)
		if int(errorOf(t, got)["code"].(float64)) != errInvalidParams {
			t.Errorf("%s: want invalid params, got %+v", payload, got)
		}
	}
}

// registrationOrder is deliberately not alphabetical so a sorted or a
// map-random listing both fail the determinism assertions below.
var registrationOrder = []string{"zeta", "alpha", "mike", "bravo", "yankee", "charlie", "xray", "delta"}

func orderedServer() *Server {
	srv := New("test", "1.0")
	for _, n := range registrationOrder {
		name := n
		srv.WithTools(Tool{Name: name, Description: name, InputSchema: map[string]any{"type": "object"},
			Handler: func(ctx context.Context, args map[string]any) (any, error) { return name, nil }})
		srv.WithResources(Resource{URI: "mkt://" + name, Name: name, MimeType: "text/plain",
			Handler: func(ctx context.Context) (string, error) { return name, nil }})
		srv.WithPrompts(Prompt{Name: name, Description: name,
			Handler: func(ctx context.Context, args map[string]string) (string, error) { return name, nil }})
	}
	return srv
}

func namesFrom(t *testing.T, got map[string]any, listKey, nameKey string) []string {
	t.Helper()
	items, ok := got["result"].(map[string]any)[listKey].([]any)
	if !ok {
		t.Fatalf("missing %q list in %+v", listKey, got)
	}
	names := make([]string, 0, len(items))
	for _, it := range items {
		names = append(names, it.(map[string]any)[nameKey].(string))
	}
	return names
}

func TestListingsAreDeterministicAcrossCalls(t *testing.T) {
	// Go randomizes map iteration, so listing straight off the registry map
	// returns a different order every call — user-visible churn for any
	// client that caches or diffs the listing.
	cases := []struct{ method, listKey, nameKey string }{
		{"tools/list", "tools", "name"},
		{"resources/list", "resources", "name"},
		{"prompts/list", "prompts", "name"},
	}
	for _, tc := range cases {
		t.Run(tc.method, func(t *testing.T) {
			srv := orderedServer()
			var first []string
			for i := 0; i < 25; i++ {
				got := runOne(t, srv, `{"jsonrpc":"2.0","id":1,"method":"`+tc.method+`"}`)
				names := namesFrom(t, got, tc.listKey, tc.nameKey)
				if i == 0 {
					first = names
					continue
				}
				if strings.Join(names, ",") != strings.Join(first, ",") {
					t.Fatalf("call %d ordering %v != first call %v", i, names, first)
				}
			}
			if strings.Join(first, ",") != strings.Join(registrationOrder, ",") {
				t.Errorf("ordering = %v, want registration order %v", first, registrationOrder)
			}
		})
	}
}

func TestReRegisteringKeepsPositionAndDoesNotDuplicate(t *testing.T) {
	srv := orderedServer()
	srv.WithTools(Tool{Name: "mike", Description: "replaced", InputSchema: map[string]any{"type": "object"},
		Handler: func(ctx context.Context, args map[string]any) (any, error) { return "v2", nil }})
	got := runOne(t, srv, `{"jsonrpc":"2.0","id":1,"method":"tools/list"}`)
	names := namesFrom(t, got, "tools", "name")
	if strings.Join(names, ",") != strings.Join(registrationOrder, ",") {
		t.Fatalf("re-registration moved or duplicated an entry: %v", names)
	}
	called := runOne(t, srv, `{"jsonrpc":"2.0","id":2,"method":"tools/call","params":{"name":"mike","arguments":{}}}`)
	content := called["result"].(map[string]any)["content"].([]any)[0].(map[string]any)
	if content["text"] != "v2" {
		t.Errorf("re-registration should replace the handler, got %+v", content)
	}
}

func TestEmptyListingsAreArraysNotNull(t *testing.T) {
	srv := New("test", "1.0")
	for _, tc := range []struct{ method, listKey string }{
		{"tools/list", "tools"},
		{"resources/list", "resources"},
		{"prompts/list", "prompts"},
	} {
		got := runOne(t, srv, `{"jsonrpc":"2.0","id":1,"method":"`+tc.method+`"}`)
		v, ok := got["result"].(map[string]any)[tc.listKey]
		if !ok || v == nil {
			t.Errorf("%s: %q must be [] not null, got %+v", tc.method, tc.listKey, got)
		}
	}
}

func TestPanickingToolDoesNotKillTheLoop(t *testing.T) {
	srv := New("test", "1.0").WithTools(Tool{
		Name: "panicky", InputSchema: map[string]any{"type": "object"},
		Handler: func(ctx context.Context, args map[string]any) (any, error) { panic("kaboom") },
	})
	out := runStream(t, srv,
		`{"jsonrpc":"2.0","id":1,"method":"tools/call","params":{"name":"panicky","arguments":{}}}`+"\n"+
			`{"jsonrpc":"2.0","id":2,"method":"ping"}`+"\n")
	if len(out) != 2 {
		t.Fatalf("expected 2 responses (the panic must not end the session), got %d: %+v", len(out), out)
	}
	first := out[0].(map[string]any)
	result, ok := first["result"].(map[string]any)
	if !ok || result["isError"] != true {
		t.Fatalf("panic should surface as an isError tool result, got %+v", first)
	}
	if txt := result["content"].([]any)[0].(map[string]any)["text"].(string); !strings.Contains(txt, "kaboom") {
		t.Errorf("panic value should reach the model, got %q", txt)
	}
	if _, ok := out[1].(map[string]any)["result"]; !ok {
		t.Errorf("the request after the panic should still be served: %+v", out[1])
	}
}

func TestPanickingResourceAndPromptAreContained(t *testing.T) {
	srv := New("test", "1.0").
		WithResources(Resource{URI: "mkt://boom", Name: "boom", MimeType: "text/plain",
			Handler: func(ctx context.Context) (string, error) { panic("res-boom") }}).
		WithPrompts(Prompt{Name: "boom",
			Handler: func(ctx context.Context, args map[string]string) (string, error) { panic("prompt-boom") }})
	out := runStream(t, srv,
		`{"jsonrpc":"2.0","id":1,"method":"resources/read","params":{"uri":"mkt://boom"}}`+"\n"+
			`{"jsonrpc":"2.0","id":2,"method":"prompts/get","params":{"name":"boom","arguments":{}}}`+"\n"+
			`{"jsonrpc":"2.0","id":3,"method":"ping"}`+"\n")
	if len(out) != 3 {
		t.Fatalf("expected 3 responses, got %d: %+v", len(out), out)
	}
	for i, want := range []string{"res-boom", "prompt-boom"} {
		e := errorOf(t, out[i].(map[string]any))
		if !strings.Contains(e["message"].(string), want) {
			t.Errorf("response %d: message %q should mention %q", i, e["message"], want)
		}
	}
}

func TestToolWithNilHandlerIsNotAPanic(t *testing.T) {
	srv := New("test", "1.0").WithTools(Tool{Name: "empty", InputSchema: map[string]any{"type": "object"}})
	got := runOne(t, srv, `{"jsonrpc":"2.0","id":1,"method":"tools/call","params":{"name":"empty","arguments":{}}}`)
	result, ok := got["result"].(map[string]any)
	if !ok || result["isError"] != true {
		t.Fatalf("a nil handler should report isError, got %+v", got)
	}
}

func TestNullIDIsTreatedAsANotification(t *testing.T) {
	// MCP forbids a null id on a real request; answering one produces a
	// response the client cannot match to anything.
	srv := New("test", "1.0")
	out := runStream(t, srv, `{"jsonrpc":"2.0","id":null,"method":"ping"}`+"\n")
	if len(out) != 0 {
		t.Errorf("null id should get no response, got %+v", out)
	}
}

func TestMalformedIDIsInvalidRequest(t *testing.T) {
	srv := New("test", "1.0")
	for _, id := range []string{`{"a":1}`, `[1]`, `true`} {
		got := runOne(t, srv, `{"jsonrpc":"2.0","id":`+id+`,"method":"ping"}`)
		e := errorOf(t, got)
		if int(e["code"].(float64)) != errInvalidRequest {
			t.Errorf("id=%s: code = %v, want %d", id, e["code"], errInvalidRequest)
		}
		if got["id"] != nil {
			t.Errorf("id=%s: response id must be null when the request id is unusable, got %v", id, got["id"])
		}
	}
}

func TestResponseEchoesIDVerbatim(t *testing.T) {
	srv := New("test", "1.0")
	if got := runOne(t, srv, `{"jsonrpc":"2.0","id":"abc-1","method":"ping"}`); got["id"] != "abc-1" {
		t.Errorf("string id: got %v", got["id"])
	}
	if got := runOne(t, srv, `{"jsonrpc":"2.0","id":42,"method":"ping"}`); got["id"] != float64(42) {
		t.Errorf("numeric id: got %v", got["id"])
	}
}

func TestParseErrorCarriesNullID(t *testing.T) {
	srv := New("test", "1.0")
	out := runStream(t, srv, "{not json\n")
	if len(out) != 1 {
		t.Fatalf("expected one parse error, got %+v", out)
	}
	got := out[0].(map[string]any)
	if _, ok := got["id"]; !ok {
		t.Errorf("JSON-RPC requires an id member on a parse error: %+v", got)
	}
	if got["id"] != nil {
		t.Errorf("parse error id = %v, want null", got["id"])
	}
	if int(errorOf(t, got)["code"].(float64)) != errParseError {
		t.Errorf("code = %+v, want %d", got["error"], errParseError)
	}
}

func TestBadJSONRPCVersionIsInvalidRequest(t *testing.T) {
	srv := New("test", "1.0")
	got := runOne(t, srv, `{"jsonrpc":"1.0","id":1,"method":"ping"}`)
	if int(errorOf(t, got)["code"].(float64)) != errInvalidRequest {
		t.Errorf("got %+v", got)
	}
	// An absent version is tolerated — some clients omit it and the intent
	// is unambiguous over an MCP transport.
	if ok := runOne(t, srv, `{"id":1,"method":"ping"}`); ok["result"] == nil {
		t.Errorf("absent jsonrpc should still be served, got %+v", ok)
	}
}

func TestMissingMethodIsInvalidRequest(t *testing.T) {
	srv := New("test", "1.0")
	got := runOne(t, srv, `{"jsonrpc":"2.0","id":1}`)
	if int(errorOf(t, got)["code"].(float64)) != errInvalidRequest {
		t.Errorf("got %+v", got)
	}
}

func TestUnknownMethodCode(t *testing.T) {
	srv := New("test", "1.0")
	got := runOne(t, srv, `{"jsonrpc":"2.0","id":1,"method":"bogus"}`)
	if int(errorOf(t, got)["code"].(float64)) != errMethodNotFound {
		t.Errorf("got %+v", got)
	}
}

func TestUnknownResourceUsesResourceNotFoundCode(t *testing.T) {
	srv := New("test", "1.0")
	got := runOne(t, srv, `{"jsonrpc":"2.0","id":1,"method":"resources/read","params":{"uri":"mkt://nope"}}`)
	if int(errorOf(t, got)["code"].(float64)) != errResourceNotFound {
		t.Errorf("code = %+v, want %d", got["error"], errResourceNotFound)
	}
}

func TestUnknownPromptIsInvalidParams(t *testing.T) {
	srv := New("test", "1.0")
	got := runOne(t, srv, `{"jsonrpc":"2.0","id":1,"method":"prompts/get","params":{"name":"nope"}}`)
	if int(errorOf(t, got)["code"].(float64)) != errInvalidParams {
		t.Errorf("got %+v", got)
	}
}

func TestBatchRequest(t *testing.T) {
	srv := New("test", "1.0").WithTools(Tool{
		Name: "echo", InputSchema: map[string]any{"type": "object"},
		Handler: func(ctx context.Context, args map[string]any) (any, error) { return args["msg"], nil },
	})
	out := runStream(t, srv, `[`+
		`{"jsonrpc":"2.0","id":1,"method":"ping"},`+
		`{"jsonrpc":"2.0","method":"notifications/initialized"},`+
		`{"jsonrpc":"2.0","id":2,"method":"tools/call","params":{"name":"echo","arguments":{"msg":"hi"}}}`+
		`]`+"\n")
	if len(out) != 1 {
		t.Fatalf("a batch gets exactly one reply line, got %d: %+v", len(out), out)
	}
	arr, ok := out[0].([]any)
	if !ok {
		t.Fatalf("batch reply must be an array, got %T: %+v", out[0], out[0])
	}
	if len(arr) != 2 {
		t.Fatalf("notification members contribute no response, want 2 got %d: %+v", len(arr), arr)
	}
	if arr[0].(map[string]any)["id"] != float64(1) || arr[1].(map[string]any)["id"] != float64(2) {
		t.Errorf("batch responses out of order: %+v", arr)
	}
}

func TestBatchOfOnlyNotificationsIsSilent(t *testing.T) {
	srv := New("test", "1.0")
	out := runStream(t, srv, `[{"jsonrpc":"2.0","method":"notifications/initialized"},{"jsonrpc":"2.0","method":"notifications/progress"}]`+"\n")
	if len(out) != 0 {
		t.Errorf("expected silence, got %+v", out)
	}
}

func TestEmptyBatchIsInvalidRequest(t *testing.T) {
	srv := New("test", "1.0")
	got := runOne(t, srv, `[]`)
	if int(errorOf(t, got)["code"].(float64)) != errInvalidRequest {
		t.Errorf("got %+v", got)
	}
}

func TestOversizeLineDoesNotKillTheLoop(t *testing.T) {
	srv := New("test", "1.0")
	huge := `{"jsonrpc":"2.0","id":1,"method":"ping","pad":"` + strings.Repeat("x", maxLineBytes) + `"}`
	out := runStream(t, srv, huge+"\n"+`{"jsonrpc":"2.0","id":2,"method":"ping"}`+"\n")
	if len(out) != 2 {
		t.Fatalf("expected a parse error then a served ping, got %d: %+v", len(out), out)
	}
	if int(errorOf(t, out[0].(map[string]any))["code"].(float64)) != errParseError {
		t.Errorf("oversize line should report a parse error, got %+v", out[0])
	}
	if _, ok := out[1].(map[string]any)["result"]; !ok {
		t.Errorf("session must survive an oversize frame: %+v", out[1])
	}
}

func TestBlankLinesAndTrailingLineWithoutNewline(t *testing.T) {
	srv := New("test", "1.0")
	out := runStream(t, srv, "\n  \n"+`{"jsonrpc":"2.0","id":1,"method":"ping"}`)
	if len(out) != 1 {
		t.Fatalf("expected one response, got %+v", out)
	}
}

func TestCancelledContextStopsServing(t *testing.T) {
	srv := New("test", "1.0")
	ctx, cancel := context.WithCancel(context.Background())
	cancel()
	var buf bytes.Buffer
	if err := srv.Serve(ctx, strings.NewReader(`{"jsonrpc":"2.0","id":1,"method":"ping"}`+"\n"), &buf); err != nil {
		t.Fatalf("Serve: %v", err)
	}
	if buf.Len() != 0 {
		t.Errorf("a cancelled context should stop dispatch, got %q", buf.String())
	}
}

// failWriter fails on the nth Write, standing in for a client that closed
// its end of the pipe mid-session.
type failWriter struct {
	n   int
	err error
}

func (f *failWriter) Write(p []byte) (int, error) {
	if f.n <= 0 {
		return 0, f.err
	}
	f.n--
	return len(p), nil
}

func TestServeReturnsWriteErrors(t *testing.T) {
	srv := New("test", "1.0")
	want := errors.New("pipe closed")
	err := srv.Serve(context.Background(), strings.NewReader(`{"jsonrpc":"2.0","id":1,"method":"ping"}`+"\n"), &failWriter{err: want})
	if !errors.Is(err, want) {
		t.Errorf("Serve = %v, want %v", err, want)
	}
	// The batch and oversize-frame paths write through the same encoder.
	if err := srv.Serve(context.Background(), strings.NewReader("{bad\n"), &failWriter{err: want}); !errors.Is(err, want) {
		t.Errorf("parse-error write: %v, want %v", err, want)
	}
	huge := strings.Repeat("x", maxLineBytes+1)
	if err := srv.Serve(context.Background(), strings.NewReader(huge+"\n"), &failWriter{err: want}); !errors.Is(err, want) {
		t.Errorf("oversize write: %v, want %v", err, want)
	}
}

// failReader fails after handing back one line.
type failReader struct {
	rest io.Reader
	err  error
}

func (f *failReader) Read(p []byte) (int, error) {
	if f.rest != nil {
		n, err := f.rest.Read(p)
		if errors.Is(err, io.EOF) {
			f.rest = nil
			if n > 0 {
				return n, nil
			}
			return 0, f.err
		}
		return n, err
	}
	return 0, f.err
}

func TestServeReturnsReadErrors(t *testing.T) {
	srv := New("test", "1.0")
	want := errors.New("stdin exploded")
	var out bytes.Buffer
	r := &failReader{rest: strings.NewReader(`{"jsonrpc":"2.0","id":1,"method":"ping"}` + "\n"), err: want}
	if err := srv.Serve(context.Background(), r, &out); !errors.Is(err, want) {
		t.Errorf("Serve = %v, want %v", err, want)
	}
	if out.Len() == 0 {
		t.Error("the request read before the failure should still have been answered")
	}
}

func TestBatchWithMalformedMembers(t *testing.T) {
	srv := New("test", "1.0")
	out := runStream(t, srv, `[1,{"jsonrpc":"2.0","id":2,"method":"ping"}]`+"\n")
	arr := out[0].([]any)
	if len(arr) != 2 {
		t.Fatalf("expected an error for the bad member plus the good reply, got %+v", arr)
	}
	if int(errorOf(t, arr[0].(map[string]any))["code"].(float64)) != errInvalidRequest {
		t.Errorf("malformed member: %+v", arr[0])
	}
	// A batch that isn't valid JSON at all is a parse error.
	got := runOne(t, srv, `[1,`)
	if int(errorOf(t, got)["code"].(float64)) != errParseError {
		t.Errorf("truncated batch: %+v", got)
	}
}

func TestNotificationsAreSilentEvenWhenMalformed(t *testing.T) {
	srv := New("test", "1.0").WithTools(Tool{
		Name: "panicky", InputSchema: map[string]any{"type": "object"},
		Handler: func(ctx context.Context, args map[string]any) (any, error) { panic("boom") },
	})
	for _, payload := range []string{
		`{"jsonrpc":"1.0","method":"ping"}`,           // bad version
		`{"jsonrpc":"2.0"}`,                           // no method
		`{"jsonrpc":"2.0","method":"resources/read"}`, // would error, but no id
	} {
		if out := runStream(t, srv, payload+"\n"); len(out) != 0 {
			t.Errorf("%s: expected silence, got %+v", payload, out)
		}
	}
}

func TestResourceAndPromptWithNilHandler(t *testing.T) {
	srv := New("test", "1.0").
		WithResources(Resource{URI: "mkt://empty", Name: "empty", MimeType: "text/plain"}).
		WithPrompts(Prompt{Name: "empty"})
	for _, payload := range []string{
		`{"jsonrpc":"2.0","id":1,"method":"resources/read","params":{"uri":"mkt://empty"}}`,
		`{"jsonrpc":"2.0","id":2,"method":"prompts/get","params":{"name":"empty"}}`,
	} {
		got := runOne(t, srv, payload)
		if !strings.Contains(errorOf(t, got)["message"].(string), "no handler") {
			t.Errorf("%s: %+v", payload, got)
		}
	}
}

func TestPromptsGetInvalidParams(t *testing.T) {
	srv := New("test", "1.0")
	got := runOne(t, srv, `{"jsonrpc":"2.0","id":1,"method":"prompts/get","params":"nope"}`)
	if int(errorOf(t, got)["code"].(float64)) != errInvalidParams {
		t.Errorf("got %+v", got)
	}
}

func TestUnserializableToolResultFallsBackToText(t *testing.T) {
	// A tool returning something JSON cannot encode must still produce a
	// usable result rather than an encoder failure mid-stream.
	srv := New("test", "1.0").WithTools(Tool{
		Name: "chan", InputSchema: map[string]any{"type": "object"},
		Handler: func(ctx context.Context, args map[string]any) (any, error) { return make(chan int), nil },
	})
	got := runOne(t, srv, `{"jsonrpc":"2.0","id":1,"method":"tools/call","params":{"name":"chan","arguments":{}}}`)
	result := got["result"].(map[string]any)
	if _, bad := result["structuredContent"]; bad {
		t.Errorf("an unencodable value must not reach structuredContent: %+v", result)
	}
	if txt := result["content"].([]any)[0].(map[string]any)["text"].(string); txt == "" {
		t.Errorf("expected a text fallback, got %+v", result)
	}
}

func TestPromptsListEmitsArgumentsArray(t *testing.T) {
	srv := New("test", "1.0").WithPrompts(Prompt{Name: "noargs", Description: "takes nothing",
		Handler: func(ctx context.Context, args map[string]string) (string, error) { return "", nil }})
	got := runOne(t, srv, `{"jsonrpc":"2.0","id":1,"method":"prompts/list"}`)
	p := got["result"].(map[string]any)["prompts"].([]any)[0].(map[string]any)
	if args, ok := p["arguments"].([]any); !ok || args == nil {
		t.Errorf("arguments must be [] not null: %+v", p)
	}
}
