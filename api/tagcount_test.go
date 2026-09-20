package api

import "testing"

func TestSimpleTagQuery(t *testing.T) {
	cases := map[string]string{
		"remielle_dan":                   "remielle_dan",
		"  jane_doe_(zenless_zone_zero)": "jane_doe_(zenless_zone_zero)",
		"remielle_dan video":             "",
		"remielle_dan -ai_generated":     "",
		"-ai_generated":                  "",
		"touhou*":                        "",
		"":                               "",
	}
	for input, want := range cases {
		if got := simpleTagQuery(input); got != want {
			t.Errorf("simpleTagQuery(%q) = %q, want %q", input, got, want)
		}
	}
}

func TestParseTagCount(t *testing.T) {
	body := []byte(`<?xml version="1.0" encoding="UTF-8"?><tags type="array"><tag type="4" count="2789" name="remielle_dan" ambiguous="true" id="189965621"/></tags>`)
	count, ok := parseTagCount(body, "remielle_dan")
	if !ok || count != 2789 {
		t.Errorf("parseTagCount = %d, %v, want the site's 2789", count, ok)
	}
	if _, ok := parseTagCount(body, "someone_else"); ok {
		t.Error("a tag that is not in the response should not report a count")
	}
	if _, ok := parseTagCount([]byte("not xml"), "remielle_dan"); ok {
		t.Error("garbage should not report a count")
	}
	if _, ok := parseTagCount([]byte(`<?xml version="1.0"?><tags type="array"></tags>`), "remielle_dan"); ok {
		t.Error("an empty tag list should not report a count")
	}
}
