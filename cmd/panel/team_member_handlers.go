package main

import (
	"encoding/json"
	"errors"
	"io"
	"net/http"
	"net/mail"
	"strings"

	"github.com/alicelik/celikpanel/internal/auth"
	"github.com/alicelik/celikpanel/internal/core"
	"github.com/alicelik/celikpanel/internal/repositories"
)

type teamMemberAccessRequest struct {
	SubscriptionPermissions []core.TeamSubscriptionPermission `json:"subscription_permissions"`
	DomainPermissions       []core.TeamDomainPermission       `json:"domain_permissions"`
}

func (r teamMemberAccessRequest) coreAccess() core.TeamMemberAccess {
	return core.TeamMemberAccess{
		SubscriptionPermissions: r.SubscriptionPermissions,
		DomainPermissions:       r.DomainPermissions,
	}
}

type createTeamMemberRequest struct {
	OwnerID  *int                    `json:"owner_id,omitempty"`
	Username string                  `json:"username"`
	Email    string                  `json:"email"`
	Password string                  `json:"password"`
	Status   string                  `json:"status,omitempty"`
	Access   teamMemberAccessRequest `json:"access"`
}

type updateTeamMemberRequest struct {
	Username *string                  `json:"username,omitempty"`
	Email    *string                  `json:"email,omitempty"`
	Password *string                  `json:"password,omitempty"`
	Status   *string                  `json:"status,omitempty"`
	Access   *teamMemberAccessRequest `json:"access,omitempty"`
}

func (p *Panel) handleTeamMembers(w http.ResponseWriter, r *http.Request) {
	if p == nil || p.db == nil {
		writeServerError(w, errors.New("team member database is unavailable"))
		return
	}
	// The collection route is exact. strictRouteSegments intentionally rejects
	// an empty remainder and is therefore only appropriate for item routes.
	if r == nil || r.URL == nil || r.URL.EscapedPath() != "/api/v1/team-members" {
		writeClientError(w, http.StatusNotFound, "not found")
		return
	}
	caller := currentCaller(r)
	if !teamMemberCallerAllowed(caller) {
		writeClientError(w, http.StatusForbidden, "team member management is not allowed for this account")
		return
	}

	repository := repositories.NewTeamMemberRepository(p.db.GetDB())
	switch r.Method {
	case http.MethodGet:
		ownerID, ok := teamMemberOwnerFromQuery(w, r, caller)
		if !ok {
			return
		}
		members, err := repository.ListByOwner(r.Context(), ownerID)
		if err != nil {
			writeTeamMemberRepositoryError(w, err)
			return
		}
		writeTeamMemberJSON(w, http.StatusOK, map[string]any{"team_members": members})

	case http.MethodPost:
		if len(r.URL.Query()) != 0 {
			writeClientError(w, http.StatusBadRequest, "query parameters are not allowed")
			return
		}
		var request createTeamMemberRequest
		if err := decodeTeamMemberJSON(r, &request); err != nil {
			writeClientError(w, http.StatusBadRequest, "invalid team member request")
			return
		}
		ownerID, ok := teamMemberOwnerFromCreate(w, caller, request.OwnerID)
		if !ok {
			return
		}
		if request.Status == "" {
			request.Status = "active"
		}
		if err := validateTeamMemberIdentity(request.Username, request.Email, request.Password, request.Status, true); err != nil {
			writeClientError(w, http.StatusBadRequest, err.Error())
			return
		}
		passwordHash, err := auth.HashPassword(request.Password)
		if err != nil {
			writeServerError(w, err)
			return
		}
		member, err := repository.Create(r.Context(), ownerID, repositories.TeamMemberCreate{
			Username:     strings.TrimSpace(request.Username),
			Email:        strings.TrimSpace(request.Email),
			PasswordHash: passwordHash,
			Status:       request.Status,
			Access:       request.Access.coreAccess(),
		})
		if err != nil {
			writeTeamMemberRepositoryError(w, err)
			return
		}
		p.audit(r, "team_member.create", "team_member", member.ID)
		writeTeamMemberJSON(w, http.StatusCreated, map[string]any{"team_member": member})

	default:
		w.Header().Set("Allow", "GET, POST")
		writeClientError(w, http.StatusMethodNotAllowed, "method not allowed")
	}
}

func (p *Panel) handleTeamMemberByID(w http.ResponseWriter, r *http.Request) {
	if p == nil || p.db == nil {
		writeServerError(w, errors.New("team member database is unavailable"))
		return
	}
	segments, ok := strictRouteSegments(r, "/api/v1/team-members/")
	if !ok || len(segments) != 1 {
		writeClientError(w, http.StatusNotFound, "not found")
		return
	}
	memberID, ok := strictPositiveID(segments[0])
	if !ok {
		writeClientError(w, http.StatusNotFound, "not found")
		return
	}
	caller := currentCaller(r)
	if !teamMemberCallerAllowed(caller) {
		writeClientError(w, http.StatusForbidden, "team member management is not allowed for this account")
		return
	}
	ownerID, ok := teamMemberOwnerFromQuery(w, r, caller)
	if !ok {
		return
	}

	repository := repositories.NewTeamMemberRepository(p.db.GetDB())
	switch r.Method {
	case http.MethodGet:
		member, err := repository.GetByOwner(r.Context(), ownerID, memberID)
		if err != nil {
			writeTeamMemberRepositoryError(w, err)
			return
		}
		writeTeamMemberJSON(w, http.StatusOK, map[string]any{"team_member": member})

	case http.MethodPut:
		var request updateTeamMemberRequest
		if err := decodeTeamMemberJSON(r, &request); err != nil {
			writeClientError(w, http.StatusBadRequest, "invalid team member request")
			return
		}
		if request.Username == nil && request.Email == nil && request.Password == nil && request.Status == nil && request.Access == nil {
			writeClientError(w, http.StatusBadRequest, "at least one field must be updated")
			return
		}
		if err := validateTeamMemberUpdateRequest(request); err != nil {
			writeClientError(w, http.StatusBadRequest, err.Error())
			return
		}
		update := repositories.TeamMemberUpdate{
			Username: request.Username,
			Email:    request.Email,
			Status:   request.Status,
		}
		if request.Username != nil {
			trimmed := strings.TrimSpace(*request.Username)
			update.Username = &trimmed
		}
		if request.Email != nil {
			trimmed := strings.TrimSpace(*request.Email)
			update.Email = &trimmed
		}
		if request.Password != nil {
			hash, err := auth.HashPassword(*request.Password)
			if err != nil {
				writeServerError(w, err)
				return
			}
			update.PasswordHash = &hash
		}
		if request.Access != nil {
			access := request.Access.coreAccess()
			update.Access = &access
		}
		member, revoked, err := repository.Update(r.Context(), ownerID, memberID, update)
		if err != nil {
			writeTeamMemberRepositoryError(w, err)
			return
		}
		if revoked {
			revokePendingLogins(memberID)
		}
		p.audit(r, "team_member.update", "team_member", memberID)
		writeTeamMemberJSON(w, http.StatusOK, map[string]any{"team_member": member})

	case http.MethodDelete:
		member, err := repository.Delete(r.Context(), ownerID, memberID)
		if err != nil {
			writeTeamMemberRepositoryError(w, err)
			return
		}
		revokePendingLogins(memberID)
		p.audit(r, "team_member.delete", "team_member", member.ID)
		w.WriteHeader(http.StatusNoContent)

	default:
		w.Header().Set("Allow", "GET, PUT, DELETE")
		writeClientError(w, http.StatusMethodNotAllowed, "method not allowed")
	}
}

func teamMemberCallerAllowed(caller *Caller) bool {
	return caller != nil && caller.validAuthorizationIdentity() &&
		(caller.hasAccountRole(roleAdmin) || caller.hasAccountRole(roleCustomer))
}

func teamMemberOwnerFromCreate(w http.ResponseWriter, caller *Caller, requested *int) (int, bool) {
	if caller.hasAccountRole(roleAdmin) {
		if requested == nil || *requested <= 0 {
			writeClientError(w, http.StatusBadRequest, "owner_id is required for administrators")
			return 0, false
		}
		return *requested, true
	}
	if requested != nil {
		writeClientError(w, http.StatusBadRequest, "owner_id is derived from the signed-in customer")
		return 0, false
	}
	return caller.ID, true
}

func teamMemberOwnerFromQuery(w http.ResponseWriter, r *http.Request, caller *Caller) (int, bool) {
	query := r.URL.Query()
	for key := range query {
		if key != "owner_id" {
			writeClientError(w, http.StatusBadRequest, "unknown query parameter")
			return 0, false
		}
	}
	values, supplied := query["owner_id"]
	if caller.hasAccountRole(roleAdmin) {
		if !supplied || len(values) != 1 {
			writeClientError(w, http.StatusBadRequest, "owner_id is required for administrators")
			return 0, false
		}
		ownerID, ok := strictPositiveID(values[0])
		if !ok {
			writeClientError(w, http.StatusBadRequest, "owner_id must be a positive integer")
			return 0, false
		}
		return ownerID, true
	}
	if supplied {
		writeClientError(w, http.StatusBadRequest, "owner_id is derived from the signed-in customer")
		return 0, false
	}
	return caller.ID, true
}

func decodeTeamMemberJSON(r *http.Request, destination any) error {
	decoder := json.NewDecoder(io.LimitReader(r.Body, 1<<20))
	decoder.DisallowUnknownFields()
	if err := decoder.Decode(destination); err != nil {
		return err
	}
	var trailing any
	if err := decoder.Decode(&trailing); !errors.Is(err, io.EOF) {
		return errors.New("request must contain one JSON object")
	}
	return nil
}

func validateTeamMemberIdentity(username, email, password, status string, passwordRequired bool) error {
	username = strings.TrimSpace(username)
	email = strings.TrimSpace(email)
	if err := auth.ValidateUsername(username); err != nil {
		return errors.New("invalid username")
	}
	address, err := mail.ParseAddress(email)
	if err != nil || address.Address != email {
		return errors.New("invalid email address")
	}
	if passwordRequired && len(password) < minPasswordLen {
		return errors.New("password must be at least 8 characters")
	}
	if status != "active" && status != "suspended" {
		return errors.New("status must be active or suspended")
	}
	return nil
}

func validateTeamMemberUpdateRequest(request updateTeamMemberRequest) error {
	if request.Username != nil {
		if err := auth.ValidateUsername(strings.TrimSpace(*request.Username)); err != nil {
			return errors.New("invalid username")
		}
	}
	if request.Email != nil {
		email := strings.TrimSpace(*request.Email)
		address, err := mail.ParseAddress(email)
		if err != nil || address.Address != email {
			return errors.New("invalid email address")
		}
	}
	if request.Password != nil && len(*request.Password) < minPasswordLen {
		return errors.New("password must be at least 8 characters")
	}
	if request.Status != nil && *request.Status != "active" && *request.Status != "suspended" {
		return errors.New("status must be active or suspended")
	}
	if request.Access != nil && (request.Access.SubscriptionPermissions == nil || request.Access.DomainPermissions == nil) {
		return errors.New("access must include subscription_permissions and domain_permissions")
	}
	return nil
}

func writeTeamMemberRepositoryError(w http.ResponseWriter, err error) {
	switch {
	case errors.Is(err, repositories.ErrTeamMemberNotFound),
		errors.Is(err, repositories.ErrTeamMemberOwnerNotFound),
		errors.Is(err, repositories.ErrTeamMemberForeignScope):
		writeClientError(w, http.StatusNotFound, "team member or permission scope not found")
	case errors.Is(err, repositories.ErrInvalidTeamPermission):
		writeClientError(w, http.StatusBadRequest, "invalid team member permission")
	case errors.Is(err, repositories.ErrTeamMemberConflict):
		writeClientError(w, http.StatusConflict, "username or email is already in use")
	default:
		writeServerError(w, err)
	}
}

func writeTeamMemberJSON(w http.ResponseWriter, status int, value any) {
	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(status)
	_ = json.NewEncoder(w).Encode(value)
}
