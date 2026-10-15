package i18n

import (
	"go/ast"
	"go/parser"
	"go/token"
	"strconv"
	"testing"

	"github.com/stretchr/testify/require"
)

// forkDeclaredKeys lists the message keys declared in keys_fork.go, so
// TestLocalesRenderEveryKey (which parses keys.go) also checks that every fork
// key is in each locale and renders. Hook: one line in TestLocalesRenderEveryKey.
func forkDeclaredKeys(t *testing.T) []string {
	t.Helper()
	keysFile, err := parser.ParseFile(token.NewFileSet(), "keys_fork.go", nil, parser.SkipObjectResolution)
	require.NoError(t, err)
	var declared []string
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
	require.NotEmpty(t, declared)
	return declared
}
