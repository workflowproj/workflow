package httpapi

import (
	"context"
	"io"
	"mime"
	"net/http"
	"strconv"
	"strings"

	"github.com/workflowproj/workflow/internal/api"
	"github.com/workflowproj/workflow/internal/apperr"
	"github.com/workflowproj/workflow/internal/authz"
)

// ---------------------------------------------------------------------------
// Customers reach the portal and their own account, nothing else

// customerPaths are the API routes customers may call: the portal, their account and sign-in.
// Everything else is the agent API, refused even where it would find nothing for them.
var customerPaths = []string{
	"/api/v1/portal/", "/api/v1/auth/",
	"/api/v1/me/email-notifications", "/api/v1/me/security", "/api/v1/me/password", "/api/v1/me/totp/",
}

// portalOnly refuses customers (organisation role "customer") every API route but the portal's,
// their own profile and sign-in. Their project permissions are empty anyway (authz); this keeps
// the organisation-wide routes (people, groups, filters, live events...) out of reach too.
func portalOnly(a *authz.Actor, path string) error {
	if !a.IsCustomer() {
		return nil
	}
	if path == "/api/v1/me" || path == "/api/v1/openapi.yaml" || path == "/api/v1/openapi.json" {
		return nil
	}
	for _, p := range customerPaths {
		if strings.HasPrefix(path, p) || path == strings.TrimSuffix(p, "/") {
			return nil
		}
	}
	return apperr.New(http.StatusForbidden, "portal_only", "Customers use the help centre at /portal.")
}

// ---------------------------------------------------------------------------
// The customer portal

func (s *Server) ListPortalDesks(ctx context.Context, _ api.ListPortalDesksRequestObject) (api.ListPortalDesksResponseObject, error) {
	a, err := actor(ctx)
	if err != nil {
		return nil, err
	}
	ds, err := s.ServiceDesk.ListDesks(ctx, a)
	if err != nil {
		return nil, err
	}
	return api.ListPortalDesks200JSONResponse(ds), nil
}

func (s *Server) GetPortalDesk(ctx context.Context, req api.GetPortalDeskRequestObject) (api.GetPortalDeskResponseObject, error) {
	a, err := actor(ctx)
	if err != nil {
		return nil, err
	}
	d, err := s.ServiceDesk.PortalDesk(ctx, a, req.ProjectKey)
	if err != nil {
		return nil, err
	}
	return api.GetPortalDesk200JSONResponse(d), nil
}

func (s *Server) CreatePortalRequest(ctx context.Context, req api.CreatePortalRequestRequestObject) (api.CreatePortalRequestResponseObject, error) {
	a, err := actor(ctx)
	if err != nil {
		return nil, err
	}
	r, err := s.ServiceDesk.CreateRequest(ctx, a, req.ProjectKey, *req.Body)
	if err != nil {
		return nil, err
	}
	return api.CreatePortalRequest201JSONResponse(r), nil
}

func (s *Server) ListPortalRequests(ctx context.Context, req api.ListPortalRequestsRequestObject) (api.ListPortalRequestsResponseObject, error) {
	a, err := actor(ctx)
	if err != nil {
		return nil, err
	}
	status := "all"
	if req.Params.Status != nil {
		status = string(*req.Params.Status)
	}
	rs, err := s.ServiceDesk.ListRequests(ctx, a, status, deref(req.Params.Project), limitOr(req.Params.Limit, 100))
	if err != nil {
		return nil, err
	}
	return api.ListPortalRequests200JSONResponse(rs), nil
}

func (s *Server) GetPortalRequest(ctx context.Context, req api.GetPortalRequestRequestObject) (api.GetPortalRequestResponseObject, error) {
	a, err := actor(ctx)
	if err != nil {
		return nil, err
	}
	r, err := s.ServiceDesk.PortalRequest(ctx, a, req.IssueKey)
	if err != nil {
		return nil, err
	}
	return api.GetPortalRequest200JSONResponse(r), nil
}

func (s *Server) AddPortalComment(ctx context.Context, req api.AddPortalCommentRequestObject) (api.AddPortalCommentResponseObject, error) {
	a, err := actor(ctx)
	if err != nil {
		return nil, err
	}
	c, err := s.ServiceDesk.AddComment(ctx, a, req.IssueKey, req.Body.Body)
	if err != nil {
		return nil, err
	}
	return api.AddPortalComment201JSONResponse(c), nil
}

func (s *Server) UploadPortalAttachment(ctx context.Context, req api.UploadPortalAttachmentRequestObject) (api.UploadPortalAttachmentResponseObject, error) {
	a, err := actor(ctx)
	if err != nil {
		return nil, err
	}
	for {
		part, err := req.Body.NextPart()
		if err == io.EOF {
			return nil, apperr.Invalid("file", "no file part in the upload")
		}
		if err != nil {
			return nil, apperr.BadRequest("invalid multipart body: %v", err)
		}
		if part.FormName() != "file" {
			continue
		}
		size := int64(-1)
		if v := part.Header.Get("Content-Length"); v != "" {
			size, _ = strconv.ParseInt(v, 10, 64)
		}
		ct := part.Header.Get("Content-Type")
		if mt, _, err := mime.ParseMediaType(ct); err == nil {
			ct = mt
		}
		att, err := s.ServiceDesk.Attach(ctx, a, req.IssueKey, part.FileName(), ct, size, part)
		if err != nil {
			return nil, err
		}
		return api.UploadPortalAttachment201JSONResponse(att), nil
	}
}

func (s *Server) DownloadPortalAttachment(ctx context.Context, req api.DownloadPortalAttachmentRequestObject) (api.DownloadPortalAttachmentResponseObject, error) {
	a, err := actor(ctx)
	if err != nil {
		return nil, err
	}
	att, rc, u, err := s.ServiceDesk.OpenAttachment(ctx, a, req.AttachmentId)
	if err != nil {
		return nil, err
	}
	if u != nil {
		return api.DownloadPortalAttachment302Response{Headers: api.DownloadPortalAttachment302ResponseHeaders{Location: ptr(u.String())}}, nil
	}
	if w, _ := httpFrom(ctx); w != nil {
		w.Header().Set("Content-Type", att.ContentType)
		w.Header().Set("Content-Security-Policy", "sandbox")
	}
	return api.DownloadPortalAttachment200ApplicationoctetStreamResponse{
		Body: rc, ContentLength: att.SizeBytes,
		Headers: api.DownloadPortalAttachment200ResponseHeaders{ContentDisposition: ptr(mime.FormatMediaType("attachment", map[string]string{"filename": att.Filename}))},
	}, nil
}

func (s *Server) ListPortalApprovals(ctx context.Context, req api.ListPortalApprovalsRequestObject) (api.ListPortalApprovalsResponseObject, error) {
	a, err := actor(ctx)
	if err != nil {
		return nil, err
	}
	rs, err := s.ServiceDesk.ListApprovals(ctx, a, limitOr(req.Params.Limit, 100))
	if err != nil {
		return nil, err
	}
	return api.ListPortalApprovals200JSONResponse(rs), nil
}

func (s *Server) DecidePortalApproval(ctx context.Context, req api.DecidePortalApprovalRequestObject) (api.DecidePortalApprovalResponseObject, error) {
	a, err := actor(ctx)
	if err != nil {
		return nil, err
	}
	r, err := s.ServiceDesk.Decide(ctx, a, req.IssueKey, *req.Body)
	if err != nil {
		return nil, err
	}
	return api.DecidePortalApproval200JSONResponse(r), nil
}

func (s *Server) GetPortalSignup(ctx context.Context, req api.GetPortalSignupRequestObject) (api.GetPortalSignupResponseObject, error) {
	info, err := s.ServiceDesk.SignupInfo(ctx, req.ProjectKey)
	if err != nil {
		return nil, err
	}
	return api.GetPortalSignup200JSONResponse(info), nil
}

func (s *Server) PortalSignup(ctx context.Context, req api.PortalSignupRequestObject) (api.PortalSignupResponseObject, error) {
	if err := s.ServiceDesk.Signup(ctx, req.ProjectKey, *req.Body, requestIP(ctx)); err != nil {
		return nil, localErr(ctx, err)
	}
	return api.PortalSignup202Response{}, nil
}
