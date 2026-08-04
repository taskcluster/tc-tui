package resource

import (
	"strings"
	"testing"
)

func TestParseActionInputNone(t *testing.T) {
	got, err := ParseActionInput(InputNone, "", true)
	if err != nil {
		t.Fatalf("InputNone should never error, got %v", err)
	}
	if got.Raw != "" || got.Value != nil {
		t.Fatalf("unexpected input: %+v", got)
	}
}

func TestParseActionInputLineRequiredEmpty(t *testing.T) {
	_, err := ParseActionInput(InputLine, "   ", true)
	if err == nil {
		t.Fatal("expected a required-value error for blank required input")
	}
}

func TestParseActionInputLineOptionalEmpty(t *testing.T) {
	got, err := ParseActionInput(InputLine, "", false)
	if err != nil {
		t.Fatalf("optional blank input should be allowed, got %v", err)
	}
	if got.Raw != "" {
		t.Fatalf("unexpected raw: %q", got.Raw)
	}
}

func TestParseActionInputLineKeepsRawExactly(t *testing.T) {
	got, err := ParseActionInput(InputLine, "  spaced value  ", true)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if got.Raw != "  spaced value  " {
		t.Fatalf("Raw should be untouched, got %q", got.Raw)
	}
	if got.Value != nil {
		t.Fatalf("InputLine should not populate Value, got %v", got.Value)
	}
}

func TestParseActionInputYAMLValid(t *testing.T) {
	got, err := ParseActionInput(InputYAML, "name: hello\ncount: 3\n", true)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	m, ok := got.Value.(map[string]interface{})
	if !ok {
		t.Fatalf("expected a parsed map, got %T", got.Value)
	}
	if m["name"] != "hello" {
		t.Fatalf("unexpected parsed value: %+v", m)
	}
}

func TestParseActionInputYAMLInvalid(t *testing.T) {
	_, err := ParseActionInput(InputYAML, "name: [unterminated", true)
	if err == nil || !strings.Contains(err.Error(), "invalid YAML") {
		t.Fatalf("expected an invalid YAML error, got %v", err)
	}
}

func TestParseActionInputJSONValid(t *testing.T) {
	got, err := ParseActionInput(InputJSON, `{"a":1}`, true)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	m, ok := got.Value.(map[string]interface{})
	if !ok || m["a"].(float64) != 1 {
		t.Fatalf("unexpected parsed value: %+v (%T)", got.Value, got.Value)
	}
}

func TestParseActionInputJSONInvalid(t *testing.T) {
	_, err := ParseActionInput(InputJSON, `{"a":}`, true)
	if err == nil || !strings.Contains(err.Error(), "invalid JSON") {
		t.Fatalf("expected an invalid JSON error, got %v", err)
	}
}

func TestParseActionInputOptionalYAMLBlankSkipsParse(t *testing.T) {
	got, err := ParseActionInput(InputYAML, "  \n ", false)
	if err != nil {
		t.Fatalf("blank optional YAML should not be parsed/rejected, got %v", err)
	}
	if got.Value != nil {
		t.Fatalf("blank input should leave Value nil, got %v", got.Value)
	}
}

func TestInputModeMultiline(t *testing.T) {
	multiline := map[InputMode]bool{
		InputNone:           false,
		InputLine:           false,
		InputText:           true,
		InputYAML:           true,
		InputJSON:           true,
		InputExternalEditor: false,
	}
	for mode, want := range multiline {
		if got := mode.Multiline(); got != want {
			t.Errorf("InputMode(%d).Multiline() = %v, want %v", mode, got, want)
		}
	}
}

func TestParseActionInputExternalEditorReturnsRaw(t *testing.T) {
	in, err := ParseActionInput(InputExternalEditor, "name: x\n", true)
	if err != nil {
		t.Fatalf("InputExternalEditor must parse: %v", err)
	}
	if in.Raw != "name: x\n" {
		t.Fatalf("Raw = %q, want the buffer verbatim", in.Raw)
	}
}
