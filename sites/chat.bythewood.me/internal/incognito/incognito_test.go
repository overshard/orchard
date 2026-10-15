package incognito

import (
	"context"
	"testing"
)

func TestOnlyAMarkedContextIsIncognito(t *testing.T) {
	ctx := context.Background()
	if On(ctx) {
		t.Error("an unmarked context reads as incognito")
	}
	marked := With(ctx)
	if !On(marked) {
		t.Error("a marked context does not read as incognito")
	}
	child, cancel := context.WithCancel(marked)
	defer cancel()
	if !On(child) {
		t.Error("the mark did not reach a context made from the marked one")
	}
}
