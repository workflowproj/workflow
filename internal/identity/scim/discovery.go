package scim

import (
	"net/http"
)

func (h *Handler) serviceProviderConfig(_ http.ResponseWriter, _ *http.Request) (any, int, error) {
	return map[string]any{
		"schemas":          []string{"urn:ietf:params:scim:schemas:core:2.0:ServiceProviderConfig"},
		"documentationUri": "https://docs.workflowproj.io/integrations/identity/#scim-20",
		"patch":            map[string]any{"supported": true},
		"bulk":             map[string]any{"supported": false, "maxOperations": 0, "maxPayloadSize": 0},
		"filter":           map[string]any{"supported": true, "maxResults": maxCount},
		"changePassword":   map[string]any{"supported": false},
		"sort":             map[string]any{"supported": false},
		"etag":             map[string]any{"supported": false},
		"authenticationSchemes": []map[string]any{{
			"type": "oauthbearertoken", "name": "Bearer token", "primary": true,
			"description": "A per-connection token created under Administration → Provisioning.",
		}},
		"meta": map[string]any{"resourceType": "ServiceProviderConfig", "location": h.BaseURL + "/ServiceProviderConfig"},
	}, http.StatusOK, nil
}

func (h *Handler) resourceTypeList() []map[string]any {
	return []map[string]any{
		{
			"schemas": []string{"urn:ietf:params:scim:schemas:core:2.0:ResourceType"}, "id": "User", "name": "User",
			"endpoint": "/Users", "schema": SchemaUser, "description": "People",
			"schemaExtensions": []map[string]any{{"schema": SchemaEnterprise, "required": false}},
			"meta":             map[string]any{"resourceType": "ResourceType", "location": h.BaseURL + "/ResourceTypes/User"},
		},
		{
			"schemas": []string{"urn:ietf:params:scim:schemas:core:2.0:ResourceType"}, "id": "Group", "name": "Group",
			"endpoint": "/Groups", "schema": SchemaGroup, "description": "Groups (map to project roles)",
			"meta": map[string]any{"resourceType": "ResourceType", "location": h.BaseURL + "/ResourceTypes/Group"},
		},
	}
}

func (h *Handler) resourceTypes(_ http.ResponseWriter, r *http.Request) (any, int, error) {
	return listResponse(h.resourceTypeList(), parseList(r)), http.StatusOK, nil
}

func (h *Handler) resourceType(_ http.ResponseWriter, r *http.Request) (any, int, error) {
	for _, rt := range h.resourceTypeList() {
		if rt["id"] == r.PathValue("id") {
			return rt, http.StatusOK, nil
		}
	}
	return nil, 0, errf(http.StatusNotFound, "", "resource type not found")
}

func attr(name, typ string, required bool, extra map[string]any) map[string]any {
	a := map[string]any{
		"name": name, "type": typ, "multiValued": false, "required": required, "caseExact": false,
		"mutability": "readWrite", "returned": "default", "uniqueness": "none",
	}
	for k, v := range extra {
		a[k] = v
	}
	return a
}

func (h *Handler) schemaList() []map[string]any {
	sub := func(attrs ...map[string]any) map[string]any { return map[string]any{"subAttributes": attrs} }
	user := map[string]any{
		"schemas": []string{"urn:ietf:params:scim:schemas:core:2.0:Schema"}, "id": SchemaUser, "name": "User", "description": "User account",
		"attributes": []map[string]any{
			attr("userName", "string", true, map[string]any{"uniqueness": "server"}),
			attr("name", "complex", false, sub(attr("formatted", "string", false, nil), attr("givenName", "string", false, nil), attr("familyName", "string", false, nil))),
			attr("displayName", "string", false, nil),
			attr("active", "boolean", false, nil),
			attr("emails", "complex", false, map[string]any{"multiValued": true, "subAttributes": []map[string]any{
				attr("value", "string", false, nil), attr("type", "string", false, nil), attr("primary", "boolean", false, nil),
			}}),
			attr("groups", "complex", false, map[string]any{"multiValued": true, "mutability": "readOnly", "subAttributes": []map[string]any{
				attr("value", "string", false, nil), attr("display", "string", false, nil),
			}}),
		},
		"meta": map[string]any{"resourceType": "Schema", "location": h.BaseURL + "/Schemas/" + SchemaUser},
	}
	group := map[string]any{
		"schemas": []string{"urn:ietf:params:scim:schemas:core:2.0:Schema"}, "id": SchemaGroup, "name": "Group", "description": "Group",
		"attributes": []map[string]any{
			attr("displayName", "string", true, nil),
			attr("members", "complex", false, map[string]any{"multiValued": true, "subAttributes": []map[string]any{
				attr("value", "string", false, map[string]any{"mutability": "immutable"}), attr("display", "string", false, nil),
			}}),
		},
		"meta": map[string]any{"resourceType": "Schema", "location": h.BaseURL + "/Schemas/" + SchemaGroup},
	}
	return []map[string]any{user, group}
}

func (h *Handler) schemas(_ http.ResponseWriter, r *http.Request) (any, int, error) {
	return listResponse(h.schemaList(), parseList(r)), http.StatusOK, nil
}

func (h *Handler) schema(_ http.ResponseWriter, r *http.Request) (any, int, error) {
	for _, s := range h.schemaList() {
		if s["id"] == r.PathValue("id") {
			return s, http.StatusOK, nil
		}
	}
	return nil, 0, errf(http.StatusNotFound, "", "schema not found")
}
