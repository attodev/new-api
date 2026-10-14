package controller

import (
	"errors"
	"net/http"

	"github.com/QuantumNous/new-api/common"
	"github.com/QuantumNous/new-api/model"
	"github.com/gin-gonic/gin"
)

// fork: O3 — RejectDisabledOrganizationMutation is the single choke point that
// makes "disabled" mean "cannot be used" for /api/organization/*: every write
// (member edits, membership, imports, plans, plan assignment, organization
// wallet pay/amount) from a member of a disabled organization is refused.
// Reads keep working so the organization can still see its data, and root is
// not affected (it manages disabled organizations through ?organization_id=
// and /api/organizations). Registered on the /api/organization group in
// router/organization-router.go.
//
// The same choke point refuses writes from a member an organization admin
// suspended (users.organization_member_status = 2, plan decision 7) with 403:
// a suspended admin cannot resume members, raise caps, import or assign plans.
// The owner cannot be suspended, so organization top-ups are unaffected; the
// suspended member's own reads stay allowed.
func RejectDisabledOrganizationMutation() gin.HandlerFunc {
	return func(c *gin.Context) {
		switch c.Request.Method {
		case http.MethodGet, http.MethodHead, http.MethodOptions:
			c.Next()
			return
		}
		if c.GetInt("role") == common.RoleRootUser {
			c.Next()
			return
		}
		disabled, err := model.IsUserOrganizationDisabled(c.GetInt("id"))
		if err != nil {
			common.ApiError(c, err)
			c.Abort()
			return
		}
		if disabled {
			common.ApiError(c, model.ErrOrganizationDisabled)
			c.Abort()
			return
		}
		if err := model.RequireActiveOrganizationMember(c.GetInt("id")); err != nil {
			if errors.Is(err, model.ErrOrganizationMemberSuspended) {
				common.ApiErrorStatus(c, http.StatusForbidden, err)
			} else {
				common.ApiError(c, err)
			}
			c.Abort()
			return
		}
		c.Next()
	}
}
