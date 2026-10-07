package controller

import (
	"errors"
	"strconv"
	"strings"

	"github.com/QuantumNous/new-api/common"
	"github.com/QuantumNous/new-api/i18n"
	"github.com/QuantumNous/new-api/model"
	"github.com/QuantumNous/new-api/service/openwebui"
	"github.com/QuantumNous/new-api/setting/system_setting"

	"github.com/gin-gonic/gin"
	"gorm.io/gorm"
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

// requireManageableUser loads the target user and enforces the same role
// rule as the other admin user endpoints: admins manage users below them,
// root manages everyone.
func requireManageableUser(c *gin.Context, userId int) (*model.User, bool) {
	user, err := model.GetUserById(userId, false)
	if err != nil {
		if errors.Is(err, gorm.ErrRecordNotFound) {
			common.ApiErrorI18n(c, i18n.MsgUserNotExists)
		} else {
			common.ApiError(c, err)
		}
		return nil, false
	}
	if !canManageTargetRole(c.GetInt("role"), user.Role) {
		common.ApiErrorI18n(c, i18n.MsgAuthInsufficientPrivilege)
		return nil, false
	}
	return user, true
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
			common.ApiErrorI18n(c, i18n.MsgInvalidParams)
			return
		}
		link, err = model.GetOpenWebUILinkByUserId(userId)
	case strings.TrimSpace(c.Query("sub")) != "":
		link, err = model.GetOpenWebUILinkBySub(strings.TrimSpace(c.Query("sub")))
	default:
		common.ApiErrorI18n(c, i18n.MsgInvalidParams)
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
	resp, err := buildOpenWebUILinkResponse(link)
	if err != nil {
		common.ApiError(c, err)
		return
	}
	common.ApiSuccess(c, resp)
}

func CreateOpenWebUILink(c *gin.Context) {
	var req struct {
		UserId int    `json:"user_id"`
		Sub    string `json:"sub"`
	}
	if err := common.DecodeJson(c.Request.Body, &req); err != nil {
		common.ApiErrorI18n(c, i18n.MsgInvalidParams)
		return
	}
	req.Sub = strings.TrimSpace(req.Sub)
	if req.Sub == "" {
		common.ApiErrorI18n(c, i18n.MsgOpenWebUISubRequired)
		return
	}
	if req.UserId <= 0 {
		common.ApiErrorI18n(c, i18n.MsgInvalidParams)
		return
	}
	user, ok := requireManageableUser(c, req.UserId)
	if !ok {
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
			common.ApiErrorI18n(c, i18n.MsgOpenWebUILinkConflict)
			return
		}
		common.ApiError(c, err)
		return
	}
	openwebui.InvalidateSub(link.Sub)
	resp, err := buildOpenWebUILinkResponse(link)
	if err != nil {
		common.ApiError(c, err)
		return
	}
	common.ApiSuccess(c, resp)
}

func DeleteOpenWebUILink(c *gin.Context) {
	id, err := strconv.Atoi(c.Param("id"))
	if err != nil || id <= 0 {
		common.ApiErrorI18n(c, i18n.MsgInvalidParams)
		return
	}
	link, err := model.GetOpenWebUILinkById(id)
	if err != nil {
		if errors.Is(err, gorm.ErrRecordNotFound) {
			common.ApiErrorI18n(c, i18n.MsgOpenWebUILinkNotFound)
			return
		}
		common.ApiError(c, err)
		return
	}
	// A deleted user leaves a dangling link; admins must still be able to
	// remove it, so only enforce the role rule when the user exists.
	if user, getErr := model.GetUserById(link.UserId, false); getErr == nil {
		if !canManageTargetRole(c.GetInt("role"), user.Role) {
			common.ApiErrorI18n(c, i18n.MsgAuthInsufficientPrivilege)
			return
		}
	} else if !errors.Is(getErr, gorm.ErrRecordNotFound) {
		common.ApiError(c, getErr)
		return
	}
	if err := model.DeleteOpenWebUILink(id); err != nil {
		common.ApiError(c, err)
		return
	}
	openwebui.InvalidateSub(link.Sub)
	common.ApiSuccess(c, nil)
}
