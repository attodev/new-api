package model

import (
	"go/ast"
	"go/parser"
	"go/token"
	"os"
	"path/filepath"
	"sort"
	"strings"
	"testing"

	"github.com/stretchr/testify/require"
)

// fork: O12 — 상주 캐시에서는 캐시를 맞추지 않는 DB 잔액 쓰기가 60초 뒤에도
// 저절로 맞춰지지 않는다. upstream 병합이 새 쓰기 경로를 들여오면 여기서 드러난다.
// 목록에 넣을 때 그 경로가 캐시에 차이를 더하는지(또는 해시를 지우는지) 확인하고
// 설계 문서 §5 표를 갱신한다. 값은 "상대경로:함수명".
// 검출: Update("quota")/UpdateColumn("quota"), Updates(map)의 "quota" 키, x["quota"] = ...
// 로 채운 맵을 넘기는 Updates(x), Model(&User{})에 식별자 맵을 넘기는 Updates(x),
// 이름에 user가 든 식별자의 Save/Updates. 목록에 있는데 검출되지 않는 항목도 실패한다.
var userQuotaWriteAllowlist = map[string]string{
	// users.quota — cache is kept in step
	"model/quota_reserve.go:persistUserQuotaDelta":          "cache decremented first (TryReserveUserQuota)",
	"model/quota_reserve.go:reserveUserQuotaDB":             "Redis-unavailable fallback; repair list",
	"model/user.go:increaseUserQuota":                       "IncreaseUserQuota calls cacheIncrUserQuota",
	"model/user.go:decreaseUserQuota":                       "DecreaseUserQuota calls cacheDecrUserQuota",
	"model/user.go:updateUserQuotaUsedQuotaAndRequestCount": "batch flush (upstream; unused after Task 6 hook)",
	"model/user.go:TransferAffQuotaToQuota":                 "commitAffTransfer syncs (Task 4)",
	"model/user.go:UpdateWithTx":                            "Updates(newUser) with quota in Omit",
	"model/batch_inflight_fork.go:flushUserQuotaRow":        "batch flush (fork)",
	"model/user_quota_adjustment.go:AdjustUserQuota":        "committed delta synced after commit",
	"model/topup.go:creditTopUpQuota":                       "every caller syncs: syncCreditTopUpCache / syncCreditUserQuotaCache / checkin cacheIncrUserQuota",
	"model/subscription.go:PurchaseSubscriptionWithBalance": "cacheDecrUserQuota after commit",
	// users.quota — DB write, then the cache hash is invalidated by the caller
	"model/organization.go:SetOrganizationMemberQuota":                     "service/org_user_export.go calls InvalidateUserCache after both call sites",
	"model/organization.go:IncreaseOrganizationMemberQuota":                "invalidates the hash itself (InvalidateUserCache)",
	"model/organization.go:clearOrganizationMembershipTx":                  "zeroes quota; callers invalidate: controller RemoveOrganizationUser, controller DeleteOrganization (owner), service ImportOrgUsersFromFile removeAbsent",
	"model/organization_member_status.go:updateOrganizationMemberFieldsTx": "pass-through map; only controller UpdateOrganizationUser sets quota and it invalidates; import/assign maps carry no quota",
	// users table, no quota key
	"model/account_security.go:ChangeUserPassword": "map holds password/username/display_name only",
	// other tables that happen to have a quota column
	"model/organization.go:IncreaseOrganizationQuota":   "organizations table",
	"model/organization.go:DecreaseOrganizationQuota":   "organizations table",
	"model/organization.go:TryReserveOrganizationQuota": "organizations table",
	"model/topup_target.go:creditOrganizationQuota":     "organizations table",
	"model/task.go:UpdateQuota":                         "tasks table",
	"model/usedata.go:increaseQuotaData":                "quota_data table",
}

func TestUserQuotaWriteSitesAreAllowlisted(t *testing.T) {
	root, err := filepath.Abs("..")
	require.NoError(t, err)
	var found []string
	for _, dir := range []string{"model", "service", "controller"} {
		entries, err := os.ReadDir(filepath.Join(root, dir))
		require.NoError(t, err)
		for _, e := range entries {
			name := e.Name()
			if e.IsDir() || !strings.HasSuffix(name, ".go") || strings.HasSuffix(name, "_test.go") {
				continue
			}
			found = append(found, quotaWriteSitesInFile(t, root, filepath.Join(dir, name))...)
		}
	}
	sort.Strings(found)
	detected := make(map[string]bool, len(found))
	var missing []string
	for _, site := range found {
		detected[site] = true
		if _, ok := userQuotaWriteAllowlist[site]; !ok {
			missing = append(missing, site)
		}
	}
	t.Logf("detected sites:\n%s", strings.Join(found, "\n"))
	require.Empty(t, missing, "new users.quota write sites: check cache sync and add to the allowlist (spec §5)")
	var stale []string
	for site := range userQuotaWriteAllowlist {
		if !detected[site] {
			stale = append(stale, site)
		}
	}
	sort.Strings(stale)
	require.Empty(t, stale, "allowlist entries the detector no longer finds: remove them or fix the detector (spec §5)")
}

func quotaWriteSitesInFile(t *testing.T, root, rel string) []string {
	fset := token.NewFileSet()
	file, err := parser.ParseFile(fset, filepath.Join(root, rel), nil, 0)
	require.NoError(t, err)
	var sites []string
	for _, decl := range file.Decls {
		fn, ok := decl.(*ast.FuncDecl)
		if !ok || fn.Body == nil {
			continue
		}
		quotaMaps := quotaMapNames(fn.Body)
		ast.Inspect(fn.Body, func(n ast.Node) bool {
			call, ok := n.(*ast.CallExpr)
			if !ok {
				return true
			}
			sel, ok := call.Fun.(*ast.SelectorExpr)
			if !ok || len(call.Args) == 0 {
				return true
			}
			if isQuotaWriteCall(sel, call.Args, quotaMaps) {
				sites = append(sites, rel+":"+fn.Name.Name)
				return false
			}
			return true
		})
	}
	return sites
}

func isQuotaWriteCall(sel *ast.SelectorExpr, args []ast.Expr, quotaMaps map[string]bool) bool {
	switch sel.Sel.Name {
	case "Update", "UpdateColumn":
		lit, ok := args[0].(*ast.BasicLit)
		return ok && lit.Value == `"quota"`
	case "Updates", "UpdateColumns":
		if comp, ok := args[0].(*ast.CompositeLit); ok {
			for _, elt := range comp.Elts {
				kv, ok := elt.(*ast.KeyValueExpr)
				if !ok {
					continue
				}
				if lit, ok := kv.Key.(*ast.BasicLit); ok && lit.Value == `"quota"` {
					return true
				}
			}
			return false
		}
		if id, ok := args[0].(*ast.Ident); ok && quotaMaps[id.Name] {
			return true
		}
		// A map built elsewhere (a parameter, say) written to the users table:
		// the keys are not visible here, so treat it as a possible quota write.
		if _, ok := args[0].(*ast.Ident); ok && chainModelsUser(sel.X) {
			return true
		}
		return isUserIdent(args[0])
	case "Save":
		return isUserIdent(args[0])
	}
	return false
}

func isUserIdent(expr ast.Expr) bool {
	if u, ok := expr.(*ast.UnaryExpr); ok {
		expr = u.X
	}
	id, ok := expr.(*ast.Ident)
	return ok && strings.Contains(strings.ToLower(id.Name), "user")
}

// quotaMapNames returns the identifiers that get a "quota" key assigned
// (fields["quota"] = ...) inside body, so a later Updates(fields) counts as a
// quota write. A bare params["quota"] that never reaches Updates (audit
// fields, for example) does not count.
func quotaMapNames(body *ast.BlockStmt) map[string]bool {
	names := map[string]bool{}
	ast.Inspect(body, func(n ast.Node) bool {
		assign, ok := n.(*ast.AssignStmt)
		if !ok {
			return true
		}
		for _, lhs := range assign.Lhs {
			idx, ok := lhs.(*ast.IndexExpr)
			if !ok {
				continue
			}
			lit, ok := idx.Index.(*ast.BasicLit)
			if !ok || lit.Value != `"quota"` {
				continue
			}
			if id, ok := idx.X.(*ast.Ident); ok {
				names[id.Name] = true
			}
		}
		return true
	})
	return names
}

// chainModelsUser reports whether the call chain receiver contains
// Model(&User{}) (or Model(User{})), i.e. the write targets the users table.
func chainModelsUser(expr ast.Expr) bool {
	for {
		call, ok := expr.(*ast.CallExpr)
		if !ok {
			return false
		}
		sel, ok := call.Fun.(*ast.SelectorExpr)
		if !ok {
			return false
		}
		if sel.Sel.Name == "Model" && len(call.Args) == 1 {
			arg := call.Args[0]
			if u, ok := arg.(*ast.UnaryExpr); ok {
				arg = u.X
			}
			if comp, ok := arg.(*ast.CompositeLit); ok {
				if id, ok := comp.Type.(*ast.Ident); ok && id.Name == "User" {
					return true
				}
			}
		}
		expr = sel.X
	}
}
