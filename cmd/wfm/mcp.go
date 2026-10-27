package main

import (
	"bufio"
	"bytes"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"os"
	"strings"
	"time"
)

// mcpBridge relays Model Context Protocol messages between stdio and the server's /mcp
// endpoint, for assistants that launch local MCP servers (Claude Desktop, IDEs) rather than
// connecting over HTTP. Each line on stdin is a JSON-RPC message; each reply is one line on
// stdout. WFM_SERVER and WFM_TOKEN say where to connect and as whom.
func mcpBridge() error {
	server, token := strings.TrimRight(os.Getenv("WFM_SERVER"), "/"), os.Getenv("WFM_TOKEN")
	if server == "" || token == "" {
		return fmt.Errorf("set WFM_SERVER (e.g. https://wfm.example.com) and WFM_TOKEN (a personal access token)")
	}
	client := &http.Client{Timeout: 2 * time.Minute}
	in := bufio.NewScanner(os.Stdin)
	in.Buffer(make([]byte, 64*1024), 16*1024*1024)
	out := bufio.NewWriter(os.Stdout)
	protocol := ""
	for in.Scan() {
		msg := bytes.TrimSpace(in.Bytes())
		if len(msg) == 0 {
			continue
		}
		var probe struct {
			ID     json.RawMessage `json:"id"`
			Method string          `json:"method"`
		}
		_ = json.Unmarshal(msg, &probe)
		reply, err := mcpPost(client, server+"/mcp", token, protocol, msg)
		if err != nil {
			fmt.Fprintln(os.Stderr, "wfm mcp:", err)
			if len(probe.ID) > 0 {
				reply, _ = json.Marshal(map[string]any{"jsonrpc": "2.0", "id": probe.ID, "error": map[string]any{"code": -32603, "message": err.Error()}})
			}
		}
		if probe.Method == "initialize" && len(reply) > 0 {
			var r struct {
				Result struct {
					ProtocolVersion string `json:"protocolVersion"`
				} `json:"result"`
			}
			if json.Unmarshal(reply, &r) == nil {
				protocol = r.Result.ProtocolVersion
			}
		}
		if len(reply) > 0 {
			out.Write(bytes.TrimSpace(reply))
			out.WriteByte('\n')
			if err := out.Flush(); err != nil {
				return err
			}
		}
	}
	return in.Err()
}

// mcpPost sends one message and returns the JSON reply (nothing for notifications).
func mcpPost(client *http.Client, url, token, protocol string, msg []byte) ([]byte, error) {
	req, err := http.NewRequest(http.MethodPost, url, bytes.NewReader(msg)) // #nosec G704 -- the CLI talks to the server its user configured (WFM_SERVER)
	if err != nil {
		return nil, err
	}
	req.Header.Set("Authorization", "Bearer "+token)
	req.Header.Set("Content-Type", "application/json")
	req.Header.Set("Accept", "application/json, text/event-stream")
	if protocol != "" {
		req.Header.Set("MCP-Protocol-Version", protocol)
	}
	res, err := client.Do(req) // #nosec G704 -- the server its user configured
	if err != nil {
		return nil, err
	}
	defer res.Body.Close()
	body, err := io.ReadAll(io.LimitReader(res.Body, 32*1024*1024))
	if err != nil {
		return nil, err
	}
	switch {
	case res.StatusCode == http.StatusAccepted:
		return nil, nil
	case res.StatusCode >= 300:
		return nil, fmt.Errorf("%s: %s", res.Status, strings.TrimSpace(string(body)))
	case strings.HasPrefix(res.Header.Get("Content-Type"), "text/event-stream"):
		// One response per request: take the event's data lines.
		var data []string
		for _, line := range strings.Split(string(body), "\n") {
			if v, ok := strings.CutPrefix(line, "data:"); ok {
				data = append(data, strings.TrimSpace(v))
			}
		}
		return []byte(strings.Join(data, "")), nil
	}
	return body, nil
}
