package main

import (
	"math"
	"strings"
)

// The all-in monthly number, because a list price is not what a house costs. The
// budget this is worked against is a single figure covering everything, so what a
// card shows has to be principal, interest, county specific tax, insurance, PMI,
// HOA, utilities and internet, not PITI with a shrug at the rest.
type Monthly struct {
	Price         int
	DownPayment   float64
	Loan          float64
	PrincipalInt  float64
	Tax           float64
	Insurance     float64
	PMI           float64
	HOA           float64
	Utilities     float64
	Internet      float64
	Total         float64
	TaxRateSource string // which county rate was used, or that a default was
	LoanToValue   float64
	PMIEndsAtLTV  float64 // 80, where PMI drops off, for the detail view
}

// amortize is the standard level payment formula. M = P * r(1+r)^n / ((1+r)^n - 1)
// with r the monthly rate and n the number of payments.
func amortize(principal, annualRatePct float64, years int) float64 {
	n := float64(years * 12)
	if n <= 0 || principal <= 0 {
		return 0
	}
	r := annualRatePct / 100 / 12
	if r == 0 {
		return principal / n
	}
	pow := math.Pow(1+r, n)
	return principal * r * pow / (pow - 1)
}

// rateFor is the combined county plus municipal rate per $100 of assessed value.
// Neighbouring counties differ enough to move a payment by $40 a month on the
// same price, so a single average rate would be wrong in both directions at once.
func (m Money) rateFor(county, city string) (float64, string) {
	key := strings.ToLower(strings.TrimSpace(strings.TrimSuffix(county, " County")))

	// A city rate is looked up first, as "county:city", since a house inside town
	// limits pays both.
	if city != "" {
		ck := key + ":" + strings.ToLower(strings.TrimSpace(city))
		if r, ok := m.CountyTaxPer100[ck]; ok {
			return r, county + " plus " + city
		}
	}
	if r, ok := m.CountyTaxPer100[key]; ok {
		return r, county + " county rate"
	}
	if r, ok := m.CountyTaxPer100["default"]; ok {
		return r, "default rate, no entry for " + orUnnamed(county, "this county")
	}
	return 0, "no tax rate configured"
}

// Estimate builds the monthly figure. taxAnnual is what the listing reported,
// which is used when present because it is the actual bill on the actual
// assessment, and the county rate against the list price is the fallback.
func (m Money) Estimate(price int, taxAnnual, hoaMonthly float64, county, city string, dpa bool) Monthly {
	out := Monthly{Price: price, HOA: hoaMonthly, Utilities: m.UtilitiesMonthly, Internet: m.InternetMonthly}

	down := float64(price) * m.DownPaymentPercent / 100
	if dpa {
		// The forgivable down payment assistance goes in as cash, which lowers
		// the loan and so both the payment and the PMI.
		down += m.DPAAmount
	}
	if down > float64(price) {
		down = float64(price)
	}
	out.DownPayment = down
	out.Loan = float64(price) - down
	out.LoanToValue = 0
	if price > 0 {
		out.LoanToValue = out.Loan / float64(price) * 100
	}
	out.PMIEndsAtLTV = 80

	out.PrincipalInt = amortize(out.Loan, m.RatePercent, m.TermYears)

	if taxAnnual > 0 {
		out.Tax = taxAnnual / 12
		out.TaxRateSource = "from the listing"
	} else {
		rate, src := m.rateFor(county, city)
		out.Tax = float64(price) * rate / 100 / 12
		out.TaxRateSource = src
	}

	out.Insurance = m.InsuranceAnnual / 12

	// PMI applies above 80% loan to value and is quoted as an annual percentage
	// of the loan, not of the price.
	if out.LoanToValue > 80 {
		out.PMI = out.Loan * m.PMIAnnualPercent / 100 / 12
	}

	out.Total = out.PrincipalInt + out.Tax + out.Insurance + out.PMI + out.HOA + out.Utilities + out.Internet
	return out
}
