// fork: O13 — Open WebUI admin API: relay status and manual sub -> user links (ported from OpenWebUI controller/openwebui_link.go; link and unlink require develop's admin step-up proof and write an operation audit entry).

package controller

import (
	"errors"
	"fmt"
	"strconv"
	"strings"

	"github.com/QuantumNous/new-api/common"
	"github.com/QuantumNous/new-api/model"
	"github.com/QuantumNous/new-api/service"
	"github.com/QuantumNous/new-api/service/openwebui"
	"github.com/QuantumNous/new-api/setting/system_setting"

	"github.com/gin-gonic/gin"
	"gorm.io/gorm"
)

const (
	openWebUILinkConflictMessage = "This Open WebUI account or new-api user is already linked"
	openWebUILinkNotFoundMessage = "Open WebUI link not found"
	openWebUISubRequiredMessage  = "Open WebUI user id (sub) is required"

	// openWebUIBindingType is the binding_type of the admin.user.binding.clear
	// proof that removes a link.
	openWebUIBindingType = "openwebui"
)

type openWebUITokenStatus struct {
	Name   string `json:"name"`
	Exists bool   `json:"exists"`
	Status int    `json:"status"`
}

type openWebUILinkResponse struct {
	Id        int                    `json:"id"`
	Sub       string                 `json:"sub"`
	UserId    int                    `json:"user_id"`
	Email     string                 `json:"email"`
	LinkedBy  string                 `json:"linked_by"`
	CreatedAt int64                  `json:"created_at"`
	Tokens    []openWebUITokenStatus `json:"tokens"`
}

// GetOpenWebUIRelayStatus tells the admin UI which write-only secrets are
// set. The secrets themselves are hidden from GET /api/option/ by name.
func GetOpenWebUIRelayStatus(c *gin.Context) {
	s := system_setting.GetOpenWebUISettings()
	common.ApiSuccess(c, gin.H{
		"relay_enabled":                  s.RelayEnabled,
		"service_key_configured":         s.ServiceApiKey != "",
		"jwt_secret_configured":          s.JwtSecret != "",
		"jwt_previous_secret_configured": s.JwtPreviousSecret != "",
		"jwt_issuer":                     s.JwtIssuer,
		"jwt_leeway_seconds":             s.JwtLeewaySeconds,
		"allow_unsigned_requests":        s.AllowUnsignedRequests,
	})
}

func buildOpenWebUILinkResponse(link *model.OpenWebUILink) (*openWebUILinkResponse, error) {
	resp := &openWebUILinkResponse{
		Id: link.Id, Sub: link.Sub, UserId: link.UserId, Email: link.Email,
		LinkedBy: link.LinkedBy, CreatedAt: link.CreatedAt,
		Tokens: make([]openWebUITokenStatus, 0, 2),
	}
	for _, name := range []string{openwebui.TokenNameChat, openwebui.TokenNameTask} {
		status := openWebUITokenStatus{Name: name}
		token, err := model.FindOldestUserTokenByName(link.UserId, name)
		if err == nil {
			status.Exists = true
			status.Status = token.Status
		} else if !errors.Is(err, gorm.ErrRecordNotFound) {
			return nil, err
		}
		resp.Tokens = append(resp.Tokens, status)
	}
	return resp, nil
}

// requireLinkedUserManageable enforces the admin user role rule (admins
// manage users below them, root manages everyone) for the user a link points
// at. A dangling link (user deleted) passes so it can be seen and removed.
func requireLinkedUserManageable(c *gin.Context, userId int, deniedMessage string) bool {
	user, err := model.GetUserById(userId, false)
	if err != nil {
		if errors.Is(err, gorm.ErrRecordNotFound) {
			return true
		}
		common.ApiError(c, err)
		return false
	}
	if !canManageTargetRole(c.GetInt("role"), user.Role) {
		common.ApiErrorT(c, deniedMessage)
		return false
	}
	return true
}

func recordOpenWebUILinkAudit(c *gin.Context, action, content string, link *model.OpenWebUILink, method string) {
	params := map[string]any{
		"link_id":             link.Id,
		"sub":                 link.Sub,
		"user_id":             link.UserId,
		"verification_method": method,
	}
	if link.UserId != c.GetInt("id") {
		params["target_user_id"] = link.UserId
	}
	model.RecordOperationAuditLog(c.GetInt("id"), c.GetInt("role"), content, c.ClientIP(), action, params, auditOperatorInfo(c), nil, c)
	markAuditLogged(c)
}

// GetOpenWebUILink answers ?user_id= or ?sub=. "No link" is a successful
// response with data: null so the UI can render the link form.
func GetOpenWebUILink(c *gin.Context) {
	var link *model.OpenWebUILink
	var err error
	switch {
	case c.Query("user_id") != "":
		userId, convErr := strconv.Atoi(c.Query("user_id"))
		if convErr != nil || userId <= 0 {
			common.ApiErrorT(c, "Invalid parameters")
			return
		}
		link, err = model.GetOpenWebUILinkByUserId(userId)
	case strings.TrimSpace(c.Query("sub")) != "":
		link, err = model.GetOpenWebUILinkBySub(strings.TrimSpace(c.Query("sub")))
	default:
		common.ApiErrorT(c, "Invalid parameters")
		return
	}
	if err != nil {
		if errors.Is(err, gorm.ErrRecordNotFound) {
			common.ApiSuccess(c, nil)
			return
		}
		common.ApiError(c, err)
		return
	}
	if !requireLinkedUserManageable(c, link.UserId, "No permission to access users of same or higher level") {
		return
	}
	resp, err := buildOpenWebUILinkResponse(link)
	if err != nil {
		common.ApiError(c, err)
		return
	}
	common.ApiSuccess(c, resp)
}

// CreateOpenWebUILink links an Open WebUI account (JWT sub) to a user, which
// decides whose wallet that account's relay requests bill. Like a password
// reset it changes who can act as the user, so it needs an admin.user.update
// step-up proof for the target user (checked after validation and the
// conflict pre-check, so a refused request does not burn the proof).
func CreateOpenWebUILink(c *gin.Context) {
	var req struct {
		UserId int    `json:"user_id"`
		Sub    string `json:"sub"`
	}
	if err := common.DecodeJson(c.Request.Body, &req); err != nil {
		common.ApiErrorT(c, "Invalid parameters")
		return
	}
	req.Sub = strings.TrimSpace(req.Sub)
	if req.Sub == "" {
		common.ApiErrorT(c, openWebUISubRequiredMessage)
		return
	}
	if req.UserId <= 0 {
		common.ApiErrorT(c, "Invalid parameters")
		return
	}
	user, err := model.GetUserById(req.UserId, false)
	if err != nil {
		if errors.Is(err, gorm.ErrRecordNotFound) {
			common.ApiErrorT(c, "User does not exist")
		} else {
			common.ApiError(c, err)
		}
		return
	}
	if !canManageTargetRole(c.GetInt("role"), user.Role) {
		common.ApiErrorT(c, "No permission to update users of same or higher permission level")
		return
	}
	if taken, err := openWebUILinkTaken(req.Sub, user.Id); err != nil {
		common.ApiError(c, err)
		return
	} else if taken {
		common.ApiErrorT(c, openWebUILinkConflictMessage)
		return
	}
	authorization := requireAdminUserProof(c, service.VerificationScopeAdminUserUpdate, service.AdminUserContext{UserID: user.Id})
	if authorization == nil {
		return
	}
	link := &model.OpenWebUILink{
		Sub:      req.Sub,
		UserId:   user.Id,
		Email:    strings.ToLower(strings.TrimSpace(user.Email)),
		LinkedBy: model.OpenWebUILinkedByAdmin,
	}
	if err := model.CreateOpenWebUILink(link); err != nil {
		if errors.Is(err, model.ErrOpenWebUILinkConflict) {
			common.ApiErrorT(c, openWebUILinkConflictMessage)
			return
		}
		common.ApiError(c, err)
		return
	}
	openwebui.InvalidateSub(link.Sub)
	recordOpenWebUILinkAudit(c, "openwebui.link.create",
		fmt.Sprintf("Linked Open WebUI account %s to user %d", link.Sub, link.UserId), link, authorization.Method)
	resp, err := buildOpenWebUILinkResponse(link)
	if err != nil {
		common.ApiError(c, err)
		return
	}
	common.ApiSuccess(c, resp)
}

// openWebUILinkTaken reports whether the sub or the user is already linked.
func openWebUILinkTaken(sub string, userId int) (bool, error) {
	for _, lookup := range []func() (*model.OpenWebUILink, error){
		func() (*model.OpenWebUILink, error) { return model.GetOpenWebUILinkBySub(sub) },
		func() (*model.OpenWebUILink, error) { return model.GetOpenWebUILinkByUserId(userId) },
	} {
		if _, err := lookup(); err == nil {
			return true, nil
		} else if !errors.Is(err, gorm.ErrRecordNotFound) {
			return false, err
		}
	}
	return false, nil
}

// DeleteOpenWebUILink removes a link. Like clearing a login binding it needs
// an admin.user.binding.clear proof for the linked user with binding_type
// "openwebui" (also for a dangling link whose user was deleted). The
// per-user tokens are kept.
func DeleteOpenWebUILink(c *gin.Context) {
	id, err := strconv.Atoi(c.Param("id"))
	if err != nil || id <= 0 {
		common.ApiErrorT(c, "Invalid parameters")
		return
	}
	link, err := model.GetOpenWebUILinkById(id)
	if err != nil {
		if errors.Is(err, gorm.ErrRecordNotFound) {
			common.ApiErrorT(c, openWebUILinkNotFoundMessage)
			return
		}
		common.ApiError(c, err)
		return
	}
	if !requireLinkedUserManageable(c, link.UserId, "No permission to update users of same or higher permission level") {
		return
	}
	authorization := requireAdminUserProof(c, service.VerificationScopeAdminUserBindingClear, service.AdminUserBindingContext{UserID: link.UserId, BindingType: openWebUIBindingType})
	if authorization == nil {
		return
	}
	if err := model.DeleteOpenWebUILink(id); err != nil {
		if errors.Is(err, gorm.ErrRecordNotFound) {
			common.ApiErrorT(c, openWebUILinkNotFoundMessage)
			return
		}
		common.ApiError(c, err)
		return
	}
	openwebui.InvalidateSub(link.Sub)
	recordOpenWebUILinkAudit(c, "openwebui.link.delete",
		fmt.Sprintf("Unlinked Open WebUI account %s from user %d", link.Sub, link.UserId), link, authorization.Method)
	common.ApiSuccess(c, nil)
}
