// Package mcp implements a Model Context Protocol server over stdio.
// Supports the initialization handshake, ping, tools, resources, and
// prompts — covering the read-only surface MCP clients (Claude Code,
// Claude Desktop, etc.) need to introspect a stateful CLI like mkt.
package mcp

import (
	"bufio"
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
)

// ProtocolVersion advertised in initialize responses.
const ProtocolVersion = "2025-03-26"

// Tool is one callable in the MCP server.
type Tool struct {
	Name        string                                                      `json:"name"`
	Description string                                                      `json:"description"`
	InputSchema map[string]any                                              `json:"inputSchema"`
	Handler     func(ctx context.Context, args map[string]any) (any, error) `json:"-"`
}

// Resource is an addressable piece of content the client can read.
// Handler returns the text content for the URI.
type Resource struct {
	URI         string                                    `json:"uri"`
	Name        string                                    `json:"name"`
	Description string                                    `json:"description"`
	MimeType    string                                    `json:"mimeType"`
	Handler     func(ctx context.Context) (string, error) `json:"-"`
}

// PromptArg describes one argument for a Prompt template.
type PromptArg struct {
	Name        string `json:"name"`
	Description string `json:"description,omitempty"`
	Required    bool   `json:"required,omitempty"`
}

// Prompt is a reusable user-facing template.
type Prompt struct {
	Name        string                                                            `json:"name"`
	Description string                                                            `json:"description"`
	Arguments   []PromptArg                                                       `json:"arguments,omitempty"`
	Handler     func(ctx context.Context, args map[string]string) (string, error) `json:"-"`
}

// Server is an MCP server bound to a set of tools, resources, and prompts.
//
// Each registry keeps a parallel slice of keys in registration order: Go
// map iteration is randomized, so listing straight off the map would hand
// a different ordering to every tools/list call. Clients cache and diff
// those listings, so the order has to be stable across calls and across
// process restarts.
type Server struct {
	tools     map[string]Tool
	toolOrder []string

	resources     map[string]Resource
	resourceOrder []string

	prompts     map[string]Prompt
	promptOrder []string

	name    string
	version string
}

// New constructs an empty Server. Use the With… registration helpers to
// populate it.
func New(name, version string) *Server {
	return &Server{
		tools:     map[string]Tool{},
		resources: map[string]Resource{},
		prompts:   map[string]Prompt{},
		name:      name,
		version:   version,
	}
}

// WithTools registers the given tools. tools/list reports them in
// registration order; re-registering a name replaces the definition in
// place without moving it.
func (s *Server) WithTools(tools ...Tool) *Server {
	for _, t := range tools {
		if _, dup := s.tools[t.Name]; !dup {
			s.toolOrder = append(s.toolOrder, t.Name)
		}
		s.tools[t.Name] = t
	}
	return s
}

// WithResources registers the given resources. resources/list reports
// them in registration order; re-registering a URI replaces the
// definition in place without moving it.
func (s *Server) WithResources(rs ...Resource) *Server {
	for _, r := range rs {
		if _, dup := s.resources[r.URI]; !dup {
			s.resourceOrder = append(s.resourceOrder, r.URI)
		}
		s.resources[r.URI] = r
	}
	return s
}

// WithPrompts registers the given prompts. prompts/list reports them in
// registration order; re-registering a name replaces the definition in
// place without moving it.
func (s *Server) WithPrompts(ps ...Prompt) *Server {
	for _, p := range ps {
		if _, dup := s.prompts[p.Name]; !dup {
			s.promptOrder = append(s.promptOrder, p.Name)
		}
		s.prompts[p.Name] = p
	}
	return s
}

type req struct {
	JSONRPC string          `json:"jsonrpc"`
	ID      json.RawMessage `json:"id,omitempty"` // absent for notifications
	Method  string          `json:"method"`
	Params  json.RawMessage `json:"params,omitempty"`
}

// resp is a JSON-RPC response. ID is always emitted — the spec requires
// the member to be present, and null when the request's own id could not
// be determined (parse errors, malformed envelopes).
type resp struct {
	JSONRPC string          `json:"jsonrpc"`
	ID      json.RawMessage `json:"id"`
	Result  any             `json:"result,omitempty"`
	Error   *rpcError       `json:"error,omitempty"`
}

type rpcError struct {
	Code    int    `json:"code"`
	Message string `json:"message"`
}

// Standard JSON-RPC + MCP error codes.
const (
	errParseError     = -32700
	errInvalidRequest = -32600
	errMethodNotFound = -32601
	errInvalidParams  = -32602
	errInternal       = -32603
	// errResourceNotFound is MCP's resource-specific code. An unknown URI
	// is not a JSON-RPC method lookup failure, so it does not use -32601.
	errResourceNotFound = -32002
	errAppError         = -32000
)

// nullID is the JSON `null` used as the response id when the request's own
// id is unknown or malformed.
var nullID = json.RawMessage("null")

// maxLineBytes caps a single inbound message. A longer line is drained and
// answered with a parse error rather than tearing the session down: a
// stdio server that dies on one oversized frame takes the client's whole
// session with it.
const maxLineBytes = 4 * 1024 * 1024

// Serve reads JSON-RPC messages line-by-line from r, dispatches, and
// writes responses (one per line) to w. Notifications (requests with no
// `id`, or an explicit null one) generate no response, and a batch whose
// members are all notifications generates no response either. Returns
// when r is exhausted or ctx is cancelled.
func (s *Server) Serve(ctx context.Context, r io.Reader, w io.Writer) error {
	br := bufio.NewReaderSize(r, 64*1024)
	enc := json.NewEncoder(w)
	for {
		if ctx.Err() != nil {
			// The caller is shutting the transport down. Not an error
			// condition — stdio is simply going away.
			return nil
		}
		line, tooLong, err := readLine(br, maxLineBytes)
		if err != nil {
			if errors.Is(err, io.EOF) {
				return nil
			}
			return err
		}
		if tooLong {
			if err := enc.Encode(errReply(nullID, errParseError, "message too large")); err != nil {
				return err
			}
			continue
		}
		if len(bytes.TrimSpace(line)) == 0 {
			continue
		}
		out, suppress := s.handle(ctx, line)
		if suppress {
			continue
		}
		if err := enc.Encode(out); err != nil {
			return err
		}
	}
}

// readLine reads one newline-delimited message. A line longer than max is
// consumed and discarded, reporting tooLong so the caller can answer with
// a parse error instead of aborting the session.
func readLine(br *bufio.Reader, max int) (line []byte, tooLong bool, err error) {
	for {
		chunk, isPrefix, err := br.ReadLine()
		if err != nil {
			return nil, false, err
		}
		switch {
		case tooLong:
			// Already over the cap; keep draining to the newline.
		case len(line)+len(chunk) > max:
			tooLong, line = true, nil
		default:
			line = append(line, chunk...)
		}
		if !isPrefix {
			return line, tooLong, nil
		}
	}
}

// handle parses one line — a single request or a JSON-RPC batch — and
// returns the value to encode plus a flag that, when true, tells Serve to
// write nothing.
func (s *Server) handle(ctx context.Context, line []byte) (any, bool) {
	if trimmed := bytes.TrimLeft(line, " \t\r\n"); len(trimmed) > 0 && trimmed[0] == '[' {
		return s.handleBatch(ctx, line)
	}
	var rq req
	if err := json.Unmarshal(line, &rq); err != nil {
		return errReply(nullID, errParseError, "parse error"), false
	}
	return s.dispatchChecked(ctx, rq)
}

// handleBatch dispatches an array of requests and answers with an array of
// responses, per JSON-RPC 2.0 (the batching the 2025-03-26 MCP revision
// permits). Notification members contribute nothing; a batch of nothing
// but notifications is answered with silence.
func (s *Server) handleBatch(ctx context.Context, line []byte) (any, bool) {
	var raws []json.RawMessage
	if err := json.Unmarshal(line, &raws); err != nil {
		return errReply(nullID, errParseError, "parse error"), false
	}
	if len(raws) == 0 {
		return errReply(nullID, errInvalidRequest, "empty batch"), false
	}
	out := make([]resp, 0, len(raws))
	for _, raw := range raws {
		var rq req
		if err := json.Unmarshal(raw, &rq); err != nil {
			out = append(out, errReply(nullID, errInvalidRequest, "invalid request"))
			continue
		}
		r, suppress := s.dispatchChecked(ctx, rq)
		if !suppress {
			out = append(out, r)
		}
	}
	if len(out) == 0 {
		return nil, true
	}
	return out, false
}

// dispatchChecked validates the JSON-RPC envelope and then dispatches. It
// is also the panic barrier: a handler that panics yields an error
// response instead of unwinding through Serve and killing the session.
func (s *Server) dispatchChecked(ctx context.Context, rq req) (out resp, suppress bool) {
	id, ok := normalizeID(rq.ID)
	if !ok {
		return errReply(nullID, errInvalidRequest, "id must be a string or a number"), false
	}
	rq.ID = id
	isNotification := len(id) == 0

	// A wrong version is a malformed envelope (a JSON-RPC 1.0 client, say).
	// An absent one is tolerated: some clients omit it and the intent is
	// unambiguous over an MCP transport.
	if rq.JSONRPC != "" && rq.JSONRPC != "2.0" {
		if isNotification {
			return resp{}, true
		}
		return errReply(id, errInvalidRequest, `jsonrpc must be "2.0"`), false
	}
	if rq.Method == "" {
		if isNotification {
			return resp{}, true
		}
		return errReply(id, errInvalidRequest, "missing method"), false
	}

	defer func() {
		p := recover()
		if p == nil {
			return
		}
		if isNotification {
			out, suppress = resp{}, true
			return
		}
		out, suppress = errReply(id, errInternal, fmt.Sprintf("internal error: %v", p)), false
	}()
	out, suppress = s.dispatch(ctx, rq)
	if isNotification {
		// A request with no id is a notification whatever its method:
		// JSON-RPC says run it and answer nothing. The handler still ran,
		// so any side effect the client asked for happened.
		return resp{}, true
	}
	return out, suppress
}

// normalizeID canonicalizes a request id. Both an absent id and an
// explicit null mean "no response wanted": MCP forbids a null id on a real
// request, and answering one produces a response the client cannot match
// to anything. Anything that is not a string or a number is a malformed
// envelope and reports ok=false.
func normalizeID(raw json.RawMessage) (json.RawMessage, bool) {
	t := bytes.TrimSpace(raw)
	if len(t) == 0 || bytes.Equal(t, []byte("null")) {
		return nil, true
	}
	if c := t[0]; c == '"' || c == '-' || (c >= '0' && c <= '9') {
		return t, true
	}
	return nil, false
}

// dispatch returns the response and a flag that, when true, tells Serve
// to skip writing anything (used for notifications).
// Requests carrying no id are suppressed centrally by dispatchChecked, so
// the suppress flag here only covers methods that are notification-only by
// definition.
func (s *Server) dispatch(ctx context.Context, rq req) (resp, bool) {
	switch rq.Method {
	case "initialize":
		return resp{JSONRPC: "2.0", ID: rq.ID, Result: map[string]any{
			"protocolVersion": ProtocolVersion,
			"capabilities": map[string]any{
				"tools":     map[string]any{"listChanged": false},
				"resources": map[string]any{"listChanged": false, "subscribe": false},
				"prompts":   map[string]any{"listChanged": false},
				"logging":   map[string]any{},
			},
			"serverInfo": map[string]any{"name": s.name, "version": s.version},
		}}, false

	case "notifications/initialized", "notifications/cancelled", "notifications/progress":
		// Notification-only methods: silent even if a confused client
		// attached an id.
		return resp{}, true

	case "ping":
		return resp{JSONRPC: "2.0", ID: rq.ID, Result: map[string]any{}}, false

	case "tools/list":
		tools := make([]map[string]any, 0, len(s.toolOrder))
		for _, name := range s.toolOrder {
			t := s.tools[name]
			tools = append(tools, map[string]any{
				"name":        t.Name,
				"description": t.Description,
				"inputSchema": t.InputSchema,
			})
		}
		return resp{JSONRPC: "2.0", ID: rq.ID, Result: map[string]any{"tools": tools}}, false

	case "tools/call":
		var params struct {
			Name      string         `json:"name"`
			Arguments map[string]any `json:"arguments"`
		}
		if err := json.Unmarshal(rq.Params, &params); err != nil {
			return errReply(rq.ID, errInvalidParams, "invalid params"), false
		}
		t, ok := s.tools[params.Name]
		if !ok {
			// Asking for a tool that was never listed is a client fault,
			// so it stays a protocol error (the code the MCP spec uses for
			// an unknown tool name).
			return errReply(rq.ID, errInvalidParams, fmt.Sprintf("unknown tool %q", params.Name)), false
		}
		out, err := callTool(ctx, t, params.Arguments)
		if err != nil {
			// A tool that ran and failed is not a protocol fault: the MCP
			// spec wants it reported as an ordinary result carrying
			// isError, so the calling model can read what went wrong and
			// retry instead of seeing the transport blow up.
			return resp{JSONRPC: "2.0", ID: rq.ID, Result: toolError(err)}, false
		}
		return resp{JSONRPC: "2.0", ID: rq.ID, Result: toolResult(out)}, false

	case "resources/list":
		rs := make([]map[string]any, 0, len(s.resourceOrder))
		for _, uri := range s.resourceOrder {
			r := s.resources[uri]
			rs = append(rs, map[string]any{
				"uri":         r.URI,
				"name":        r.Name,
				"description": r.Description,
				"mimeType":    r.MimeType,
			})
		}
		return resp{JSONRPC: "2.0", ID: rq.ID, Result: map[string]any{"resources": rs}}, false

	case "resources/read":
		var params struct {
			URI string `json:"uri"`
		}
		if err := json.Unmarshal(rq.Params, &params); err != nil || params.URI == "" {
			return errReply(rq.ID, errInvalidParams, "invalid params"), false
		}
		r, ok := s.resources[params.URI]
		if !ok {
			return errReply(rq.ID, errResourceNotFound, fmt.Sprintf("unknown resource %q", params.URI)), false
		}
		text, err := callResource(ctx, r)
		if err != nil {
			return errReply(rq.ID, errAppError, err.Error()), false
		}
		return resp{JSONRPC: "2.0", ID: rq.ID, Result: map[string]any{
			"contents": []map[string]any{{"uri": r.URI, "mimeType": r.MimeType, "text": text}},
		}}, false

	case "prompts/list":
		ps := make([]map[string]any, 0, len(s.promptOrder))
		for _, name := range s.promptOrder {
			p := s.prompts[name]
			args := p.Arguments
			if args == nil {
				args = []PromptArg{}
			}
			ps = append(ps, map[string]any{
				"name":        p.Name,
				"description": p.Description,
				"arguments":   args,
			})
		}
		return resp{JSONRPC: "2.0", ID: rq.ID, Result: map[string]any{"prompts": ps}}, false

	case "prompts/get":
		var params struct {
			Name      string            `json:"name"`
			Arguments map[string]string `json:"arguments"`
		}
		if err := json.Unmarshal(rq.Params, &params); err != nil {
			return errReply(rq.ID, errInvalidParams, "invalid params"), false
		}
		p, ok := s.prompts[params.Name]
		if !ok {
			return errReply(rq.ID, errInvalidParams, fmt.Sprintf("unknown prompt %q", params.Name)), false
		}
		text, err := callPrompt(ctx, p, params.Arguments)
		if err != nil {
			return errReply(rq.ID, errAppError, err.Error()), false
		}
		return resp{JSONRPC: "2.0", ID: rq.ID, Result: map[string]any{
			"description": p.Description,
			"messages": []map[string]any{{
				"role": "user",
				"content": map[string]any{
					"type": "text",
					"text": text,
				},
			}},
		}}, false

	case "logging/setLevel":
		// We accept and ack but don't actually wire a logger to MCP yet.
		return resp{JSONRPC: "2.0", ID: rq.ID, Result: map[string]any{}}, false
	}

	return errReply(rq.ID, errMethodNotFound, fmt.Sprintf("method %q not found", rq.Method)), false
}

// callTool invokes a tool handler, turning a panic or a missing handler
// into an ordinary error. The caller reports it through isError.
func callTool(ctx context.Context, t Tool, args map[string]any) (out any, err error) {
	defer func() {
		if p := recover(); p != nil {
			out, err = nil, fmt.Errorf("tool %q panicked: %v", t.Name, p)
		}
	}()
	if t.Handler == nil {
		return nil, fmt.Errorf("tool %q has no handler", t.Name)
	}
	return t.Handler(ctx, args)
}

// callResource invokes a resource handler, turning a panic or a missing
// handler into an ordinary error.
func callResource(ctx context.Context, r Resource) (text string, err error) {
	defer func() {
		if p := recover(); p != nil {
			text, err = "", fmt.Errorf("resource %q panicked: %v", r.URI, p)
		}
	}()
	if r.Handler == nil {
		return "", fmt.Errorf("resource %q has no handler", r.URI)
	}
	return r.Handler(ctx)
}

// callPrompt invokes a prompt handler, turning a panic or a missing
// handler into an ordinary error.
func callPrompt(ctx context.Context, p Prompt, args map[string]string) (text string, err error) {
	defer func() {
		if r := recover(); r != nil {
			text, err = "", fmt.Errorf("prompt %q panicked: %v", p.Name, r)
		}
	}()
	if p.Handler == nil {
		return "", fmt.Errorf("prompt %q has no handler", p.Name)
	}
	return p.Handler(ctx, args)
}

func errReply(id json.RawMessage, code int, msg string) resp {
	if len(id) == 0 {
		id = nullID
	}
	return resp{JSONRPC: "2.0", ID: id, Error: &rpcError{Code: code, Message: msg}}
}

// toolResult renders a tool handler's return value as a tools/call result.
// A string is returned as a plain text block (for prose-style tools). Any
// other value is JSON-encoded into structuredContent — so an agent can
// parse it directly instead of scraping prose — and the same JSON is also
// placed in a text block, which the MCP spec requires alongside
// structuredContent for clients that only read content.
func toolResult(out any) map[string]any {
	if s, ok := out.(string); ok {
		return map[string]any{
			"content": []map[string]any{{"type": "text", "text": s}},
		}
	}
	b, err := json.Marshal(out)
	if err != nil {
		return map[string]any{
			"content": []map[string]any{{"type": "text", "text": fmt.Sprintf("%v", out)}},
		}
	}
	return map[string]any{
		"content":           []map[string]any{{"type": "text", "text": string(b)}},
		"structuredContent": out,
	}
}

// toolError renders a tool handler's failure as a tools/call result with
// isError set. This is the shape the MCP spec reserves for execution
// failures: the model sees the message in content and can correct its
// arguments, where a JSON-RPC error would surface to the client as a
// transport-level fault it cannot act on.
func toolError(err error) map[string]any {
	return map[string]any{
		"content": []map[string]any{{"type": "text", "text": err.Error()}},
		"isError": true,
	}
}
