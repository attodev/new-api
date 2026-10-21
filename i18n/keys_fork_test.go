package i18n

import (
	"go/ast"
	"go/parser"
	"go/token"
	"strconv"
	"testing"

	"github.com/stretchr/testify/require"
)

// forkKeyFiles are the fork-only key files. TestLocalesRenderEveryKey parses
// keys.go, so each file here is also checked against every locale.
var forkKeyFiles = []string{"keys_fork.go", "keys_openwebui_fork.go"} // fork: O13

// forkDeclaredKeys lists the message keys declared in the fork key files, so
// TestLocalesRenderEveryKey (which parses keys.go) also checks that every fork
// key is in each locale and renders. Hook: one line in TestLocalesRenderEveryKey.
func forkDeclaredKeys(t *testing.T) []string {
	t.Helper()
	var declared []string
	for _, file := range forkKeyFiles {
		keysFile, err := parser.ParseFile(token.NewFileSet(), file, nil, parser.SkipObjectResolution)
		require.NoError(t, err)
		ast.Inspect(keysFile, func(node ast.Node) bool {
			if spec, ok := node.(*ast.ValueSpec); ok {
				for _, value := range spec.Values {
					if literal, ok := value.(*ast.BasicLit); ok {
						key, err := strconv.Unquote(literal.Value)
						require.NoError(t, err)
						declared = append(declared, key)
					}
				}
			}
			return true
		})
	}
	require.NotEmpty(t, declared)
	return declared
}
