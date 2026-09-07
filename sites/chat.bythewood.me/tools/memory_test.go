package tools

import (
	"context"
	"strings"
	"testing"
)

type memStub struct {
	facts   []MemoryFact
	deleted []int64
	changed map[int64]string
}

func (m *memStub) Facts() ([]MemoryFact, error) { return m.facts, nil }
func (m *memStub) Add(t string) (int64, error) {
	id := int64(len(m.facts) + 1)
	m.facts = append(m.facts, MemoryFact{ID: id, Text: t})
	return id, nil
}
func (m *memStub) Replace(id int64, t string) error {
	if m.changed == nil {
		m.changed = map[int64]string{}
	}
	m.changed[id] = t
	return nil
}
func (m *memStub) Delete(id int64) error { m.deleted = append(m.deleted, id); return nil }

func withMem(m Memory) *Deps {
	d := NewDeps()
	d.Memory = m
	return d
}

func TestRememberWritesReadsAndForgets(t *testing.T) {
	m := &memStub{}
	d := withMem(m)
	ctx := context.Background()

	if _, err := Remember.Run(ctx, d, map[string]any{"action": "add", "fact": "Isaac wants to watch End of Watch."}); err != nil {
		t.Fatalf("add: %v", err)
	}
	if len(m.facts) != 1 || !strings.Contains(m.facts[0].Text, "End of Watch") {
		t.Fatalf("facts = %+v", m.facts)
	}

	got, err := Remember.Run(ctx, d, map[string]any{"action": "list"})
	if err != nil {
		t.Fatalf("list: %v", err)
	}
	if got.(map[string]any)["count"].(int) != 1 {
		t.Errorf("list did not report the one fact: %+v", got)
	}

	// The model renders an id as a number, and json puts it back as a float.
	if _, err := Remember.Run(ctx, d, map[string]any{"action": "replace", "id": float64(1), "fact": "Isaac watched it."}); err != nil {
		t.Fatalf("replace: %v", err)
	}
	if m.changed[1] != "Isaac watched it." {
		t.Errorf("replace wrote %q", m.changed[1])
	}

	if _, err := Remember.Run(ctx, d, map[string]any{"action": "forget", "id": "1"}); err != nil {
		t.Fatalf("forget: %v", err)
	}
	if len(m.deleted) != 1 || m.deleted[0] != 1 {
		t.Errorf("forgot %v", m.deleted)
	}
}

// A mode that writes nothing down cannot be the one that teaches it something
// to write down later, so an incognito turn may read memory and not change it.
func TestIncognitoCannotWriteMemory(t *testing.T) {
	m := &memStub{facts: []MemoryFact{{ID: 1, Text: "Isaac camps."}}}
	d := withMem(m)
	d.Incognito = true
	ctx := context.Background()

	for _, action := range []string{"add", "replace", "forget"} {
		if _, err := Remember.Run(ctx, d, map[string]any{"action": action, "fact": "x", "id": 1}); err == nil {
			t.Errorf("%s was allowed in an incognito turn", action)
		}
	}
	if len(m.facts) != 1 || len(m.deleted) != 0 {
		t.Errorf("an incognito turn changed memory: %+v %v", m.facts, m.deleted)
	}
	if _, err := Remember.Run(ctx, d, map[string]any{"action": "list"}); err != nil {
		t.Errorf("reading memory in incognito failed: %v", err)
	}
}

// Missing arguments have to say what is missing, since the model reads the
// error and tries again in the same turn.
func TestRememberRefusesIncompleteCalls(t *testing.T) {
	d := withMem(&memStub{})
	ctx := context.Background()
	for _, a := range []map[string]any{
		{"action": "add"},
		{"action": "replace", "id": 1},
		{"action": "forget"},
		{"action": "sing"},
	} {
		if _, err := Remember.Run(ctx, d, a); err == nil {
			t.Errorf("%v was accepted", a)
		}
	}
}

// A site with no store must not panic a turn that calls this.
func TestRememberWithNoStore(t *testing.T) {
	if _, err := Remember.Run(context.Background(), NewDeps(), map[string]any{"action": "list"}); err == nil {
		t.Error("a missing store was not reported")
	}
}
