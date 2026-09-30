package tools

import "testing"

func TestAPointerSenseNamesItsEntry(t *testing.T) {
	luddite := []map[string]any{
		{"part_of_speech": "adjective", "definitions": []string{"Alternative letter-case form of Luddite."}},
		{"part_of_speech": "noun", "definitions": []string{"Alternative letter-case form of Luddite."}},
	}
	if got := formOf(luddite); got != "Luddite" {
		t.Errorf("formOf(luddite) = %q, want Luddite", got)
	}
	mixed := []map[string]any{
		{"definitions": []string{"Plural of mouse.", "A small rodent."}},
	}
	if got := formOf(mixed); got != "" {
		t.Errorf("a sense with its own meaning should not redirect, got %q", got)
	}
}

func TestADatedSenseLosesItsStylesheet(t *testing.T) {
	in := `A reference work.<style data-mw-deduplicate="x">.mw-parser-output .defdate{font-size:smaller}</style><span class="defdate">from 16th c.</span>`
	if got := plainText(in); got != "A reference work.from 16th c." {
		t.Errorf("plainText = %q", got)
	}
}
