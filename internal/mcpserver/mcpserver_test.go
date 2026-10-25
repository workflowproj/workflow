package mcpserver

import (
	"context"
	"slices"
	"testing"

	"github.com/modelcontextprotocol/go-sdk/mcp"

	"github.com/geoffo-dev/workflow-management/internal/authz"
)

// listTools lists the tools a server built for the actor offers.
func listTools(t *testing.T, s *Server, a *authz.Actor, connected bool) map[string]*mcp.Tool {
	t.Helper()
	ctx := context.Background()
	ct, st := mcp.NewInMemoryTransports()
	if _, err := s.server(a, connected).Connect(ctx, st, nil); err != nil {
		t.Fatal(err)
	}
	cs, err := mcp.NewClient(&mcp.Implementation{Name: "test"}, nil).Connect(ctx, ct, nil)
	if err != nil {
		t.Fatal(err)
	}
	defer cs.Close()
	res, err := cs.ListTools(ctx, nil)
	if err != nil {
		t.Fatal(err)
	}
	out := map[string]*mcp.Tool{}
	for _, tool := range res.Tools {
		out[tool.Name] = tool
	}
	return out
}

// writeTools must name exactly the tools that are not read-only: the 403 step-up gate and the
// read-only tool list depend on it.
func TestWriteToolsList(t *testing.T) {
	s := &Server{}
	all := listTools(t, s, &authz.Actor{Scopes: []string{"read", "write"}}, true)
	var writes []string
	for name, tool := range all {
		if tool.Annotations == nil || !tool.Annotations.ReadOnlyHint {
			writes = append(writes, name)
		}
	}
	slices.Sort(writes)
	want := slices.Clone(writeTools)
	slices.Sort(want)
	if !slices.Equal(writes, want) {
		t.Fatalf("write tools %v, list says %v", writes, want)
	}

	readOnly := listTools(t, s, &authz.Actor{Scopes: []string{"read"}, Via: "Claude"}, true)
	if len(readOnly) != len(all)-len(writeTools) {
		t.Fatalf("read-only connected app sees %d of %d tools", len(readOnly), len(all))
	}
	for _, w := range writeTools {
		if readOnly[w] != nil {
			t.Fatalf("%s offered to a read-only app", w)
		}
	}
	// Personal access tokens keep every tool (a read-only token gets a tool error instead).
	if pat := listTools(t, s, &authz.Actor{Scopes: []string{"read"}}, false); len(pat) != len(all) {
		t.Fatalf("PAT sees %d of %d tools", len(pat), len(all))
	}
}
