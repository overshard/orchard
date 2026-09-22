package tools

import (
	"context"
	"strings"
	"testing"

	"chat.bythewood.me/property"
)

type propStub struct {
	rep    *property.Report
	err    error
	asked  string
	opt    property.Options
	quotes []property.Quote
	in     property.LoanInput
}

func (p *propStub) Lookup(_ context.Context, address string, opt property.Options) (*property.Report, error) {
	p.asked, p.opt = address, opt
	return p.rep, p.err
}

func (p *propStub) Spend(context.Context) (int, int) { return 5, 24 }

func (p *propStub) Quote(_ context.Context, in property.LoanInput) ([]property.Quote, property.Market, error) {
	p.in = in
	return p.quotes, property.Market{Thirty: 6.95, Week: "9/17/2026", Found: true}, nil
}

func withProperty(p Property) *Deps {
	d := NewDeps()
	d.Property = p
	return d
}

func TestPropertyToolPassesThePriceThrough(t *testing.T) {
	p := &propStub{rep: &property.Report{Address: "1 Test Rd", County: "Alexander", Complete: true}}
	got, err := PropertyTool.Run(context.Background(), withProperty(p), map[string]any{
		"address": "1 Test Rd, Taylorsville NC",
		"price":   float64(250000),
		"section": "summary",
	})
	if err != nil {
		t.Fatal(err)
	}
	if p.asked != "1 Test Rd, Taylorsville NC" {
		t.Fatalf("the address went in wrong: %q", p.asked)
	}
	if p.opt.Price != 250000 {
		t.Fatalf("the price has to reach the engine, got %d", p.opt.Price)
	}
	m, ok := got.(map[string]any)
	if !ok {
		t.Fatalf("want a map, got %T", got)
	}
	if m["other_sections"] == nil {
		t.Fatal("every answer names the other sections, so a follow up knows what to ask for")
	}
}

// A half built report has to tell the model to ask again rather than let it fill
// the gaps in itself.
func TestPropertyToolSaysWhenItIsStillRunning(t *testing.T) {
	p := &propStub{rep: &property.Report{
		Address: "1 Test Rd", Complete: false,
		Missing: []string{"the area work is still running"},
	}}
	got, err := PropertyTool.Run(context.Background(), withProperty(p), map[string]any{"address": "1 Test Rd"})
	if err != nil {
		t.Fatal(err)
	}
	note, _ := got.(map[string]any)["note"].(string)
	if !strings.Contains(note, "again") {
		t.Fatalf("want a note telling it to ask again, got %q", note)
	}
}

func TestPropertyToolNeedsAnAddress(t *testing.T) {
	if _, err := PropertyTool.Run(context.Background(), withProperty(&propStub{}), map[string]any{}); err == nil {
		t.Fatal("no address is an error")
	}
}

func TestPropertyToolWithoutAnEngineSaysSo(t *testing.T) {
	if _, err := PropertyTool.Run(context.Background(), NewDeps(), map[string]any{"address": "1 Test Rd"}); err == nil {
		t.Fatal("an unwired engine has to error rather than panic")
	}
	if _, err := Mortgage.Run(context.Background(), NewDeps(), map[string]any{"price": float64(250000)}); err == nil {
		t.Fatal("an unwired engine has to error rather than panic")
	}
}

func TestMortgageNeedsAPrice(t *testing.T) {
	if _, err := Mortgage.Run(context.Background(), withProperty(&propStub{}), map[string]any{}); err == nil {
		t.Fatal("no price is an error, since there is nothing to work out")
	}
}

func TestMortgageNarrowsToOneProgramme(t *testing.T) {
	p := &propStub{quotes: []property.Quote{
		{Type: "conventional", Name: "Conventional", Eligible: true, Total: 2000},
		{Type: "fha", Name: "FHA", Eligible: true, Total: 2100},
	}}
	got, err := Mortgage.Run(context.Background(), withProperty(p), map[string]any{
		"price":            float64(250000),
		"loan_type":        "fha",
		"down_payment_pct": float64(3.5),
		"county":           "Alexander",
	})
	if err != nil {
		t.Fatal(err)
	}
	if p.in.DownPct != 3.5 || p.in.County != "Alexander" {
		t.Fatalf("the arguments went in wrong: %+v", p.in)
	}
	loans, ok := got.(map[string]any)["loans"].([]map[string]any)
	if !ok {
		t.Fatalf("want the loan list, got %T", got.(map[string]any)["loans"])
	}
	if len(loans) != 1 || loans[0]["loan"] != "FHA" {
		t.Fatalf("want FHA alone, got %v", loans)
	}
}

// An unknown programme name falls back to all of them, since a narrowed list of
// nothing is a worse answer than the whole comparison.
func TestMortgageUnknownProgrammeKeepsThemAll(t *testing.T) {
	p := &propStub{quotes: []property.Quote{
		{Type: "conventional", Name: "Conventional", Eligible: true, Total: 2000},
		{Type: "fha", Name: "FHA", Eligible: true, Total: 2100},
	}}
	got, err := Mortgage.Run(context.Background(), withProperty(p), map[string]any{
		"price":     float64(250000),
		"loan_type": "jumbo",
	})
	if err != nil {
		t.Fatal(err)
	}
	if len(got.(map[string]any)["loans"].([]map[string]any)) != 2 {
		t.Fatal("want both back")
	}
}

func TestBothToolsAreRegistered(t *testing.T) {
	r := Default()
	for _, name := range []string{"property", "mortgage"} {
		if _, ok := r.Get(name); !ok {
			t.Fatalf("%s is not in the registry, so the model is never offered it", name)
		}
	}
}
