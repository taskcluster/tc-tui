package resource

import (
	"encoding/json"
	"fmt"
	"strings"
	"time"

	"github.com/taskcluster/tc-tui/taskcluster"
)

// secretMask is what one masked secret value renders as. Deliberately a
// fixed-width placeholder: it must leak neither the real value's length nor
// its type.
const secretMask = "••••••••"

type SecretsResource struct {
	tc taskcluster.Taskcluster
}

func NewSecretsResource(tc taskcluster.Taskcluster) *SecretsResource {
	return &SecretsResource{tc: tc}
}

func (r *SecretsResource) Name() string      { return "secrets" }
func (r *SecretsResource) Aliases() []string { return []string{"secret"} }
func (r *SecretsResource) Description() string {
	return "Secret names; a secret's values are masked until explicitly revealed"
}

func (r *SecretsResource) Columns() []Column {
	return []Column{{Title: "NAME", Expand: true}}
}

func (r *SecretsResource) List() ([]Row, error) {
	names, err := r.tc.GetSecrets()
	if err != nil {
		return nil, err
	}

	rows := make([]Row, 0, len(names))
	for _, name := range names {
		rows = append(rows, Row{ID: name, Cells: []string{name}})
	}

	return rows, nil
}

// Describe renders the secret with every value masked — opening a secret,
// or leaving one on screen, must not put its contents in front of whoever
// can see the terminal. See DescribeRevealed for the opt-in unmasked
// rendering.
func (r *SecretsResource) Describe(id string) (Detail, error) {
	return r.describe(id, false)
}

func (r *SecretsResource) DescribeRevealed(id string) (Detail, error) {
	return r.describe(id, true)
}

func (r *SecretsResource) describe(id string, revealed bool) (Detail, error) {
	secret, err := r.tc.GetSecret(id)
	if err != nil {
		return Detail{}, err
	}

	value := secret.Secret
	label := "[green]Secret:[white] [yellow](values hidden)[white]"
	if revealed {
		label = "[green]Secret:[white] [red](revealed)[white]"
	} else {
		value = maskSecretValues(value)
	}

	body := fmt.Sprintf(
		"[green]Expires:[white] %s\n\n%s\n%s\n",
		secret.Expires, label, renderYAML(value),
	)

	return Detail{Title: fmt.Sprintf("Secret :: %s", id), Body: body}, nil
}

// maskSecretValues replaces every scalar leaf of raw with secretMask,
// keeping object keys and the overall structure so a secret's SHAPE (which
// keys it holds) stays readable without any of its contents. Content that
// can't be parsed collapses to a single mask rather than being shown as-is:
// masking has to fail closed, since the alternative is printing the very
// value it exists to hide.
func maskSecretValues(raw json.RawMessage) json.RawMessage {
	// Nothing to mask, and nothing that could leak — left alone so renderYAML
	// still renders its "(none)" rather than a mask implying a value exists.
	if trimmed := strings.TrimSpace(string(raw)); trimmed == "" || trimmed == "null" {
		return raw
	}

	var value any
	if err := json.Unmarshal(raw, &value); err != nil {
		return json.RawMessage(`"` + secretMask + `"`)
	}

	masked, _ := json.Marshal(maskJSONValue(value)) // can't fail: only maps, slices and strings
	return masked
}

func maskJSONValue(value any) any {
	switch v := value.(type) {
	case map[string]any:
		masked := make(map[string]any, len(v))
		for key, item := range v {
			masked[key] = maskJSONValue(item)
		}
		return masked
	case []any:
		masked := make([]any, len(v))
		for i, item := range v {
			masked[i] = maskJSONValue(item)
		}
		return masked
	default:
		return secretMask
	}
}

func (r *SecretsResource) RefreshInterval() time.Duration { return 15 * time.Second }

func (r *SecretsResource) ListWebURL(rootURL, scope string) string {
	return webUIPath(rootURL, "secrets")
}

func (r *SecretsResource) DetailWebURL(rootURL, id string) string {
	return webUIPath(rootURL, "secrets/"+pathSegment(id))
}
