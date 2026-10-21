// fork: O13 — ported from OpenWebUI controller/option_signing_key_test.go.

package controller

import (
	"go/ast"
	"go/parser"
	"go/token"
	"net/http"
	"net/http/httptest"
	"testing"

	"github.com/QuantumNous/new-api/common"
	"github.com/QuantumNous/new-api/model"
	"github.com/QuantumNous/new-api/service/oidc"
	"github.com/gin-gonic/gin"
	"github.com/stretchr/testify/require"
)

func TestOptionsAPI_HidesAndProtectsOIDCSigningKeys(t *testing.T) {
	setupOAuth2OptionTest(t)
	oidc.ResetKeyCacheForTest()
	t.Cleanup(oidc.ResetKeyCacheForTest)

	_, err := oidc.CurrentKey()
	require.NoError(t, err)
	_, err = oidc.RotateKey() // populates both current and previous

	require.NoError(t, err)

	rec := httptest.NewRecorder()
	c, _ := gin.CreateTestContext(rec)
	c.Request = httptest.NewRequest(http.MethodGet, "/api/option/", nil)
	GetOptions(c)
	body := rec.Body.String()
	require.Contains(t, body, `"success":true`)
	require.NotContains(t, body, oidc.OptionSigningKeyCurrent)
	require.NotContains(t, body, oidc.OptionSigningKeyPrevious)
	require.NotContains(t, body, "PRIVATE KEY")

	for _, name := range []string{oidc.OptionSigningKeyCurrent, oidc.OptionSigningKeyPrevious} {
		common.OptionMapRWMutex.RLock()
		before := common.OptionMap[name]
		common.OptionMapRWMutex.RUnlock()
		require.NotEmpty(t, before)

		require.Contains(t, putOAuth2Option(t, name, "tampered"), `"success":false`)

		common.OptionMapRWMutex.RLock()
		after := common.OptionMap[name]
		common.OptionMapRWMutex.RUnlock()
		require.Equal(t, before, after)
	}
}

// TestGetOptionsHidesOIDCSigningKeys checks the explicit name skip in
// GetOptions, independently of the generic "...Key" suffix filter: the skip
// must hold even if the signing key options are ever renamed.
func TestGetOptionsHidesOIDCSigningKeys(t *testing.T) {
	setupOAuth2OptionTest(t)
	oidc.ResetKeyCacheForTest()
	t.Cleanup(oidc.ResetKeyCacheForTest)
	_, err := oidc.RotateKey() // creates current, then demotes it to previous
	require.NoError(t, err)

	for _, name := range []string{oidc.OptionSigningKeyCurrent, oidc.OptionSigningKeyPrevious} {
		require.True(t, oidcHiddenOption(name), name)
	}
	require.False(t, oidcHiddenOption("oauth2.enabled"))
	require.False(t, oidcHiddenOption("OIDCSigning"))
	// The generic "...Key" suffix filter hides these names too, so the GET
	// below cannot tell whether the explicit skip is wired; check the call.
	require.True(t, funcCalls(t, "option.go", "GetOptions", "oidcHiddenOption"), "GetOptions must call oidcHiddenOption")

	rec := httptest.NewRecorder()
	c, _ := gin.CreateTestContext(rec)
	c.Request = httptest.NewRequest(http.MethodGet, "/api/option/", nil)
	GetOptions(c)
	var resp struct {
		Success bool `json:"success"`
		Data    []struct {
			Key   string `json:"key"`
			Value string `json:"value"`
		} `json:"data"`
	}
	require.NoError(t, common.Unmarshal(rec.Body.Bytes(), &resp))
	require.True(t, resp.Success)
	for _, o := range resp.Data {
		require.NotEqual(t, oidc.OptionSigningKeyCurrent, o.Key)
		require.NotEqual(t, oidc.OptionSigningKeyPrevious, o.Key)
		require.NotContains(t, o.Value, "PRIVATE KEY", o.Key)
	}
}

// TestUpdateOptionRejectsSigningKeyWrites checks that the options API can
// neither overwrite nor create nor clear a signing key, whatever the value type.
func TestUpdateOptionRejectsSigningKeyWrites(t *testing.T) {
	setupOAuth2OptionTest(t)
	oidc.ResetKeyCacheForTest()
	t.Cleanup(oidc.ResetKeyCacheForTest)

	// No key exists yet: a write must not create one.
	for _, name := range []string{oidc.OptionSigningKeyCurrent, oidc.OptionSigningKeyPrevious} {
		body := putOAuth2Option(t, name, `{"kid":"x","private_pem":"y"}`)
		require.Contains(t, body, `"success":false`, name)
		require.Contains(t, body, "Invalid parameters", name)
		var count int64
		require.NoError(t, model.DB.Model(&model.Option{}).Where("`key` = ?", name).Count(&count).Error)
		require.Zero(t, count, name)
	}

	cur, err := oidc.CurrentKey()
	require.NoError(t, err)
	_, err = oidc.RotateKey()
	require.NoError(t, err)

	for _, name := range []string{oidc.OptionSigningKeyCurrent, oidc.OptionSigningKeyPrevious} {
		common.OptionMapRWMutex.RLock()
		before := common.OptionMap[name]
		common.OptionMapRWMutex.RUnlock()
		require.NotEmpty(t, before)
		for _, value := range []any{"", "tampered", 1, true} {
			require.Contains(t, putOAuth2Option(t, name, value), `"success":false`, "%s=%v", name, value)
		}
		common.OptionMapRWMutex.RLock()
		after := common.OptionMap[name]
		common.OptionMapRWMutex.RUnlock()
		require.Equal(t, before, after, name)
		var stored model.Option
		require.NoError(t, model.DB.Where("`key` = ?", name).First(&stored).Error)
		require.Equal(t, before, stored.Value, name)
	}

	// Case and padding variants: MySQL's default *_ci (PAD SPACE) collation
	// would match them to the real rows in model.UpdateOption's FirstOrCreate.
	before := map[string]string{}
	common.OptionMapRWMutex.RLock()
	for _, name := range []string{oidc.OptionSigningKeyCurrent, oidc.OptionSigningKeyPrevious} {
		before[name] = common.OptionMap[name]
	}
	common.OptionMapRWMutex.RUnlock()
	for _, variant := range []string{"oidcsigningcurrentkey", "OIDCSigningCurrentKey ", " oidcsigningpreviouskey", "OIDCSIGNINGPREVIOUSKEY"} {
		body := putOAuth2Option(t, variant, `{"kid":"x","private_pem":"y"}`)
		require.Contains(t, body, `"success":false`, "%q", variant)
		require.Contains(t, body, "Invalid parameters", "%q", variant)
		common.OptionMapRWMutex.RLock()
		_, created := common.OptionMap[variant]
		common.OptionMapRWMutex.RUnlock()
		require.False(t, created, "%q", variant)
		var count int64
		require.NoError(t, model.DB.Model(&model.Option{}).Where("`key` = ?", variant).Count(&count).Error)
		require.Zero(t, count, "%q", variant)
	}
	for name, value := range before {
		common.OptionMapRWMutex.RLock()
		after := common.OptionMap[name]
		common.OptionMapRWMutex.RUnlock()
		require.Equal(t, value, after, name)
		var stored model.Option
		require.NoError(t, model.DB.Where("`key` = ?", name).First(&stored).Error)
		require.Equal(t, value, stored.Value, name)
	}
	require.True(t, oidcHiddenOption(" oidcSigningCurrentKey\t"))
	require.False(t, oidcHiddenOption("OIDCSigningCurrentKeyX"))

	// The retired key is still the one published next to the new current key.
	keys, err := oidc.VerificationKeys()
	require.NoError(t, err)
	require.Len(t, keys, 2)
	require.Equal(t, cur.Kid, keys[1].Kid)
}

// funcCalls reports whether the top-level function fn in file (this package)
// contains a call to callee.
func funcCalls(t *testing.T, file, fn, callee string) bool {
	t.Helper()
	f, err := parser.ParseFile(token.NewFileSet(), file, nil, 0)
	require.NoError(t, err)
	found := false
	for _, decl := range f.Decls {
		fd, ok := decl.(*ast.FuncDecl)
		if !ok || fd.Recv != nil || fd.Name.Name != fn {
			continue
		}
		ast.Inspect(fd.Body, func(n ast.Node) bool {
			if call, ok := n.(*ast.CallExpr); ok {
				if id, ok := call.Fun.(*ast.Ident); ok && id.Name == callee {
					found = true
				}
			}
			return true
		})
	}
	return found
}
