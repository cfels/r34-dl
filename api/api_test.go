package api

import (
	"reflect"
	"testing"
)

func TestParseAutocomplete(t *testing.T) {
	payload := `[{"label":"cat_girl (59713)","value":"cat_girl"},` +
		`{"label":"cat_gloves (57)","value":"cat_gloves"},` +
		`{"label":"catkgf (21)","value":"catkgf"}]`
	want := []string{"cat_girl", "cat_gloves", "catkgf"}

	got, err := parseAutocomplete([]byte(payload))
	if err != nil {
		t.Fatalf("parseAutocomplete: %v", err)
	}
	if !reflect.DeepEqual(got, want) {
		t.Errorf("got %v, want %v", got, want)
	}
}

func TestParseAutocompleteFallsBackToLabel(t *testing.T) {
	got, err := parseAutocomplete([]byte(`[{"label":"cat_girl (42)"}]`))
	if err != nil {
		t.Fatalf("parseAutocomplete: %v", err)
	}
	if len(got) != 1 || got[0] != "cat_girl" {
		t.Errorf("got %v, want [cat_girl]", got)
	}
}

func TestParseAutocompleteEdgeCases(t *testing.T) {
	for _, payload := range []string{"", "null", "[]"} {
		got, err := parseAutocomplete([]byte(payload))
		if err != nil {
			t.Errorf("parseAutocomplete(%q): %v", payload, err)
		}
		if len(got) != 0 {
			t.Errorf("parseAutocomplete(%q) = %v, want empty", payload, got)
		}
	}

	got, err := parseAutocomplete([]byte(`["cat_girl","cat_gloves"]`))
	if err != nil {
		t.Fatalf("plain array: %v", err)
	}
	if len(got) != 2 || got[1] != "cat_gloves" {
		t.Errorf("plain array gave %v", got)
	}

	if _, err := parseAutocomplete([]byte("<html>error</html>")); err == nil {
		t.Error("malformed payload should report an error")
	}
}
