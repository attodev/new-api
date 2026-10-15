package model

import (
	"go/ast"
	"go/parser"
	"go/token"
	"io/fs"
	"os"
	"path/filepath"
	"sort"
	"strings"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// fork: O12 — 상주 캐시에서는 캐시를 맞추지 않는 DB 잔액 쓰기가 60초 뒤에도
// 저절로 맞춰지지 않는다. upstream 병합이 새 쓰기 경로를 들여오면 여기서 드러난다.
// 목록에 넣을 때 그 경로가 캐시에 차이를 더하는지(또는 해시를 지우는지) 확인하고
// 설계 문서 §5 표를 갱신한다. 값은 "상대경로:함수명".
//
// 검출(함수 단위, 휴리스틱): Update/UpdateColumn의 첫 인자가 "quota"이거나 "quota"로
// 정의된 상수·변수 식별자, Updates/UpdateColumns의 맵·구조체 리터럴 키 "quota"/Quota,
// x["quota"] = ...로 채운 맵을 넘기는 Updates(x), Model(&User{})에 식별자 맵을 넘기는
// Updates(x), 이름에 user가 든 식별자의 Save/Updates, 문자열 리터럴에 update와 quota가
// 함께 든 Exec/Raw. model/·service/·controller/ 아래를 재귀로 훑는다.
// 목록에 있는데 검출되지 않는 항목도 실패한다.
//
// 남는 한계: 이름에 user가 없는 User 변수의 Save, Model(인자)가 리터럴이 아닌
// Model(param).Updates(map), 패키지 수준 `var f = func…` 본문, 함수 단위라서
// 허용된 함수 안에 더해지는 두 번째 쓰기, 다른 패키지(service·controller 밖)의 쓰기.
var userQuotaWriteAllowlist = map[string]string{
	// users.quota — cache is kept in step
	"model/quota_reserve.go:persistUserQuotaDelta":          "cache decremented first (TryReserveUserQuota)",
	"model/quota_reserve.go:reserveUserQuotaDB":             "DB fallback when Redis errors (repair list) or no hash exists",
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
	var files []quotaSourceFile
	for _, dir := range []string{"model", "service", "controller"} {
		base := filepath.Join(root, dir)
		require.NoError(t, filepath.WalkDir(base, func(path string, d fs.DirEntry, err error) error {
			if err != nil {
				return err
			}
			if d.IsDir() || !strings.HasSuffix(path, ".go") || strings.HasSuffix(path, "_test.go") {
				return nil
			}
			rel, err := filepath.Rel(root, path)
			if err != nil {
				return err
			}
			files = append(files, quotaSourceFile{abs: path, rel: filepath.ToSlash(rel)})
			return nil
		}))
	}
	found := scanQuotaWriteSites(t, files)
	detected := make(map[string]bool, len(found))
	var missing []string
	for _, site := range found {
		detected[site] = true
		if _, ok := userQuotaWriteAllowlist[site]; !ok {
			missing = append(missing, site)
		}
	}
	var stale []string
	for site := range userQuotaWriteAllowlist {
		if !detected[site] {
			stale = append(stale, site)
		}
	}
	sort.Strings(stale)
	t.Logf("detected sites:\n%s", strings.Join(found, "\n"))
	assert.Empty(t, missing, "new users.quota write sites: check cache sync and add to the allowlist (spec §5)")
	assert.Empty(t, stale, "allowlist entries the detector no longer finds: remove them or fix the detector (spec §5)")
	if t.Failed() {
		t.FailNow()
	}
}

// TestQuotaWriteDetectorPatterns pins what the detector does and does not catch.
func TestQuotaWriteDetectorPatterns(t *testing.T) {
	const src = `package probe

const quotaCol = "quota"

func mapKey(db DB)       { db.Model(&User{}).Updates(map[string]any{"quota": 1}) }
func mapAssign(db DB)    { m := map[string]any{}; m["quota"] = 1; db.Model(&User{}).Updates(m) }
func structKey(db DB)    { db.Model(&User{}).Updates(User{Quota: 5}) }
func structPtrKey(db DB) { db.Model(&User{}).Select("quota").Updates(&User{Quota: 5}) }
func updateCol(db DB)    { db.Model(&User{}).Update("quota", 1) }
func constColumn(db DB)  { db.Model(&User{}).Update(quotaCol, 1) }
func localColumn(db DB)  { c := "quota"; db.Model(&User{}).UpdateColumn(c, 1) }
func execSQL(db DB)      { db.Exec("UPDATE users SET quota = quota + ?", 1) }
func rawSQL(db DB)       { db.Raw("update users set quota = 0") }
func userVarSave(db DB)  { db.Save(user) }
func paramMapOnUser(db DB, fields map[string]any) {
	db.Model(&User{}).Where("id = 1").Updates(fields)
}
func orgTableMap(db DB) { db.Model(&Organization{}).Updates(map[string]any{"quota": 1}) }

func mapNoQuota(db DB)   { db.Model(&User{}).Updates(map[string]any{"status": 1}) }
func structNoQuota(db DB) { db.Model(&User{}).Updates(User{Status: 1}) }
func auditOnly(p map[string]any) { p["quota"] = 1 }
func otherColumn(db DB)  { db.Model(&User{}).Update("status", 1) }
func execNoQuota(db DB)  { db.Exec("UPDATE users SET status = 1") }
func selectQuota(db DB)  { db.Raw("SELECT quota FROM users") }
func orgParamMap(db DB, fields map[string]any) {
	db.Model(&Organization{}).Updates(fields)
}
func notAWrite(db DB) { db.Find(&users); log("quota") }
`
	dir := t.TempDir()
	path := filepath.Join(dir, "probe.go")
	require.NoError(t, os.WriteFile(path, []byte(src), 0o600))
	got := scanQuotaWriteSites(t, []quotaSourceFile{{abs: path, rel: "probe.go"}})

	want := []string{
		"probe.go:mapKey", "probe.go:mapAssign", "probe.go:structKey", "probe.go:structPtrKey",
		"probe.go:updateCol", "probe.go:constColumn", "probe.go:localColumn",
		"probe.go:execSQL", "probe.go:rawSQL", "probe.go:userVarSave",
		"probe.go:paramMapOnUser",
		// The table is not distinguished: the allowlist names organizations.quota writes.
		"probe.go:orgTableMap",
	}
	sort.Strings(want)
	assert.Equal(t, want, got)
}

type quotaSourceFile struct{ abs, rel string }

// scanQuotaWriteSites returns the deduplicated, sorted "rel:func" of every
// function that contains a quota write pattern.
func scanQuotaWriteSites(t *testing.T, files []quotaSourceFile) []string {
	fset := token.NewFileSet()
	parsed := make([]*ast.File, 0, len(files))
	for _, f := range files {
		file, err := parser.ParseFile(fset, f.abs, nil, 0)
		require.NoError(t, err)
		parsed = append(parsed, file)
	}
	columns := quotaColumnNames(parsed)
	seen := map[string]bool{}
	for i, file := range parsed {
		for _, decl := range file.Decls {
			fn, ok := decl.(*ast.FuncDecl)
			if !ok || fn.Body == nil {
				continue
			}
			quotaMaps := quotaMapNames(fn.Body)
			site := files[i].rel + ":" + fn.Name.Name
			ast.Inspect(fn.Body, func(n ast.Node) bool {
				call, ok := n.(*ast.CallExpr)
				if !ok {
					return true
				}
				sel, ok := call.Fun.(*ast.SelectorExpr)
				if !ok || len(call.Args) == 0 {
					return true
				}
				if isQuotaWriteCall(sel, call.Args, quotaMaps, columns) {
					seen[site] = true
					return false
				}
				return true
			})
		}
	}
	sites := make([]string, 0, len(seen))
	for site := range seen {
		sites = append(sites, site)
	}
	sort.Strings(sites)
	return sites
}

func isQuotaLit(expr ast.Expr) bool {
	lit, ok := expr.(*ast.BasicLit)
	return ok && lit.Kind == token.STRING && lit.Value == `"quota"`
}

// quotaColumnNames collects identifiers defined as the string "quota"
// (const/var specs and := / = assignments) in any scanned file, so
// Update(col, v) with col a named column counts. Names may collide across
// packages; that only over-detects.
func quotaColumnNames(files []*ast.File) map[string]bool {
	names := map[string]bool{}
	for _, file := range files {
		ast.Inspect(file, func(n ast.Node) bool {
			switch v := n.(type) {
			case *ast.ValueSpec:
				for i, name := range v.Names {
					if i < len(v.Values) && isQuotaLit(v.Values[i]) {
						names[name.Name] = true
					}
				}
			case *ast.AssignStmt:
				for i, lhs := range v.Lhs {
					if id, ok := lhs.(*ast.Ident); ok && i < len(v.Rhs) && isQuotaLit(v.Rhs[i]) {
						names[id.Name] = true
					}
				}
			}
			return true
		})
	}
	return names
}

// isQuotaColumnArg: the literal "quota", or an identifier that resolves to it
// (through the parser's object resolution, or the collected names).
func isQuotaColumnArg(expr ast.Expr, columns map[string]bool) bool {
	if isQuotaLit(expr) {
		return true
	}
	id, ok := expr.(*ast.Ident)
	if !ok {
		return false
	}
	if id.Obj != nil {
		switch decl := id.Obj.Decl.(type) {
		case *ast.ValueSpec:
			for i, name := range decl.Names {
				if name.Name == id.Name && i < len(decl.Values) && isQuotaLit(decl.Values[i]) {
					return true
				}
			}
		case *ast.AssignStmt:
			for i, lhs := range decl.Lhs {
				if l, ok := lhs.(*ast.Ident); ok && l.Name == id.Name && i < len(decl.Rhs) && isQuotaLit(decl.Rhs[i]) {
					return true
				}
			}
		}
	}
	return columns[id.Name]
}

// compositeHasQuotaKey: a map literal key "quota" or a struct literal field Quota.
func compositeHasQuotaKey(expr ast.Expr) bool {
	if u, ok := expr.(*ast.UnaryExpr); ok {
		expr = u.X
	}
	comp, ok := expr.(*ast.CompositeLit)
	if !ok {
		return false
	}
	for _, elt := range comp.Elts {
		kv, ok := elt.(*ast.KeyValueExpr)
		if !ok {
			continue
		}
		if isQuotaLit(kv.Key) {
			return true
		}
		if id, ok := kv.Key.(*ast.Ident); ok && id.Name == "Quota" {
			return true
		}
	}
	return false
}

// sqlMentionsQuotaUpdate: the first string literal in the arguments contains
// both "update" and "quota" (case-insensitive).
func sqlMentionsQuotaUpdate(args []ast.Expr) bool {
	for _, arg := range args {
		var text string
		found := false
		ast.Inspect(arg, func(n ast.Node) bool {
			if lit, ok := n.(*ast.BasicLit); ok && lit.Kind == token.STRING && !found {
				text, found = strings.ToLower(lit.Value), true
			}
			return !found
		})
		if found {
			return strings.Contains(text, "update") && strings.Contains(text, "quota")
		}
	}
	return false
}

func isQuotaWriteCall(sel *ast.SelectorExpr, args []ast.Expr, quotaMaps, columns map[string]bool) bool {
	switch sel.Sel.Name {
	case "Update", "UpdateColumn":
		return isQuotaColumnArg(args[0], columns)
	case "Updates", "UpdateColumns":
		if compositeHasQuotaKey(args[0]) {
			return true
		}
		if _, ok := args[0].(*ast.CompositeLit); ok {
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
		return compositeHasQuotaKey(args[0]) || isUserIdent(args[0])
	case "Exec", "Raw":
		return sqlMentionsQuotaUpdate(args)
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
