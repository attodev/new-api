package controller

import (
	"os"
	"path/filepath"
	"regexp"
	"slices"
	"strconv"
	"strings"
	"testing"

	"github.com/QuantumNous/new-api/common"
	"github.com/stretchr/testify/require"
)

// forkMessageSources are the fork files whose web console messages must be
// translatable, relative to the repository root. Later tasks extend this list.
var forkMessageSources = []string{
	"model/organization*.go",
	"controller/organization*.go",
	"service/*organization*.go",
	"model/topup_target.go",
	"controller/topup_target.go",
	"service/org_user_export.go",
	"controller/contact.go",
	"controller/email.go",
	"controller/topup_paypal.go",
	"setting/ratio_setting/model_discount.go",
}

// forkMessageConstants are message keys the fork files pass to NewMessage as
// constants, which the source scan below cannot read.
var forkMessageConstants = []string{
	OrganizationStatusAllowedError,
	OrganizationMemberStatusAllowedError,
	OrganizationDurationValueRangeError,
	OrganizationDurationUnitAllowedError,
}

// A message key is the first argument of NewMessage or the key of ApiErrorT /
// ApiSuccessT, written as one Go string literal.
var forkMessageKeyPattern = regexp.MustCompile(`(?:NewMessage\(|Api(?:Error|Success)T\(c, )("(?:[^"\\]|\\.)*")`)

// Every web console message of the fork files is a key of the web console's
// English locale, so the console can translate it (message_key + params).
func TestForkMessageKeysExistInWebLocale(t *testing.T) {
	data, err := os.ReadFile("../web/src/i18n/locales/en.json")
	require.NoError(t, err)
	var locale struct {
		Translation map[string]string `json:"translation"`
	}
	require.NoError(t, common.Unmarshal(data, &locale))

	var missing []string
	scanned := 0
	for _, key := range forkMessageConstants {
		if _, ok := locale.Translation[key]; !ok {
			missing = append(missing, key)
		}
	}
	for _, pattern := range forkMessageSources {
		files, err := filepath.Glob(filepath.Join("..", pattern))
		require.NoError(t, err)
		for _, file := range files {
			if strings.HasSuffix(file, "_test.go") {
				continue
			}
			source, err := os.ReadFile(file)
			require.NoError(t, err)
			for _, match := range forkMessageKeyPattern.FindAllSubmatch(source, -1) {
				key, err := strconv.Unquote(string(match[1]))
				require.NoError(t, err)
				scanned++
				if _, ok := locale.Translation[key]; !ok && !slices.Contains(missing, key) {
					missing = append(missing, key)
				}
			}
		}
	}
	require.NotZero(t, scanned)
	slices.Sort(missing)
	require.Empty(t, missing, "fork message keys missing from web/src/i18n/locales/en.json:\n%s", strings.Join(missing, "\n"))
}

// A validation error reaches the web console as message_key + message_params.
func TestOrganizationValidationErrorIsTranslatable(t *testing.T) {
	_, err := trimAndValidateText("name", " ", MaxOrganizationNameLength, true)
	var message *common.Message
	require.ErrorAs(t, err, &message)
	require.Equal(t, "{{field}} is required", message.Key)
	require.Equal(t, map[string]any{"field": "name"}, message.Params)
	require.EqualError(t, err, "name is required")
}
