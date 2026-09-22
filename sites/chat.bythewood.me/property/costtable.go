package property

import (
	"fmt"
	"sort"
	"strings"
)

// The all-in monthly written as a table, because the number on its own does not
// say what is in it and a model asked to explain it in prose drops a line or
// invents one. Handed a shape, it reproduces the shape: the prompt already tells
// it to use one exactly when a tool result gives it one.
//
// One column per programme and one row per cost, so the all-in row is visibly
// the sum of the rows above it rather than a figure to take on trust.

// costRows is every line of the monthly payment, in the order a settlement
// statement would list them.
var costRows = []struct {
	label string
	of    func(Quote) float64
}{
	{"Principal and interest", func(q Quote) float64 { return q.PrincipalInt }},
	{"Property tax", func(q Quote) float64 { return q.Tax }},
	{"Home insurance", func(q Quote) float64 { return q.Insurance }},
	{"Mortgage insurance", func(q Quote) float64 { return q.MI }},
	{"HOA", func(q Quote) float64 { return q.HOA }},
	{"Utilities and internet", func(q Quote) float64 { return q.Utilities + q.Internet }},
}

// CostTable renders the comparison. Ineligible programmes are kept, marked, and
// sorted last, since "USDA would be cheapest and this address is not in an
// eligible area" is the useful answer and dropping the column loses it.
func (r *Report) CostTable() string {
	quotes := make([]Quote, 0, len(r.Quotes))
	for _, q := range r.Quotes {
		if q.Total > 0 {
			quotes = append(quotes, q)
		}
	}
	if len(quotes) == 0 {
		return ""
	}
	sort.SliceStable(quotes, func(i, j int) bool {
		if quotes[i].Eligible != quotes[j].Eligible {
			return quotes[i].Eligible
		}
		return quotes[i].Total < quotes[j].Total
	})

	head := []string{"Monthly cost"}
	for _, q := range quotes {
		name := q.Name
		if !q.Eligible {
			name += " (not available)"
		}
		head = append(head, name)
	}

	var b strings.Builder
	row := func(cells []string) {
		b.WriteString("| " + strings.Join(cells, " | ") + " |\n")
	}
	row(head)
	sep := make([]string, len(head))
	sep[0] = "---"
	for i := 1; i < len(sep); i++ {
		sep[i] = "---:"
	}
	row(sep)

	rate := []string{"Rate"}
	for _, q := range quotes {
		rate = append(rate, fmt.Sprintf("%.2f%%", q.RatePct))
	}
	row(rate)

	for _, line := range costRows {
		cells := []string{line.label}
		any := false
		for _, q := range quotes {
			v := line.of(q)
			if v > 0 {
				any = true
			}
			cells = append(cells, "$"+comma(v))
		}
		// A row that is nought for every programme is noise. HOA usually is.
		if any {
			row(cells)
		}
	}

	total := []string{"**All in per month**"}
	for _, q := range quotes {
		total = append(total, "**$"+comma(q.Total)+"**")
	}
	row(total)

	b.WriteString("\n")
	upfront := append([]string{"Up front"}, head[1:]...)
	row(upfront)
	row(sep)
	for _, line := range []struct {
		label string
		of    func(Quote) float64
	}{
		{"Down payment", func(q Quote) float64 { return q.DownPayment }},
		{"Cash to close, roughly", func(q Quote) float64 { return q.CashToClose }},
		{"Fee financed into the loan", func(q Quote) float64 { return q.UpfrontFee }},
	} {
		cells := []string{line.label}
		any := false
		for _, q := range quotes {
			v := line.of(q)
			if v > 0 {
				any = true
			}
			cells = append(cells, "$"+comma(v))
		}
		if any {
			row(cells)
		}
	}
	return b.String()
}
