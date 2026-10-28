// Package mcpapi implements the hub's MCP transport (streamable HTTP,
// /mcp) on top of internal/service, exposing the same 8 operations as
// internal/api's REST transport with identical request/response shapes
// (task E1 §3).
package mcpapi

import (
	"context"
	"encoding/json"
	"net/http"
	"reflect"

	"github.com/google/jsonschema-go/jsonschema"
	"github.com/modelcontextprotocol/go-sdk/mcp"

	"github.com/tereshed/8bitnews/internal/service"
)

// schemaOpts overrides the schema the go-sdk's reflection-based inference
// would otherwise assign to json.RawMessage fields.
//
// json.RawMessage is defined as []byte, and jsonschema-go has no special
// case for it (unlike time.Time, big.Int, etc.), so by default it infers
// a byte-array schema ({"type": ["null", "array"]}) for every RawMessage
// field. That rejects the JSON objects these fields actually carry
// (research_dossier, script, legal_review, ...) before a call ever
// reaches the service layer. Map it to an unrestricted schema instead,
// matching reflect.Interface's "any type valid" behavior.
var schemaOpts = &jsonschema.ForOptions{
	TypeSchemas: map[reflect.Type]*jsonschema.Schema{
		reflect.TypeFor[json.RawMessage](): {},
	},
}

// schemaFor builds the JSON Schema for T with schemaOpts applied, panicking
// on error since T is always one of our own request/response types.
func schemaFor[T any]() *jsonschema.Schema {
	s, err := jsonschema.For[T](schemaOpts)
	if err != nil {
		panic(err)
	}
	return s
}

// addTool is mcp.AddTool with input/output schemas built via schemaFor
// instead of the SDK's default reflection, so RawMessage fields get an
// unrestricted schema rather than a byte-array one.
func addTool[In, Out any](server *mcp.Server, tool *mcp.Tool, handler mcp.ToolHandlerFor[In, Out]) {
	tool.InputSchema = schemaFor[In]()
	tool.OutputSchema = schemaFor[Out]()
	mcp.AddTool(server, tool, handler)
}

// NewServer registers the 8 hub tools on a fresh MCP server.
func NewServer(svc *service.Service) *mcp.Server {
	server := mcp.NewServer(&mcp.Implementation{Name: "8bitnews-hub", Version: "0.1.0"}, nil)

	addTool(server, &mcp.Tool{
		Name:        "channel_get",
		Description: "Get a channel's profile (docs/architecture.md §10) plus the text of its format.md.",
	}, func(ctx context.Context, _ *mcp.CallToolRequest, in service.ChannelGetRequest) (*mcp.CallToolResult, service.ChannelView, error) {
		out, err := svc.ChannelGet(ctx, in)
		return nil, out, err
	})

	addTool(server, &mcp.Tool{
		Name:        "story_find_similar",
		Description: "Find a channel's storylines similar to a candidate title/summary, each with its developments and the releases made for them.",
	}, func(ctx context.Context, _ *mcp.CallToolRequest, in service.StoryFindSimilarRequest) (*mcp.CallToolResult, service.StoryFindSimilarResponse, error) {
		out, err := svc.StoryFindSimilar(ctx, in)
		return nil, out, err
	})

	addTool(server, &mcp.Tool{
		Name:        "story_register",
		Description: "Register a new storyline (story_id null) with its first development, or add a new development to an existing storyline.",
	}, func(ctx context.Context, _ *mcp.CallToolRequest, in service.StoryRegisterRequest) (*mcp.CallToolResult, service.StoryRegisterResponse, error) {
		out, err := svc.StoryRegister(ctx, in)
		return nil, out, err
	})

	addTool(server, &mcp.Tool{
		Name:        "experiment_assign",
		Description: "Assign the experiment arm and variant params for a story development (docs/architecture.md §9).",
	}, func(ctx context.Context, _ *mcp.CallToolRequest, in service.ExperimentAssignRequest) (*mcp.CallToolResult, service.ExperimentAssignResponse, error) {
		out, err := svc.ExperimentAssign(ctx, in)
		return nil, out, err
	})

	addTool(server, &mcp.Tool{
		Name:        "media_upload_url",
		Description: "Get a presigned S3 PUT URL and object key to upload a development's media file to.",
	}, func(ctx context.Context, _ *mcp.CallToolRequest, in service.MediaUploadURLRequest) (*mcp.CallToolResult, service.MediaUploadURLResponse, error) {
		out, err := svc.MediaUploadURL(ctx, in)
		return nil, out, err
	})

	addTool(server, &mcp.Tool{
		Name:        "media_download_url",
		Description: "Get a presigned S3 GET URL for a release's media (releases/...) or a music catalog track (music/...).",
	}, func(ctx context.Context, _ *mcp.CallToolRequest, in service.MediaDownloadURLRequest) (*mcp.CallToolResult, service.MediaDownloadURLResponse, error) {
		out, err := svc.MediaDownloadURL(ctx, in)
		return nil, out, err
	})

	addTool(server, &mcp.Tool{
		Name:        "release_submit",
		Description: "Submit a release manifest: validates media, QA and legal verdicts, checks and archives sources, and creates the release in status submitted.",
	}, func(ctx context.Context, _ *mcp.CallToolRequest, in service.ReleaseManifest) (*mcp.CallToolResult, service.ReleaseSubmitResponse, error) {
		out, err := svc.ReleaseSubmit(ctx, in)
		return nil, out, err
	})

	addTool(server, &mcp.Tool{
		Name:        "release_get",
		Description: "Get a release by id, including its checked and archived sources.",
	}, func(ctx context.Context, _ *mcp.CallToolRequest, in service.ReleaseGetRequest) (*mcp.CallToolResult, service.ReleaseView, error) {
		out, err := svc.ReleaseGet(ctx, in)
		return nil, out, err
	})

	addTool(server, &mcp.Tool{
		Name:        "releases_recent",
		Description: "List a channel's most recent releases, for Legal to check a new release against them for un-originality.",
	}, func(ctx context.Context, _ *mcp.CallToolRequest, in service.ReleasesRecentRequest) (*mcp.CallToolResult, service.ReleasesRecentResponse, error) {
		out, err := svc.ReleasesRecent(ctx, in)
		return nil, out, err
	})

	return server
}

// NewHTTPHandler wraps the MCP server in a streamable-HTTP handler
// suitable for mounting at /mcp.
func NewHTTPHandler(svc *service.Service) http.Handler {
	server := NewServer(svc)
	return mcp.NewStreamableHTTPHandler(func(*http.Request) *mcp.Server {
		return server
	}, nil)
}
