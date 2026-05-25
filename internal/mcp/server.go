// Package mcp implements a minimal Model Context Protocol server over
// stdio. Supports the handshake (initialize), tool discovery
// (tools/list), and tool execution (tools/call) for a small set of
// read-only mkt tools.
package mcp

import (
	"bufio"
	"context"
	"encoding/json"
	"fmt"
	"io"
)

// Tool is one callable in the MCP server.
type Tool struct {
	Name        string                                                      `json:"name"`
	Description string                                                      `json:"description"`
	InputSchema map[string]any                                              `json:"inputSchema"`
	Handler     func(ctx context.Context, args map[string]any) (any, error) `json:"-"`
}

// Server reads JSON-RPC requests from r and writes responses to w.
type Server struct {
	tools map[string]Tool
}

// New constructs a Server with the supplied tools.
func New(tools []Tool) *Server {
	m := make(map[string]Tool, len(tools))
	for _, t := range tools {
		m[t.Name] = t
	}
	return &Server{tools: m}
}

type req struct {
	JSONRPC string          `json:"jsonrpc"`
	ID      json.RawMessage `json:"id,omitempty"`
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

// Serve reads one JSON object per line from r, dispatches, and writes
// the response per line to w. Returns when r is exhausted.
func (s *Server) Serve(ctx context.Context, r io.Reader, w io.Writer) error {
	sc := bufio.NewScanner(r)
	sc.Buffer(make([]byte, 0, 64*1024), 4*1024*1024)
	enc := json.NewEncoder(w)
	for sc.Scan() {
		var rq req
		if err := json.Unmarshal(sc.Bytes(), &rq); err != nil {
			_ = enc.Encode(resp{JSONRPC: "2.0", Error: &rpcError{Code: -32700, Message: "parse error"}})
			continue
		}
		out := s.dispatch(ctx, rq)
		if err := enc.Encode(out); err != nil {
			return err
		}
	}
	return sc.Err()
}

func (s *Server) dispatch(ctx context.Context, rq req) resp {
	switch rq.Method {
	case "initialize":
		return resp{JSONRPC: "2.0", ID: rq.ID, Result: map[string]any{
			"protocolVersion": "2025-03-26",
			"capabilities":    map[string]any{"tools": map[string]any{}},
			"serverInfo":      map[string]any{"name": "mkt", "version": "dev"},
		}}
	case "tools/list":
		var tools []map[string]any
		for _, t := range s.tools {
			tools = append(tools, map[string]any{
				"name":        t.Name,
				"description": t.Description,
				"inputSchema": t.InputSchema,
			})
		}
		return resp{JSONRPC: "2.0", ID: rq.ID, Result: map[string]any{"tools": tools}}
	case "tools/call":
		var params struct {
			Name      string         `json:"name"`
			Arguments map[string]any `json:"arguments"`
		}
		if err := json.Unmarshal(rq.Params, &params); err != nil {
			return resp{JSONRPC: "2.0", ID: rq.ID, Error: &rpcError{Code: -32602, Message: "invalid params"}}
		}
		t, ok := s.tools[params.Name]
		if !ok {
			return resp{JSONRPC: "2.0", ID: rq.ID, Error: &rpcError{Code: -32601, Message: fmt.Sprintf("unknown tool %q", params.Name)}}
		}
		out, err := t.Handler(ctx, params.Arguments)
		if err != nil {
			return resp{JSONRPC: "2.0", ID: rq.ID, Error: &rpcError{Code: -32000, Message: err.Error()}}
		}
		return resp{JSONRPC: "2.0", ID: rq.ID, Result: map[string]any{
			"content": []map[string]any{{"type": "text", "text": fmt.Sprintf("%v", out)}},
		}}
	}
	return resp{JSONRPC: "2.0", ID: rq.ID, Error: &rpcError{Code: -32601, Message: "method not found"}}
}
