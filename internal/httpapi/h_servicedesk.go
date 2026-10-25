package httpapi

import (
	"context"

	"github.com/workflowproj/workflow/internal/api"
)

// ---------------------------------------------------------------------------
// Service desk: settings, request types, customers, queues, SLAs and requests (agents)

func (s *Server) GetServiceDesk(ctx context.Context, req api.GetServiceDeskRequestObject) (api.GetServiceDeskResponseObject, error) {
	a, err := actor(ctx)
	if err != nil {
		return nil, err
	}
	d, err := s.ServiceDesk.Desk(ctx, a, req.ProjectKey)
	if err != nil {
		return nil, err
	}
	return api.GetServiceDesk200JSONResponse(d), nil
}

func (s *Server) UpdateServiceDesk(ctx context.Context, req api.UpdateServiceDeskRequestObject) (api.UpdateServiceDeskResponseObject, error) {
	a, err := actor(ctx)
	if err != nil {
		return nil, err
	}
	d, err := s.ServiceDesk.UpdateDesk(ctx, a, req.ProjectKey, *req.Body)
	if err != nil {
		return nil, err
	}
	return api.UpdateServiceDesk200JSONResponse(d), nil
}

func (s *Server) ListRequestTypes(ctx context.Context, req api.ListRequestTypesRequestObject) (api.ListRequestTypesResponseObject, error) {
	a, err := actor(ctx)
	if err != nil {
		return nil, err
	}
	rts, err := s.ServiceDesk.ListRequestTypes(ctx, a, req.ProjectKey)
	if err != nil {
		return nil, err
	}
	return api.ListRequestTypes200JSONResponse(rts), nil
}

func (s *Server) CreateRequestType(ctx context.Context, req api.CreateRequestTypeRequestObject) (api.CreateRequestTypeResponseObject, error) {
	a, err := actor(ctx)
	if err != nil {
		return nil, err
	}
	rt, err := s.ServiceDesk.CreateRequestType(ctx, a, req.ProjectKey, *req.Body)
	if err != nil {
		return nil, err
	}
	return api.CreateRequestType201JSONResponse(rt), nil
}

func (s *Server) UpdateRequestType(ctx context.Context, req api.UpdateRequestTypeRequestObject) (api.UpdateRequestTypeResponseObject, error) {
	a, err := actor(ctx)
	if err != nil {
		return nil, err
	}
	rt, err := s.ServiceDesk.UpdateRequestType(ctx, a, req.RequestTypeId, *req.Body)
	if err != nil {
		return nil, err
	}
	return api.UpdateRequestType200JSONResponse(rt), nil
}

func (s *Server) DeleteRequestType(ctx context.Context, req api.DeleteRequestTypeRequestObject) (api.DeleteRequestTypeResponseObject, error) {
	a, err := actor(ctx)
	if err != nil {
		return nil, err
	}
	if err := s.ServiceDesk.DeleteRequestType(ctx, a, req.RequestTypeId); err != nil {
		return nil, err
	}
	return api.DeleteRequestType204Response{}, nil
}

func (s *Server) ListServiceCustomers(ctx context.Context, req api.ListServiceCustomersRequestObject) (api.ListServiceCustomersResponseObject, error) {
	a, err := actor(ctx)
	if err != nil {
		return nil, err
	}
	cs, err := s.ServiceDesk.ListCustomers(ctx, a, req.ProjectKey, deref(req.Params.Q), limitOr(req.Params.Limit, 100))
	if err != nil {
		return nil, err
	}
	return api.ListServiceCustomers200JSONResponse(cs), nil
}

func (s *Server) AddServiceCustomer(ctx context.Context, req api.AddServiceCustomerRequestObject) (api.AddServiceCustomerResponseObject, error) {
	a, err := actor(ctx)
	if err != nil {
		return nil, err
	}
	out, err := s.ServiceDesk.AddCustomer(ctx, a, req.ProjectKey, *req.Body, requestIP(ctx))
	if err != nil {
		return nil, err
	}
	return api.AddServiceCustomer201JSONResponse(out), nil
}

func (s *Server) RemoveServiceCustomer(ctx context.Context, req api.RemoveServiceCustomerRequestObject) (api.RemoveServiceCustomerResponseObject, error) {
	a, err := actor(ctx)
	if err != nil {
		return nil, err
	}
	if err := s.ServiceDesk.RemoveCustomer(ctx, a, req.ProjectKey, req.UserId); err != nil {
		return nil, err
	}
	return api.RemoveServiceCustomer204Response{}, nil
}

func (s *Server) ListQueues(ctx context.Context, req api.ListQueuesRequestObject) (api.ListQueuesResponseObject, error) {
	a, err := actor(ctx)
	if err != nil {
		return nil, err
	}
	qs, err := s.ServiceDesk.ListQueues(ctx, a, req.ProjectKey)
	if err != nil {
		return nil, err
	}
	return api.ListQueues200JSONResponse(qs), nil
}

func (s *Server) CreateQueue(ctx context.Context, req api.CreateQueueRequestObject) (api.CreateQueueResponseObject, error) {
	a, err := actor(ctx)
	if err != nil {
		return nil, err
	}
	q, err := s.ServiceDesk.CreateQueue(ctx, a, req.ProjectKey, *req.Body)
	if err != nil {
		return nil, err
	}
	return api.CreateQueue201JSONResponse(q), nil
}

func (s *Server) UpdateQueue(ctx context.Context, req api.UpdateQueueRequestObject) (api.UpdateQueueResponseObject, error) {
	a, err := actor(ctx)
	if err != nil {
		return nil, err
	}
	q, err := s.ServiceDesk.UpdateQueue(ctx, a, req.QueueId, *req.Body)
	if err != nil {
		return nil, err
	}
	return api.UpdateQueue200JSONResponse(q), nil
}

func (s *Server) DeleteQueue(ctx context.Context, req api.DeleteQueueRequestObject) (api.DeleteQueueResponseObject, error) {
	a, err := actor(ctx)
	if err != nil {
		return nil, err
	}
	if err := s.ServiceDesk.DeleteQueue(ctx, a, req.QueueId); err != nil {
		return nil, err
	}
	return api.DeleteQueue204Response{}, nil
}

func (s *Server) ListQueueIssues(ctx context.Context, req api.ListQueueIssuesRequestObject) (api.ListQueueIssuesResponseObject, error) {
	a, err := actor(ctx)
	if err != nil {
		return nil, err
	}
	offset := 0
	if req.Params.Offset != nil {
		offset = max(*req.Params.Offset, 0)
	}
	out, err := s.ServiceDesk.QueueIssues(ctx, a, req.QueueId, limitOr(req.Params.Limit, 50), offset)
	if err != nil {
		return nil, err
	}
	return api.ListQueueIssues200JSONResponse(out), nil
}

func (s *Server) GetSlaConfig(ctx context.Context, req api.GetSlaConfigRequestObject) (api.GetSlaConfigResponseObject, error) {
	a, err := actor(ctx)
	if err != nil {
		return nil, err
	}
	c, err := s.ServiceDesk.SLAConfig(ctx, a, req.ProjectKey)
	if err != nil {
		return nil, err
	}
	return api.GetSlaConfig200JSONResponse(c), nil
}

func (s *Server) UpdateSlaConfig(ctx context.Context, req api.UpdateSlaConfigRequestObject) (api.UpdateSlaConfigResponseObject, error) {
	a, err := actor(ctx)
	if err != nil {
		return nil, err
	}
	c, err := s.ServiceDesk.UpdateSLAConfig(ctx, a, req.ProjectKey, *req.Body)
	if err != nil {
		return nil, err
	}
	return api.UpdateSlaConfig200JSONResponse(c), nil
}

func (s *Server) GetServiceRequest(ctx context.Context, req api.GetServiceRequestRequestObject) (api.GetServiceRequestResponseObject, error) {
	a, err := actor(ctx)
	if err != nil {
		return nil, err
	}
	r, err := s.ServiceDesk.Request(ctx, a, req.IssueKey)
	if err != nil {
		return nil, err
	}
	return api.GetServiceRequest200JSONResponse(r), nil
}

func (s *Server) UpdateServiceRequest(ctx context.Context, req api.UpdateServiceRequestRequestObject) (api.UpdateServiceRequestResponseObject, error) {
	a, err := actor(ctx)
	if err != nil {
		return nil, err
	}
	r, err := s.ServiceDesk.SetRequestType(ctx, a, req.IssueKey, req.Body.RequestTypeId)
	if err != nil {
		return nil, err
	}
	return api.UpdateServiceRequest200JSONResponse(r), nil
}

func (s *Server) AddRequestParticipant(ctx context.Context, req api.AddRequestParticipantRequestObject) (api.AddRequestParticipantResponseObject, error) {
	a, err := actor(ctx)
	if err != nil {
		return nil, err
	}
	r, err := s.ServiceDesk.AddParticipant(ctx, a, req.IssueKey, req.Body.Email)
	if err != nil {
		return nil, err
	}
	return api.AddRequestParticipant200JSONResponse(r), nil
}

func (s *Server) RemoveRequestParticipant(ctx context.Context, req api.RemoveRequestParticipantRequestObject) (api.RemoveRequestParticipantResponseObject, error) {
	a, err := actor(ctx)
	if err != nil {
		return nil, err
	}
	if err := s.ServiceDesk.RemoveParticipant(ctx, a, req.IssueKey, req.UserId); err != nil {
		return nil, err
	}
	return api.RemoveRequestParticipant204Response{}, nil
}

func (s *Server) DecideApproval(ctx context.Context, req api.DecideApprovalRequestObject) (api.DecideApprovalResponseObject, error) {
	a, err := actor(ctx)
	if err != nil {
		return nil, err
	}
	r, err := s.ServiceDesk.DecideAsAgent(ctx, a, req.IssueKey, req.ApprovalId, *req.Body)
	if err != nil {
		return nil, err
	}
	return api.DecideApproval200JSONResponse(r), nil
}
