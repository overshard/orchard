package property

import (
	"fmt"
	"math"
	"strings"
)

// Each programme charges insurance and fees differently, so comparing note rates
// alone gets the ranking wrong most weeks and every figure here is all in.

// Every figure here is published and moves, so the year is in the name or the
// comment.
const (
	// FHFA's 2026 baseline for one unit. No county in this part of the state is
	// high cost.
	conformingLimit2026 = 832750.0

	// Of the conforming limit. Every county in the search area sits at the floor.
	fhaFloorPct = 0.65

	// HUD's annual MIP schedule steps here, the 2023 conforming limit, unmoved
	// since ML 2023-05.
	fhaMIPStep = 726200.0

	fhaUpfrontPct = 1.75

	// USDA Section 502 guaranteed, the annual fee on the scheduled balance.
	usdaUpfrontPct = 1.00
	usdaAnnualPct  = 0.35

	// Conventional MI comes off at 80% on request and 78% automatically, where
	// FHA's runs eleven years or the life of the loan and USDA's runs for life.
	pmiDropLTV = 80.0
)

var LoanTypes = []string{"conventional", "fha", "va", "usda"}

var loanNames = map[string]string{
	"conventional": "Conventional",
	"fha":          "FHA",
	"va":           "VA",
	"usda":         "USDA Rural Development",
}

type Quote struct {
	Type string `json:"type"`
	Name string `json:"name"`

	// An ineligible quote still comes back with its reasons, since "USDA would be
	// cheapest and this address isn't eligible" is a useful answer.
	Eligible bool     `json:"eligible"`
	Blockers []string `json:"blockers,omitempty"`
	Checks   []string `json:"unverified,omitempty"`

	RatePct   float64 `json:"rate_pct"`
	TermYears int     `json:"term_years"`

	Price       int     `json:"price"`
	DownPayment float64 `json:"down_payment"`
	DownPct     float64 `json:"down_pct"`
	BaseLoan    float64 `json:"base_loan"`
	UpfrontFee  float64 `json:"upfront_fee"`
	UpfrontWhat string  `json:"upfront_fee_is,omitempty"`
	Loan        float64 `json:"loan"`
	LTV         float64 `json:"ltv_pct"`

	PrincipalInt float64 `json:"principal_interest"`
	Tax          float64 `json:"tax"`
	TaxSource    string  `json:"tax_source"`
	Insurance    float64 `json:"insurance"`
	MI           float64 `json:"mortgage_insurance"`
	MINote       string  `json:"mortgage_insurance_note,omitempty"`
	HOA          float64 `json:"hoa"`
	Utilities    float64 `json:"utilities"`
	Internet     float64 `json:"internet"`
	Total        float64 `json:"total_monthly"`

	CashToClose float64 `json:"cash_to_close"`

	// Front is housing over income and back is housing plus every other monthly
	// debt, set only when the income is known.
	FrontDTI float64 `json:"front_dti_pct,omitempty"`
	BackDTI  float64 `json:"back_dti_pct,omitempty"`
	DTINote  string  `json:"dti_note,omitempty"`
}

type LoanInput struct {
	Price      int
	County     string
	City       string
	TaxAnnual  float64 // the actual bill, when a listing carried one
	HOAMonthly float64

	// Overrides. Zero means take the config's figure.
	DownPct     float64
	DownAmount  float64
	RatePct     float64
	TermYears   int
	CreditScore int

	// A skipped check mustn't read as a failed one.
	USDAArea        bool
	USDAAreaChecked bool

	// Down payment assistance goes in as cash, so it lowers the loan, the
	// payment and the insurance together.
	UseDPA bool
}

// amortize is the level payment formula, M = P * r(1+r)^n / ((1+r)^n - 1), with r
// the monthly rate and n the number of payments.
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

// conventionalPMI is the annual premium as a percent of the loan. There's none at
// 80% or under, which is why conventional wins once there's a deposit behind it.
func conventionalPMI(ltv float64, score int) float64 {
	if ltv <= pmiDropLTV {
		return 0
	}
	band := func(a, b, c, d float64) float64 {
		switch {
		case score >= 740:
			return a
		case score >= 700:
			return b
		case score >= 660:
			return c
		default:
			return d
		}
	}
	switch {
	case ltv > 95:
		return band(0.41, 0.66, 1.10, 1.50)
	case ltv > 90:
		return band(0.30, 0.49, 0.81, 1.17)
	case ltv > 85:
		return band(0.19, 0.32, 0.55, 0.87)
	default:
		return band(0.14, 0.21, 0.35, 0.56)
	}
}

// fhaMIP runs the life of the loan above 90% at origination, which is why FHA can
// be dearer than conventional at the same rate.
func fhaMIP(baseLoan, ltv float64) (float64, string) {
	big := baseLoan > fhaMIPStep
	var pct float64
	switch {
	case ltv > 95 && big:
		pct = 0.75
	case ltv > 95:
		pct = 0.55
	case big:
		pct = 0.70
	default:
		pct = 0.50
	}
	if ltv > 90 {
		return pct, "for the life of the loan, since the loan to value is over 90% at closing"
	}
	return pct, "for eleven years"
}

func vaFundingFee(downPct float64, firstUse, exempt bool) (float64, string) {
	if exempt {
		return 0, "waived on a service connected disability rating"
	}
	switch {
	case downPct >= 10 && firstUse:
		return 1.25, "first use, 10% or more down"
	case downPct >= 5 && firstUse:
		return 1.50, "first use, 5% or more down"
	case firstUse:
		return 2.15, "first use, under 5% down"
	case downPct >= 10:
		return 1.25, "subsequent use, 10% or more down"
	case downPct >= 5:
		return 1.50, "subsequent use, 5% or more down"
	default:
		return 3.30, "subsequent use, under 5% down"
	}
}

// Conventional's 3% needs a first time buyer or an income under the area limit,
// and 5% is the plain figure.
func minDownPct(loan string, score int, firstTime bool) float64 {
	switch loan {
	case "fha":
		if score > 0 && score < 580 {
			return 10
		}
		return 3.5
	case "va", "usda":
		return 0
	default:
		if firstTime {
			return 3
		}
		return 5
	}
}

// VA and USDA have no ceiling, since VA caps the guarantee and USDA caps the
// borrower's income.
func loanCeiling(loan string) (float64, string) {
	switch loan {
	case "conventional":
		return conformingLimit2026, "FHFA conforming limit, 2026 baseline"
	case "fha":
		return math.Round(conformingLimit2026*fhaFloorPct/50) * 50, "FHA floor for 2026, which every county here sits at"
	default:
		return 0, ""
	}
}

// Quotes keeps a programme the house rules out but drops one the borrower rules
// out, since no report will change it. VA without entitlement priced cheapest and
// the model led with it before saying it was unavailable.
func (c Config) Quotes(in LoanInput, market Market) []Quote {
	out := make([]Quote, 0, len(LoanTypes))
	for _, t := range LoanTypes {
		if t == "va" && !c.Money.VAEligible {
			continue
		}
		out = append(out, c.Quote(t, in, market))
	}
	return out
}

func (c Config) Quote(loan string, in LoanInput, market Market) Quote {
	m := c.Money
	q := Quote{
		Type:      loan,
		Name:      loanNames[loan],
		Price:     in.Price,
		TermYears: firstNonZeroInt(in.TermYears, m.TermYears, 30),
		HOA:       in.HOAMonthly,
		Utilities: m.UtilitiesMonthly,
		Internet:  m.InternetMonthly,
		Eligible:  true,
	}
	score := firstNonZeroInt(in.CreditScore, m.CreditScore)
	price := float64(in.Price)

	// A report with neither a quoted rate nor the survey says so instead of
	// inventing a number.
	base := market.Thirty
	switch {
	case in.RatePct > 0:
		q.RatePct = in.RatePct
	case market.Found:
		q.RatePct = noteRate(base, loan, score)
	default:
		q.RatePct = 0
		q.Checks = append(q.Checks, "no rate available, so the payment could not be worked out")
	}

	// The programme's own floor goes last so a 0% USDA quote isn't silently
	// turned into 3%.
	floor := minDownPct(loan, score, m.FirstTimeBuyer)
	downPct := in.DownPct
	if in.DownAmount > 0 && price > 0 {
		downPct = in.DownAmount / price * 100
	}
	if downPct == 0 && in.DownPct == 0 && in.DownAmount == 0 {
		downPct = math.Max(floor, m.DownPaymentPercent)
		if loan == "va" || loan == "usda" {
			downPct = 0
		}
	}
	if downPct < floor {
		q.Checks = append(q.Checks,
			fmt.Sprintf("%.3g%% down is under the %.3g%% this programme takes, so it is quoted at the floor", downPct, floor))
		downPct = floor
	}

	down := price * downPct / 100
	if in.UseDPA && m.DPAAmount > 0 {
		down += m.DPAAmount
	}
	if down > price {
		down = price
	}
	q.DownPayment = round2(down)
	q.DownPct = round2(down / math.Max(price, 1) * 100)
	q.BaseLoan = round2(price - down)
	q.LTV = round2(q.BaseLoan / math.Max(price, 1) * 100)

	// Every government programme finances its upfront fee, so it lands on the
	// loan and not on the cash to close.
	switch loan {
	case "fha":
		q.UpfrontFee = round2(q.BaseLoan * fhaUpfrontPct / 100)
		q.UpfrontWhat = fmt.Sprintf("FHA upfront MIP, %.2f%% of the loan, financed", fhaUpfrontPct)
	case "va":
		pct, why := vaFundingFee(q.DownPct, m.VAFirstUse, m.VAExemptFundFee)
		q.UpfrontFee = round2(q.BaseLoan * pct / 100)
		if pct > 0 {
			q.UpfrontWhat = fmt.Sprintf("VA funding fee, %.2f%% of the loan, financed, %s", pct, why)
		} else {
			q.UpfrontWhat = "VA funding fee " + why
		}
	case "usda":
		q.UpfrontFee = round2(q.BaseLoan * usdaUpfrontPct / 100)
		q.UpfrontWhat = fmt.Sprintf("USDA upfront guarantee fee, %.2f%% of the loan, financed", usdaUpfrontPct)
	}
	q.Loan = round2(q.BaseLoan + q.UpfrontFee)

	q.PrincipalInt = round2(amortize(q.Loan, q.RatePct, q.TermYears))

	// On the base loan for conventional and FHA and on the financed balance for
	// USDA, which is how each programme writes it.
	switch loan {
	case "conventional":
		if pct := conventionalPMI(q.LTV, score); pct > 0 {
			q.MI = round2(q.BaseLoan * pct / 100 / 12)
			q.MINote = fmt.Sprintf("%.2f%% a year, off at %.0f%% loan to value on request and automatically at 78%%", pct, pmiDropLTV)
		} else {
			q.MINote = "none, the deposit is 20% or more"
		}
	case "fha":
		pct, how := fhaMIP(q.BaseLoan, q.LTV)
		q.MI = round2(q.BaseLoan * pct / 100 / 12)
		q.MINote = fmt.Sprintf("%.2f%% a year, %s", pct, how)
	case "va":
		q.MINote = "none, VA charges the funding fee instead"
	case "usda":
		q.MI = round2(q.Loan * usdaAnnualPct / 100 / 12)
		q.MINote = fmt.Sprintf("%.2f%% a year for the life of the loan", usdaAnnualPct)
	}

	// The listing's bill wins since it's the actual bill on the actual assessment.
	if in.TaxAnnual > 0 {
		q.Tax = round2(in.TaxAnnual / 12)
		q.TaxSource = "the bill on the listing"
	} else {
		rate, src := m.rateFor(in.County, in.City)
		q.Tax = round2(price * rate / 100 / 12)
		q.TaxSource = src
	}
	q.Insurance = round2(m.InsuranceAnnual / 12)

	q.Total = round2(q.PrincipalInt + q.Tax + q.Insurance + q.MI + q.HOA + q.Utilities + q.Internet)

	// Nobody publishes closing costs, so 3% is the band a settlement statement
	// lands in round here and the report names it a guess.
	q.CashToClose = round2(down + price*0.03)

	if m.AnnualIncome > 0 {
		monthlyIncome := m.AnnualIncome / 12
		housing := q.PrincipalInt + q.Tax + q.Insurance + q.MI + q.HOA
		q.FrontDTI = round1(housing / monthlyIncome * 100)
		q.BackDTI = round1((housing + m.MonthlyDebts) / monthlyIncome * 100)
		q.DTINote = dtiNote(loan, q.FrontDTI, q.BackDTI)
	}

	c.applyEligibility(&q, loan, in, score)
	return q
}

// dtiNote is a heads up and never a refusal, since an underwriter moves every one
// of these on compensating factors.
func dtiNote(loan string, front, back float64) string {
	switch loan {
	case "usda":
		if front > 29 || back > 41 {
			return fmt.Sprintf("USDA writes to 29/41 and this is %.0f/%.0f, which needs a waiver and usually a 680 score", front, back)
		}
		return "inside USDA's 29/41"
	case "fha":
		if back > 57 {
			return fmt.Sprintf("back end of %.0f%% is past what FHA's own engine will approve", back)
		}
		if back > 43 {
			return fmt.Sprintf("back end of %.0f%% is over 43%%, which FHA takes with compensating factors", back)
		}
		return "inside FHA's 31/43"
	case "va":
		if back > 41 {
			return fmt.Sprintf("back end of %.0f%% is over VA's 41%% guideline, which residual income can carry", back)
		}
		return "inside VA's 41%, and residual income is what actually decides"
	default:
		if back > 50 {
			return fmt.Sprintf("back end of %.0f%% is past the 50%% conventional ceiling", back)
		}
		if back > 45 {
			return fmt.Sprintf("back end of %.0f%% needs reserves or a strong score", back)
		}
		return "inside the conventional 45%"
	}
}

// A blocker is known to be wrong and a check is something nobody said, and the
// two mustn't read alike.
func (c Config) applyEligibility(q *Quote, loan string, in LoanInput, score int) {
	m := c.Money

	if ceiling, why := loanCeiling(loan); ceiling > 0 && q.BaseLoan > ceiling {
		q.Eligible = false
		q.Blockers = append(q.Blockers,
			fmt.Sprintf("the loan is over the %s of $%s", why, comma(ceiling)))
	}

	switch loan {
	case "fha":
		if score > 0 && score < 500 {
			q.Eligible = false
			q.Blockers = append(q.Blockers, "FHA needs a 500 score at the very least")
		}
		q.Checks = append(q.Checks, "FHA appraises to its own minimum property standards, which an older house can fail on peeling paint, a bad roof or a well too close to a septic field")

	case "va":
		if !m.VAEligible {
			q.Eligible = false
			q.Blockers = append(q.Blockers, "no VA entitlement on file in the config, so this is here for comparison only")
		}

	case "usda":
		switch {
		case !in.USDAAreaChecked:
			q.Checks = append(q.Checks, "the rural area map was not checked for this address")
		case !in.USDAArea:
			q.Eligible = false
			q.Blockers = append(q.Blockers, "the address is inside a USDA ineligible area, so this programme is out whatever the income is")
		}
		if m.AnnualIncome > 0 {
			limit := c.usdaIncomeLimit(in.County)
			switch {
			case limit <= 0:
				q.Checks = append(q.Checks, "no USDA income limit configured for this county, so the income test was not run")
			case m.AnnualIncome > limit:
				q.Eligible = false
				q.Blockers = append(q.Blockers,
					fmt.Sprintf("household income of $%s is over the $%s USDA limit for this county and household size", comma(m.AnnualIncome), comma(limit)))
			}
		} else {
			q.Checks = append(q.Checks, "no household income configured, so the USDA income limit was not checked")
		}
		q.Checks = append(q.Checks, "USDA counts every adult's income in the household, not just whoever is on the loan")

	case "conventional":
		if q.DownPct < 5 && !m.FirstTimeBuyer {
			q.Checks = append(q.Checks, "3% down needs a first time buyer or an income under the area limit, otherwise the floor is 5%")
		}
	}
}

// usdaIncomeLimit is 115% of area median income, from config since USDA
// publishes it as a PDF map that moves every year.
func (c Config) usdaIncomeLimit(county string) float64 {
	limits := c.Money.usdaLimits()
	if limits == nil {
		return 0
	}
	v, ok := limits[countyKey(county)]
	if !ok {
		v, ok = limits["default"]
		if !ok {
			return 0
		}
	}
	// USDA's figure covers one to four and rises 8% for five to eight.
	if c.Money.HouseholdSize > 4 {
		v *= 1.08
	}
	return math.Round(v)
}

func firstNonZeroInt(v ...int) int {
	for _, n := range v {
		if n != 0 {
			return n
		}
	}
	return 0
}

func comma(v float64) string {
	s := fmt.Sprintf("%.0f", math.Abs(v))
	var b strings.Builder
	if v < 0 {
		b.WriteByte('-')
	}
	for i, r := range s {
		if i > 0 && (len(s)-i)%3 == 0 {
			b.WriteByte(',')
		}
		b.WriteRune(r)
	}
	return b.String()
}
