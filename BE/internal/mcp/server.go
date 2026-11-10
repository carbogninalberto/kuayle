// Package mcp serves Kuayle's Model Context Protocol endpoint: a stateless
// Streamable HTTP server whose tools and resources are calls to the HTTP API.
//
// Every call is dispatched in-process through the API's own routes with the
// caller's personal access token, so authentication, token scopes, workspace
// roles, team privacy, validation and history are the API's, not a copy.
package mcp

import (
	"bytes"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"net/url"
	"regexp"

	"github.com/kuayle/kuayle-backend/internal/buildinfo"
	"github.com/kuayle/kuayle-backend/internal/middleware"
	"github.com/kuayle/kuayle-backend/pkg/response"
	"github.com/labstack/echo/v5"
)

const (
	protocolVersion = "2025-06-18"
	maxRequestBytes = 2 << 20

	codeParseError       = -32700
	codeInvalidRequest   = -32600
	codeMethodNotFound   = -32601
	codeInvalidParams    = -32602
	codeInternalError    = -32603
	codeResourceNotFound = -32002

	instructions = "Kuayle issue tracker and wiki. Issues are addressed by identifier (KUA-12); teams, projects, cycles, labels, statuses and members by the IDs their list tools return. Each call acts as the user who owns the access token, within the token's scopes."
)

// safeSegment is what an argument placed in a route may look like: slugs,
// UUIDs and issue identifiers. Anything else is refused, never escaped.
var safeSegment = regexp.MustCompile(`^[A-Za-z0-9][A-Za-z0-9_-]*$`)

// quotedPathParam finds a route placeholder in a regexp-quoted template.
var quotedPathParam = regexp.MustCompile(`\\\{[a-z_]+\\\}`)

// Server answers MCP requests by calling api, the application's own router.
type Server struct {
	api http.Handler
}

func NewServer(api http.Handler) *Server { return &Server{api: api} }

type rpcRequest struct {
	JSONRPC string          `json:"jsonrpc"`
	ID      json.RawMessage `json:"id,omitempty"`
	Method  string          `json:"method"`
	Params  json.RawMessage `json:"params,omitempty"`
}

type rpcError struct {
	Code    int    `json:"code"`
	Message string `json:"message"`
}

type rpcResponse struct {
	JSONRPC string          `json:"jsonrpc"`
	ID      json.RawMessage `json:"id"`
	Result  any             `json:"result,omitempty"`
	Error   *rpcError       `json:"error,omitempty"`
}

func invalidParams(format string, args ...any) *rpcError {
	return &rpcError{Code: codeInvalidParams, Message: fmt.Sprintf(format, args...)}
}

// Handle serves POST /mcp. Only personal access tokens are accepted: the
// endpoint is for external agents, and a browser session must not reach it.
func (s *Server) Handle(c *echo.Context) error {
	if c.Get(middleware.TokenScopesKey) == nil {
		c.Response().Header().Set("WWW-Authenticate", "Bearer")
		return response.Unauthorized(c)
	}
	body, err := io.ReadAll(http.MaxBytesReader(c.Response(), c.Request().Body, maxRequestBytes))
	var req rpcRequest
	if err != nil || json.Unmarshal(body, &req) != nil {
		return c.JSON(http.StatusBadRequest, rpcResponse{JSONRPC: "2.0", ID: json.RawMessage("null"), Error: &rpcError{Code: codeParseError, Message: "The request body must be one JSON-RPC message of at most 2 MB."}})
	}
	if len(req.ID) == 0 {
		// Notifications and client responses need no answer.
		return c.NoContent(http.StatusAccepted)
	}
	resp := rpcResponse{JSONRPC: "2.0", ID: req.ID}
	if req.JSONRPC != "2.0" || req.Method == "" {
		resp.Error = &rpcError{Code: codeInvalidRequest, Message: "Not a JSON-RPC 2.0 request."}
	} else if resp.Result, resp.Error = s.dispatch(c.Request(), req); resp.Error != nil {
		resp.Result = nil
	}
	return c.JSON(http.StatusOK, resp)
}

func (s *Server) dispatch(r *http.Request, req rpcRequest) (any, *rpcError) {
	switch req.Method {
	case "initialize":
		return map[string]any{
			"protocolVersion": protocolVersion,
			"capabilities":    map[string]any{"tools": map[string]any{}, "resources": map[string]any{}},
			"serverInfo":      map[string]any{"name": "kuayle", "title": "Kuayle", "version": buildinfo.Version},
			"instructions":    instructions,
		}, nil
	case "ping":
		return map[string]any{}, nil
	case "tools/list":
		list := make([]map[string]any, len(tools))
		for i, t := range tools {
			list[i] = t.descriptor()
		}
		return map[string]any{"tools": list}, nil
	case "tools/call":
		return s.callTool(r, req.Params)
	case "resources/list":
		return map[string]any{"resources": []any{}}, nil
	case "resources/templates/list":
		list := make([]map[string]any, len(resourceTemplates))
		for i, t := range resourceTemplates {
			list[i] = map[string]any{"uriTemplate": t.URI, "name": t.Name, "description": t.Description, "mimeType": "application/json"}
		}
		return map[string]any{"resourceTemplates": list}, nil
	case "resources/read":
		return s.readResource(r, req.Params)
	}
	return nil, &rpcError{Code: codeMethodNotFound, Message: "Method not found: " + req.Method}
}

// callTool runs one tool. A refusal or validation error of the API is a tool
// result with isError, which the model can read and correct; only a malformed
// call is a protocol error.
func (s *Server) callTool(r *http.Request, params json.RawMessage) (any, *rpcError) {
	var call struct {
		Name      string                     `json:"name"`
		Arguments map[string]json.RawMessage `json:"arguments"`
	}
	if err := json.Unmarshal(params, &call); err != nil {
		return nil, invalidParams("Invalid tool call.")
	}
	var selected *tool
	for i := range tools {
		if tools[i].Name == call.Name {
			selected = &tools[i]
		}
	}
	if selected == nil {
		return nil, invalidParams("Unknown tool: %s", call.Name)
	}

	known := map[string]bool{}
	for _, p := range selected.props() {
		known[p.Name] = true
	}
	args := map[string]json.RawMessage{}
	for name, value := range call.Arguments {
		if !known[name] {
			return toolError("Unknown argument: " + name), nil
		}
		if string(value) != "null" || selected.Method != http.MethodGet {
			args[name] = value
		}
	}

	path, problem := s.route(r, selected.Path, selected.workspaceScoped(), func(name string) (string, bool) {
		var text string
		raw, ok := args[name]
		delete(args, name)
		return text, ok && json.Unmarshal(raw, &text) == nil
	})
	if problem != "" {
		return toolError(problem), nil
	}

	var query url.Values
	var body []byte
	if selected.Method == http.MethodGet {
		query = url.Values{}
		for name, raw := range args {
			var text string
			if json.Unmarshal(raw, &text) != nil {
				text = string(raw) // numbers and booleans as written
			}
			query.Set(name, text)
		}
	} else {
		body, _ = json.Marshal(args)
	}
	status, payload := s.call(r, selected.Method, path, query, body)
	return map[string]any{"content": []map[string]any{{"type": "text", "text": string(payload)}}, "isError": status >= 400}, nil
}

func toolError(message string) map[string]any {
	return map[string]any{"content": []map[string]any{{"type": "text", "text": message}}, "isError": true}
}

func (s *Server) readResource(r *http.Request, params json.RawMessage) (any, *rpcError) {
	var read struct {
		URI string `json:"uri"`
	}
	if err := json.Unmarshal(params, &read); err != nil || read.URI == "" {
		return nil, invalidParams("uri is required.")
	}
	for _, template := range resourceTemplates {
		// The URI template becomes a pattern with one group per placeholder.
		pattern := "^" + quotedPathParam.ReplaceAllString(regexp.QuoteMeta(template.URI), "([^/?#]+)") + "$"
		match := regexp.MustCompile(pattern).FindStringSubmatch(read.URI)
		if match == nil {
			continue
		}
		values := map[string]string{}
		for i, name := range pathParam.FindAllStringSubmatch(template.URI, -1) {
			values[name[1]] = match[i+1]
		}
		path, problem := s.route(r, template.Path, true, func(name string) (string, bool) {
			value, ok := values[name]
			return value, ok
		})
		if problem != "" {
			return nil, invalidParams("%s", problem)
		}
		status, payload := s.call(r, http.MethodGet, path, nil, nil)
		switch {
		case status == http.StatusNotFound:
			return nil, &rpcError{Code: codeResourceNotFound, Message: "Resource not found: " + read.URI}
		case status >= 400:
			return nil, &rpcError{Code: codeInternalError, Message: apiErrorMessage(payload)}
		}
		return map[string]any{"contents": []map[string]any{{"uri": read.URI, "mimeType": "application/json", "text": string(payload)}}}, nil
	}
	return nil, &rpcError{Code: codeResourceNotFound, Message: "Resource not found: " + read.URI}
}

// route fills a route template from arguments. It returns a message for the
// caller when an argument is missing or cannot be part of a route.
func (s *Server) route(r *http.Request, template string, workspaceScoped bool, arg func(string) (string, bool)) (path, problem string) {
	if workspaceScoped {
		slug, given := arg(workspaceProp.Name)
		if !given {
			slug = r.URL.Query().Get(workspaceProp.Name)
		}
		if slug == "" {
			if slug, problem = s.onlyWorkspace(r); problem != "" {
				return "", problem
			}
		}
		if !safeSegment.MatchString(slug) {
			return "", "Invalid workspace."
		}
		template = "/api/workspaces/" + slug + template
	}
	path = pathParam.ReplaceAllStringFunc(template, func(placeholder string) string {
		name := placeholder[1 : len(placeholder)-1]
		value, ok := arg(name)
		switch {
		case !ok || value == "":
			problem = name + " is required."
		case name == "scope" && value != "team" && value != "project":
			problem = "scope must be team or project."
		case !safeSegment.MatchString(value):
			problem = "Invalid " + name + "."
		}
		return value
	})
	return path, problem
}

// onlyWorkspace is the workspace of a call that names none: the single one
// the token can reach.
func (s *Server) onlyWorkspace(r *http.Request) (slug, problem string) {
	status, payload := s.call(r, http.MethodGet, "/api/workspaces", nil, nil)
	var workspaces []struct {
		Slug string `json:"slug"`
	}
	if status != http.StatusOK || json.Unmarshal(payload, &workspaces) != nil || len(workspaces) != 1 {
		return "", "Name the workspace: pass the workspace argument, or add ?workspace=<slug> to the server URL. list_workspaces shows the slugs."
	}
	return workspaces[0].Slug, ""
}

// call performs one API request as the MCP caller: same token, same client
// address, through the full middleware chain of the route.
func (s *Server) call(r *http.Request, method, path string, query url.Values, body []byte) (int, []byte) {
	sub := r.Clone(r.Context())
	sub.Method = method
	sub.URL = &url.URL{Path: path, RawQuery: query.Encode()}
	sub.RequestURI = sub.URL.RequestURI()
	sub.Header = http.Header{}
	for _, name := range []string{"Authorization", "X-Forwarded-For", "User-Agent"} {
		if values := r.Header.Values(name); len(values) > 0 {
			sub.Header[name] = values
		}
	}
	sub.Body = http.NoBody
	sub.ContentLength = int64(len(body))
	if body != nil {
		sub.Header.Set("Content-Type", "application/json")
		sub.Body = io.NopCloser(bytes.NewReader(body))
	}
	rec := &recorder{header: http.Header{}, status: http.StatusOK}
	s.api.ServeHTTP(rec, sub)
	return rec.status, rec.body.Bytes()
}

func apiErrorMessage(payload []byte) string {
	var failure struct {
		Error struct {
			Message string `json:"message"`
		} `json:"error"`
	}
	if json.Unmarshal(payload, &failure) == nil && failure.Error.Message != "" {
		return failure.Error.Message
	}
	return "The request failed."
}

// recorder buffers the API's response to a tool call.
type recorder struct {
	header http.Header
	status int
	body   bytes.Buffer
}

func (r *recorder) Header() http.Header         { return r.header }
func (r *recorder) WriteHeader(status int)      { r.status = status }
func (r *recorder) Write(b []byte) (int, error) { return r.body.Write(b) }
