package httpapi

import (
	"context"
	"encoding/json"

	"github.com/workflowproj/workflow/internal/api"
	"github.com/workflowproj/workflow/internal/db/dbq"
	"github.com/workflowproj/workflow/internal/jiraimport"
)

// Import from Jira: the wizard's API. The import service's views serialise to the same JSON as
// the API schemas, so they convert with a JSON round trip.

func convertJSON[T any](v any) (T, error) {
	var out T
	raw, err := json.Marshal(v)
	if err != nil {
		return out, err
	}
	err = json.Unmarshal(raw, &out)
	return out, err
}

func (s *Server) jiraImportView(ctx context.Context, imp dbq.JiraImport, err error) (api.JiraImport, error) {
	if err != nil {
		return api.JiraImport{}, err
	}
	v, err := s.JiraImport.ViewOf(ctx, imp)
	if err != nil {
		return api.JiraImport{}, err
	}
	return convertJSON[api.JiraImport](v)
}

func connectionInput(b api.JiraConnectionInput) jiraimport.ConnectionInput {
	return jiraimport.ConnectionInput{Flavor: string(b.Flavor), BaseURL: b.BaseUrl, Email: deref(b.Email), Token: deref(b.Token)}
}

func (s *Server) AdminListJiraImports(ctx context.Context, _ api.AdminListJiraImportsRequestObject) (api.AdminListJiraImportsResponseObject, error) {
	a, err := orgAdmin(ctx)
	if err != nil {
		return nil, err
	}
	list, err := s.JiraImport.List(ctx, a)
	if err != nil {
		return nil, err
	}
	out, err := convertJSON[[]api.JiraImport](list)
	return api.AdminListJiraImports200JSONResponse(out), err
}

func (s *Server) AdminCreateJiraImport(ctx context.Context, req api.AdminCreateJiraImportRequestObject) (api.AdminCreateJiraImportResponseObject, error) {
	a, err := orgAdmin(ctx)
	if err != nil {
		return nil, err
	}
	imp, err := s.JiraImport.Create(ctx, a, connectionInput(*req.Body))
	v, err := s.jiraImportView(ctx, imp, err)
	return api.AdminCreateJiraImport201JSONResponse(v), err
}

func (s *Server) AdminTestJiraConnection(ctx context.Context, req api.AdminTestJiraConnectionRequestObject) (api.AdminTestJiraConnectionResponseObject, error) {
	a, err := orgAdmin(ctx)
	if err != nil {
		return nil, err
	}
	res, err := s.JiraImport.TestConnection(ctx, a, connectionInput(*req.Body))
	if err != nil {
		return nil, err
	}
	out, err := convertJSON[api.JiraConnectionTest](res)
	return api.AdminTestJiraConnection200JSONResponse(out), err
}

func (s *Server) AdminGetJiraImport(ctx context.Context, req api.AdminGetJiraImportRequestObject) (api.AdminGetJiraImportResponseObject, error) {
	a, err := orgAdmin(ctx)
	if err != nil {
		return nil, err
	}
	v, err := s.JiraImport.Get(ctx, a, req.ImportId)
	if err != nil {
		return nil, err
	}
	out, err := convertJSON[api.JiraImport](v)
	return api.AdminGetJiraImport200JSONResponse(out), err
}

func (s *Server) AdminUpdateJiraImport(ctx context.Context, req api.AdminUpdateJiraImportRequestObject) (api.AdminUpdateJiraImportResponseObject, error) {
	a, err := orgAdmin(ctx)
	if err != nil {
		return nil, err
	}
	b := req.Body
	var imp dbq.JiraImport
	if b.Token != nil && *b.Token != "" {
		if imp, err = s.JiraImport.UpdateConnection(ctx, a, req.ImportId, deref(b.Email), *b.Token); err != nil {
			return nil, err
		}
	}
	if b.Config != nil {
		cfg, err := convertJSON[jiraimport.Config](b.Config)
		if err != nil {
			return nil, err
		}
		if imp, err = s.JiraImport.UpdateConfig(ctx, a, req.ImportId, cfg); err != nil {
			return nil, err
		}
	}
	if imp.ID != req.ImportId {
		v, err := s.JiraImport.Get(ctx, a, req.ImportId)
		if err != nil {
			return nil, err
		}
		out, err := convertJSON[api.JiraImport](v)
		return api.AdminUpdateJiraImport200JSONResponse(out), err
	}
	v, err := s.jiraImportView(ctx, imp, nil)
	return api.AdminUpdateJiraImport200JSONResponse(v), err
}

func (s *Server) AdminDeleteJiraImport(ctx context.Context, req api.AdminDeleteJiraImportRequestObject) (api.AdminDeleteJiraImportResponseObject, error) {
	a, err := orgAdmin(ctx)
	if err != nil {
		return nil, err
	}
	if err := s.JiraImport.Delete(ctx, a, req.ImportId); err != nil {
		return nil, err
	}
	return api.AdminDeleteJiraImport204Response{}, nil
}

func (s *Server) AdminDeleteJiraImportCredentials(ctx context.Context, req api.AdminDeleteJiraImportCredentialsRequestObject) (api.AdminDeleteJiraImportCredentialsResponseObject, error) {
	a, err := orgAdmin(ctx)
	if err != nil {
		return nil, err
	}
	imp, err := s.JiraImport.DeleteCredentials(ctx, a, req.ImportId)
	v, err := s.jiraImportView(ctx, imp, err)
	return api.AdminDeleteJiraImportCredentials200JSONResponse(v), err
}

func (s *Server) AdminListJiraProjects(ctx context.Context, req api.AdminListJiraProjectsRequestObject) (api.AdminListJiraProjectsResponseObject, error) {
	a, err := orgAdmin(ctx)
	if err != nil {
		return nil, err
	}
	list, err := s.JiraImport.JiraProjects(ctx, a, req.ImportId)
	if err != nil {
		return nil, err
	}
	out, err := convertJSON[[]api.JiraProject](list)
	return api.AdminListJiraProjects200JSONResponse(out), err
}

func (s *Server) AdminScanJiraImport(ctx context.Context, req api.AdminScanJiraImportRequestObject) (api.AdminScanJiraImportResponseObject, error) {
	a, err := orgAdmin(ctx)
	if err != nil {
		return nil, err
	}
	imp, err := s.JiraImport.Scan(ctx, a, req.ImportId)
	v, err := s.jiraImportView(ctx, imp, err)
	return api.AdminScanJiraImport202JSONResponse(v), err
}

func (s *Server) AdminDryRunJiraImport(ctx context.Context, req api.AdminDryRunJiraImportRequestObject) (api.AdminDryRunJiraImportResponseObject, error) {
	a, err := orgAdmin(ctx)
	if err != nil {
		return nil, err
	}
	dry, err := s.JiraImport.DryRun(ctx, a, req.ImportId)
	if err != nil {
		return nil, err
	}
	out, err := convertJSON[api.JiraDryRun](dry)
	return api.AdminDryRunJiraImport200JSONResponse(out), err
}

func (s *Server) AdminRunJiraImport(ctx context.Context, req api.AdminRunJiraImportRequestObject) (api.AdminRunJiraImportResponseObject, error) {
	a, err := orgAdmin(ctx)
	if err != nil {
		return nil, err
	}
	imp, err := s.JiraImport.Import(ctx, a, req.ImportId)
	v, err := s.jiraImportView(ctx, imp, err)
	return api.AdminRunJiraImport202JSONResponse(v), err
}

func (s *Server) AdminCancelJiraImport(ctx context.Context, req api.AdminCancelJiraImportRequestObject) (api.AdminCancelJiraImportResponseObject, error) {
	a, err := orgAdmin(ctx)
	if err != nil {
		return nil, err
	}
	imp, err := s.JiraImport.Cancel(ctx, a, req.ImportId)
	v, err := s.jiraImportView(ctx, imp, err)
	return api.AdminCancelJiraImport200JSONResponse(v), err
}
