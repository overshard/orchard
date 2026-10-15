package tools

import (
	"strings"
	"testing"
)

func offered(schemas []map[string]any) string {
	var names []string
	for _, s := range schemas {
		names = append(names, schemaName(s))
	}
	return strings.Join(names, " ")
}

func TestAnOfferNarrowsByTrait(t *testing.T) {
	all := Default().Schemas()
	if got := offered(OnlyAny(all, Notes|Lookup)); got != "remember web_search wikipedia" {
		t.Errorf("notes and lookups offered %q", got)
	}
	kept := WithoutAny(all, Private)
	if len(kept)+len(OnlyAny(all, Private)) != len(all) || len(kept) == len(all) {
		t.Errorf("dropping private tools left %d of %d", len(kept), len(all))
	}
	for _, s := range kept {
		if Of(schemaName(s)).Is(Private) {
			t.Errorf("%s is private and still offered", schemaName(s))
		}
	}
	if len(all) != len(Default().Schemas()) {
		t.Error("narrowing an offer changed the one it was given")
	}
}

func TestAnUnknownNameHasNoTraits(t *testing.T) {
	if tool := Of("no_such_tool"); tool.Traits != 0 || tool.Cites != nil {
		t.Errorf("an unknown name came back with %+v", tool)
	}
}
