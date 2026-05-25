// Package mcp implements a Model Context Protocol server over stdio.
// Supports the initialization handshake, ping, tools, resources, and
// prompts — covering the read-only surface MCP clients (Claude Code,
// Claude Desktop, etc.) need to introspect a stateful CLI like mkt.
package mcp

import (
	"bufio"
	"context"
	"encoding/json"
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
type Server struct {
	tools     map[string]Tool
	resources map[string]Resource
	prompts   map[string]Prompt

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

// WithTools registers the given tools.
func (s *Server) WithTools(tools ...Tool) *Server {
	for _, t := range tools {
		s.tools[t.Name] = t
	}
	return s
}

// WithResources registers the given resources.
func (s *Server) WithResources(rs ...Resource) *Server {
	for _, r := range rs {
		s.resources[r.URI] = r
	}
	return s
}

// WithPrompts registers the given prompts.
func (s *Server) WithPrompts(ps ...Prompt) *Server {
	for _, p := range ps {
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

type resp struct {
	JSONRPC string          `json:"jsonrpc"`
	ID      json.RawMessage `json:"id,omitempty"`
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
	errAppError       = -32000
)

// Serve reads JSON-RPC messages line-by-line from r, dispatches, and
// writes responses (one per line) to w. Notifications (requests without
// an `id`) generate no response. Returns when r is exhausted.
func (s *Server) Serve(ctx context.Context, r io.Reader, w io.Writer) error {
	sc := bufio.NewScanner(r)
	sc.Buffer(make([]byte, 0, 64*1024), 4*1024*1024)
	enc := json.NewEncoder(w)
	for sc.Scan() {
		line := sc.Bytes()
		if len(line) == 0 {
			continue
		}
		var rq req
		if err := json.Unmarshal(line, &rq); err != nil {
			_ = enc.Encode(resp{JSONRPC: "2.0", Error: &rpcError{Code: errParseError, Message: "parse error"}})
			continue
		}
		out, suppress := s.dispatch(ctx, rq)
		if suppress {
			continue
		}
		if err := enc.Encode(out); err != nil {
			return err
		}
	}
	return sc.Err()
}

// dispatch returns the response and a flag that, when true, tells Serve
// to skip writing anything (used for notifications).
func (s *Server) dispatch(ctx context.Context, rq req) (resp, bool) {
	isNotification := len(rq.ID) == 0
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
		// Notifications never get a response.
		return resp{}, true

	case "ping":
		return resp{JSONRPC: "2.0", ID: rq.ID, Result: map[string]any{}}, false

	case "tools/list":
		var tools []map[string]any
		for _, t := range s.tools {
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
			return errReply(rq.ID, errMethodNotFound, fmt.Sprintf("unknown tool %q", params.Name)), false
		}
		out, err := t.Handler(ctx, params.Arguments)
		if err != nil {
			return errReply(rq.ID, errAppError, err.Error()), false
		}
		return resp{JSONRPC: "2.0", ID: rq.ID, Result: map[string]any{
			"content": []map[string]any{{"type": "text", "text": fmt.Sprintf("%v", out)}},
		}}, false

	case "resources/list":
		var rs []map[string]any
		for _, r := range s.resources {
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
			return errReply(rq.ID, errMethodNotFound, fmt.Sprintf("unknown resource %q", params.URI)), false
		}
		text, err := r.Handler(ctx)
		if err != nil {
			return errReply(rq.ID, errAppError, err.Error()), false
		}
		return resp{JSONRPC: "2.0", ID: rq.ID, Result: map[string]any{
			"contents": []map[string]any{{"uri": r.URI, "mimeType": r.MimeType, "text": text}},
		}}, false

	case "prompts/list":
		var ps []map[string]any
		for _, p := range s.prompts {
			ps = append(ps, map[string]any{
				"name":        p.Name,
				"description": p.Description,
				"arguments":   p.Arguments,
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
			return errReply(rq.ID, errMethodNotFound, fmt.Sprintf("unknown prompt %q", params.Name)), false
		}
		text, err := p.Handler(ctx, params.Arguments)
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

	if isNotification {
		// Unknown notifications: silently ignored per the JSON-RPC spec.
		return resp{}, true
	}
	return errReply(rq.ID, errMethodNotFound, fmt.Sprintf("method %q not found", rq.Method)), false
}

func errReply(id json.RawMessage, code int, msg string) resp {
	return resp{JSONRPC: "2.0", ID: id, Error: &rpcError{Code: code, Message: msg}}
}
