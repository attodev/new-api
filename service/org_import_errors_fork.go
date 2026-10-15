package service

import (
	"errors"
	"fmt"

	"github.com/QuantumNous/new-api/common"
)

// OrgImportRowError is one rejected row of an organization import, in a form
// the web console translates: MessageKey is the English template (also its key
// in web/src/i18n/locales) and Params fill its {{name}} placeholders. Line 0 is
// a failure of the remove-absent step, which belongs to no row.
type OrgImportRowError struct {
	Line       int            `json:"line"`
	Username   string         `json:"username,omitempty"`
	MessageKey string         `json:"message_key"`
	Params     map[string]any `json:"params,omitempty"`
}

// addRowError records a row error in both forms: the structured entry, and the
// English "line N (user): text" line that API callers and older web consoles
// read. The English text is rendered from the same template and params, so the
// two cannot drift. Params carry display-ready values (quote text cells with
// %q before passing them).
func (r *OrgImportResult) addRowError(line int, username, key string, params map[string]any) {
	r.ErrorDetails = append(r.ErrorDetails, OrgImportRowError{
		Line:       line,
		Username:   username,
		MessageKey: key,
		Params:     params,
	})
	r.Errors = append(r.Errors, orgImportRowPrefix(line, username)+common.NewMessage(key, params).Error())
}

// orgImportRowPrefix is the English "line N (user): " lead of a row error.
func orgImportRowPrefix(line int, username string) string {
	prefix := "remove-absent"
	if line > 0 {
		prefix = fmt.Sprintf("line %d", line)
	}
	if username != "" {
		prefix += " (" + username + ")"
	}
	return prefix + ": "
}

// rowErrorParams returns params extended with the nested error: its text as
// "error" and, when it is a common.Message, its key and params as "error_key"
// and "error_params" so the web console translates it too. params may be nil.
func rowErrorParams(err error, params map[string]any) map[string]any {
	out := make(map[string]any, len(params)+3)
	for name, value := range params {
		out[name] = value
	}
	out["error"] = err.Error()
	var message *common.Message
	// A wrapped Message is shown as text: its key alone would drop the wrapper.
	if errors.As(err, &message) && message.Error() == err.Error() {
		out["error_key"] = message.Key
		if len(message.Params) > 0 {
			out["error_params"] = message.Params
		}
	}
	return out
}
