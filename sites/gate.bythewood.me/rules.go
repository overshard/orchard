package main

import (
	"fmt"
	"net/netip"
	"regexp"
	"slices"
	"strconv"
	"strings"
	"time"
)

// Actions a rule can take. allow wins over everything, then block, limit and
// challenge in that order, so a request a block catches never gets a challenge.
const (
	actAllow     = "allow"
	actBlock     = "block"
	actLimit     = "limit"
	actChallenge = "challenge"
)

var actions = []string{actAllow, actBlock, actLimit, actChallenge}

func actionRank(a string) int { return slices.Index(actions, a) }

// Rule is one line of the firewall. A watching rule counts what it would have
// done and does nothing else, which is how every proposal starts.
type Rule struct {
	ID       int64
	Name     string
	Note     string
	When     string
	Action   string
	Status   int    // the code a block answers with, 403 or 404
	Message  string // the body of a block
	Rate     int    // requests a minute per address, for limit
	BanAfter int    // hits from one address inside banWindow that ban it everywhere, 0 for never
	BanFor   time.Duration
	Quiet    bool // keep its hits out of Caddy's access log
	Watching bool
	Pending  bool // proposed by an agent and not looked at yet
	Author   string
	Created  time.Time
	Expires  time.Time

	conds []cond
}

const banWindow = 10 * time.Minute

func (r Rule) Expired(now time.Time) bool { return !r.Expires.IsZero() && now.After(r.Expires) }

// Then is the action in words, the way the tables show it.
func (r Rule) Then() string {
	switch r.Action {
	case actBlock:
		return strconv.Itoa(r.Status)
	case actLimit:
		return fmt.Sprintf("%d/min", r.Rate)
	}
	return r.Action
}

// req is what gate knows about a request when it decides.
type req struct {
	IP      netip.Addr
	ASN     int
	Org     string
	Country string
	Host    string
	Method  string
	Path    string
	Query   string
	UA      string
	Crawler string
	Header  func(string) string
	Session bool // carried an orchard session cookie, live or not
}

type cond struct {
	line   int
	not    bool
	field  string
	header string
	op     string
	vals   []string
	re     *regexp.Regexp
	nets   []netip.Prefix
}

var fields = []string{"host", "path", "query", "method", "ip", "asn", "country", "ua", "crawler", "header"}
var ops = []string{"is", "in", "prefix", "contains", "suffix", "regex"}

// setsFlags is a regex that opens with its own flags, like (?i) or (?-i), which
// is left alone. Every other regex ignores case like the other tests do.
var setsFlags = regexp.MustCompile(`^\(\?[a-zA-Z-]+[:)]`)

// parseWhen reads the condition lines of a rule, one per line, every line has
// to hold. A line is `[not] field op values...`, with header written as
// header:Name, and a regex taking the rest of the line as it is.
func parseWhen(when string) ([]cond, error) {
	var out []cond
	for n, line := range strings.Split(when, "\n") {
		line = strings.TrimSpace(line)
		if line == "" || strings.HasPrefix(line, "#") {
			continue
		}
		c, err := parseCond(line)
		if err != nil {
			return nil, fmt.Errorf("line %d: %v", n+1, err)
		}
		c.line = n + 1
		out = append(out, c)
	}
	if len(out) == 0 {
		return nil, fmt.Errorf("a rule needs at least one condition")
	}
	return out, nil
}

func parseCond(line string) (cond, error) {
	var c cond
	word := func() string {
		line = strings.TrimSpace(line)
		i := strings.IndexAny(line, " \t")
		if i < 0 {
			w := line
			line = ""
			return w
		}
		w := line[:i]
		line = line[i+1:]
		return w
	}
	f := strings.ToLower(word())
	if f == "not" {
		c.not = true
		f = strings.ToLower(word())
	}
	if h, ok := strings.CutPrefix(f, "header:"); ok && h != "" {
		c.field, c.header = "header", h
	} else if slices.Contains(fields, f) && f != "header" {
		c.field = f
	} else {
		return c, fmt.Errorf("%q isn't a field, use one of host, path, query, method, ip, asn, country, ua, crawler or header:Name", f)
	}
	c.op = strings.ToLower(word())
	if !slices.Contains(ops, c.op) {
		return c, fmt.Errorf("%q isn't a test, use is, prefix, contains, suffix or regex", c.op)
	}
	if c.op == "in" {
		c.op = "is"
	}
	rest := strings.TrimSpace(line)
	if rest == "" {
		return c, fmt.Errorf("%s %s needs a value", c.field, c.op)
	}
	if c.op == "regex" {
		pat := rest
		if !setsFlags.MatchString(pat) {
			pat = "(?i)" + pat
		}
		re, err := regexp.Compile(pat)
		if err != nil {
			return c, fmt.Errorf("the regex doesn't compile: %v", err)
		}
		c.re = re
		c.vals = []string{rest}
		return c, nil
	}
	c.vals = strings.Fields(rest)
	switch c.field {
	case "ip":
		if c.op != "is" {
			return c, fmt.Errorf("ip only takes is, with addresses or ranges")
		}
		for _, v := range c.vals {
			p, err := netip.ParsePrefix(v)
			if err != nil {
				a, aerr := netip.ParseAddr(v)
				if aerr != nil {
					return c, fmt.Errorf("%q isn't an address or a range", v)
				}
				p = netip.PrefixFrom(a, a.BitLen())
			}
			c.nets = append(c.nets, p.Masked())
		}
	case "asn":
		if c.op != "is" {
			return c, fmt.Errorf("asn only takes is, with numbers")
		}
		for i, v := range c.vals {
			v = strings.TrimPrefix(strings.ToUpper(v), "AS")
			if _, err := strconv.Atoi(v); err != nil {
				return c, fmt.Errorf("%q isn't an AS number", c.vals[i])
			}
			c.vals[i] = v
		}
	case "country":
		for i, v := range c.vals {
			c.vals[i] = strings.ToUpper(v)
		}
	default:
		for i, v := range c.vals {
			c.vals[i] = strings.ToLower(v)
		}
	}
	return c, nil
}

// key is a condition line in a canonical form, for comparing two rules.
func (c *cond) key() string {
	return fmt.Sprintf("%t\x00%s\x00%s\x00%s\x00%s", c.not, c.field, c.header, c.op, strings.Join(c.vals, "\x00"))
}

// match reports whether every condition holds. A field gate doesn't know, like
// the country of an address no database has, fails its test either way, so an
// unknown never trips a `not` rule.
func (r *Rule) match(q *req) bool {
	for i := range r.conds {
		c := &r.conds[i]
		v, known := c.value(q)
		if !known {
			return false
		}
		if c.test(q, v) == c.not {
			return false
		}
	}
	return true
}

func (c *cond) value(q *req) (string, bool) {
	switch c.field {
	case "host":
		return strings.ToLower(q.Host), true
	case "path":
		return q.Path, true
	case "query":
		return q.Query, true
	case "method":
		return strings.ToLower(q.Method), true
	case "ip":
		return "", q.IP.IsValid()
	case "asn":
		return strconv.Itoa(q.ASN), q.ASN != 0
	case "country":
		return q.Country, q.Country != ""
	case "ua":
		return q.UA, true
	case "crawler":
		return q.Crawler, q.Crawler != ""
	case "header":
		if q.Header == nil {
			return "", false
		}
		return q.Header(c.header), true
	}
	return "", false
}

func (c *cond) test(q *req, v string) bool {
	if c.field == "ip" {
		for _, p := range c.nets {
			if p.Contains(q.IP.Unmap()) {
				return true
			}
		}
		return false
	}
	if c.op == "regex" {
		return c.re.MatchString(v)
	}
	if c.field != "asn" && c.field != "country" {
		v = strings.ToLower(v)
	}
	for _, want := range c.vals {
		switch c.op {
		case "is":
			if v == want {
				return true
			}
		case "prefix":
			if strings.HasPrefix(v, want) {
				return true
			}
		case "contains":
			if strings.Contains(v, want) {
				return true
			}
		case "suffix":
			if strings.HasSuffix(v, want) {
				return true
			}
		}
	}
	return false
}

// validate fills in the defaults and refuses a rule that can't run.
func (r *Rule) validate() error {
	r.Name = strings.TrimSpace(r.Name)
	if r.Name == "" || len(r.Name) > 60 || strings.ContainsAny(r.Name, " \t\n/") {
		return fmt.Errorf("a name is one word of up to 60 characters, like feed-hammer")
	}
	if !slices.Contains(actions, r.Action) {
		return fmt.Errorf("then has to be allow, block, limit or challenge")
	}
	conds, err := parseWhen(r.When)
	if err != nil {
		return err
	}
	r.conds = conds
	if r.Action == actBlock && r.Status != 403 && r.Status != 404 {
		r.Status = 403
	}
	if r.Action == actLimit && (r.Rate < 1 || r.Rate > 10000) {
		return fmt.Errorf("a limit needs a rate between 1 and 10000 a minute")
	}
	if r.BanAfter < 0 || r.BanAfter > 1000 {
		return fmt.Errorf("ban after has to be between 0 and 1000 hits")
	}
	if r.BanAfter > 0 && r.BanFor <= 0 {
		r.BanFor = 24 * time.Hour
	}
	if len(r.Message) > 300 {
		return fmt.Errorf("a message is at most 300 characters")
	}
	return nil
}

// Why is the line of a rule that decided a match and the piece of the request
// it matched, worked out only for events so a plain pass costs nothing.
type Why struct {
	Line    int
	Matched string
}

// why names the last line that matched something, since the lines before it
// usually only narrow the host and the last one is what caught it. A rule of
// only not lines names its first line.
func (r *Rule) why(q *req) Why {
	var out Why
	for i := range r.conds {
		c := &r.conds[i]
		if c.not {
			continue
		}
		v, _ := c.value(q)
		out = Why{Line: c.line, Matched: cut(c.piece(q, v), 120)}
	}
	if out.Line == 0 && len(r.conds) > 0 {
		out.Line = r.conds[0].line
	}
	return out
}

// piece is the part of a value a condition matched, empty when it didn't.
func (c *cond) piece(q *req, v string) string {
	if c.field == "ip" {
		for _, p := range c.nets {
			if p.Contains(q.IP.Unmap()) {
				return p.String()
			}
		}
		return ""
	}
	if c.op == "regex" {
		return c.re.FindString(v)
	}
	lv := v
	if c.field != "asn" && c.field != "country" {
		lv = strings.ToLower(v)
	}
	for _, want := range c.vals {
		switch {
		case c.op == "is" && lv == want, c.op == "prefix" && strings.HasPrefix(lv, want),
			c.op == "contains" && strings.Contains(lv, want), c.op == "suffix" && strings.HasSuffix(lv, want):
			return want
		}
	}
	return ""
}
