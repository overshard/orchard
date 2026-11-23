package main

import (
	"bufio"
	"os"
	"slices"
	"strings"
	"testing"
	"time"
)

func TestCompareDebian(t *testing.T) {
	cases := []struct {
		a, b string
		want int
	}{
		{"6.12.117-1", "6.12.90-1", 1},
		{"6.12.90-1", "6.12.117-1", -1},
		{"1:10.0p1-7+deb13u4", "1:10.6p2-1", -1},
		{"1:9.2p1", "10.0p1", 1},
		{"1.0~rc1-1", "1.0-1", -1},
		{"1.0-1", "1.0-1", 0},
		{"1.0a", "1.0", 1},
		{"1.0", "1.0+deb13u1", -1},
		{"2.1.326", "2.1.251", 1},
		{"1.27.4", "1.27.10", -1},
	}
	for _, c := range cases {
		if got := compareDebian(c.a, c.b); got != c.want {
			t.Errorf("compareDebian(%q, %q) = %d, want %d", c.a, c.b, got, c.want)
		}
	}
}

func TestParseDpkgStatus(t *testing.T) {
	status := `Package: openssh-server
Status: install ok installed
Source: openssh
Version: 1:10.0p1-7+deb13u4

Package: libssl3t64
Status: install ok installed
Source: openssl (3.5.7-1~deb13u1)
Version: 3.5.7-1~deb13u1

Package: sudo
Status: deinstall ok config-files
Version: 1.9.16p2-3

Package: xz-utils
Status: install ok installed
Version: 5.8.1-1
`
	got, bins := parseDpkgStatus(bufio.NewScanner(strings.NewReader(status)))
	if bins["openssh-server"] != "1:10.0p1-7+deb13u4" || bins["sudo"] != "" {
		t.Errorf("binary packages = %v", bins)
	}
	want := map[string]string{"openssh": "1:10.0p1-7+deb13u4", "openssl": "3.5.7-1~deb13u1", "xz-utils": "5.8.1-1"}
	if len(got) != len(want) {
		t.Fatalf("got %v, want %v", got, want)
	}
	for k, v := range want {
		if got[k] != v {
			t.Errorf("%s = %q, want %q", k, got[k], v)
		}
	}
}

func TestKernelVersion(t *testing.T) {
	proc := "Linux version 6.12.117+deb13-amd64 (debian-kernel@lists.debian.org) (x86_64-linux-gnu-gcc-14 (Debian 14.2.0-19) 14.2.0, GNU ld (GNU Binutils for Debian) 2.44) #1 SMP PREEMPT_DYNAMIC Debian 6.12.117-1 (2026-11-12)\n"
	if got := kernelVersion(proc); got != "6.12.117-1" {
		t.Errorf("kernelVersion = %q", got)
	}
}

func readFixture(t *testing.T, name string) string {
	b, err := os.ReadFile("testdata/" + name)
	if err != nil {
		t.Fatal(err)
	}
	return string(b)
}

func TestParseTracker(t *testing.T) {
	ssh := parseTracker(readFixture(t, "tracker-openssh.html"), "openssh")
	if !ssh.known || ssh.fixed != "" || ssh.urgency != "unimportant" || ssh.notAffected {
		t.Errorf("openssh = %+v", ssh)
	}
	linux := parseTracker(readFixture(t, "tracker-linux.html"), "linux")
	if !linux.known || linux.fixed != "6.12.90-1" || linux.notAffected {
		t.Errorf("linux = %+v", linux)
	}
}

func TestKEVMatch(t *testing.T) {
	cases := []struct {
		vendor, product, want string
	}{
		{"Linux", "Kernel", "LINUX KERNEL"},
		{"OpenBSD", "OpenSSH", "OPENSSH"},
		{"Microsoft", "Windows", "WINDOWS"},
		{"Microsoft", "Ancillary Function Driver for WinSock", "WINDOWS"},
		{"Microsoft", "Office", ""},
		{"Microsoft", "SharePoint", ""},
		{"Google", "Chromium V8", ""},
		{"Google", "Pixel", "PIXEL"},
		{"Mozilla", "Firefox", "FIREFOX"},
	}
	for _, c := range cases {
		w, ok := kevMatch(c.vendor, c.product)
		if got := map[bool]string{true: w.label}[ok]; got != c.want {
			t.Errorf("kevMatch(%q, %q) = %q, want %q", c.vendor, c.product, got, c.want)
		}
	}
}

func TestParseMFSA(t *testing.T) {
	m := parseMFSA("announced: September 29, 2026\nimpact: critical\nfixed_in:\n- Firefox 157\ntitle: Security Vulnerabilities fixed in Firefox 157\n")
	if m.impact != "critical" || m.title != "Security Vulnerabilities fixed in Firefox 157" || m.announced.Format(time.DateOnly) != "2026-09-29" {
		t.Errorf("parseMFSA = %+v", m)
	}
}

func TestNextPatchTuesday(t *testing.T) {
	cases := map[string]string{
		"2026-11-18T15:00:00Z": "TUE DEC 8",
		"2026-12-08T15:00:00Z": "TUE DEC 8",
		"2026-12-01T15:00:00Z": "TUE DEC 8",
		"2026-12-09T15:00:00Z": "TUE JAN 12",
	}
	for in, want := range cases {
		now, _ := time.Parse(time.RFC3339, in)
		if got := nextPatchTuesday(now); got != want {
			t.Errorf("nextPatchTuesday(%s) = %q, want %q", in, got, want)
		}
	}
}

func TestFinishRowsBannerAndOrder(t *testing.T) {
	now := time.Date(2026, 11, 18, 15, 0, 0, 0, time.UTC)
	rows := finishRows([]Advisory{
		{ID: "A", Tag: "EXPLOITED", State: "patched", Added: "2026-11-17"},
		{ID: "B", Tag: "EXPLOITED", State: "check", Added: "2026-11-17"},
		{ID: "C", Tag: "EXPLOITED", State: "check", Added: "2026-11-10"},
		{ID: "D", Tag: "HIGH", State: "fixed", Added: "2026-11-18"},
		{ID: "B", Tag: "HIGH", State: "fixed", Added: "2026-11-17"},
	}, now)
	var ids []string
	for _, r := range rows {
		ids = append(ids, r.ID)
	}
	if strings.Join(ids, "") != "BCDA" {
		t.Errorf("order = %v", ids)
	}
	if !rows[0].Banner || rows[1].Banner || rows[2].Banner {
		t.Errorf("banner only belongs on a fresh exploited row that wants doing: %+v", rows)
	}
	if rows[1].Age != "8D AGO" {
		t.Errorf("age = %q", rows[1].Age)
	}
}

func TestGoRowCollapses(t *testing.T) {
	now := time.Date(2026, 11, 18, 0, 0, 0, 0, time.UTC)
	var v1, v2 osvVuln
	v1.ID, v1.Published = "GO-2026-1", "2026-11-05T00:00:00Z"
	v2.ID, v2.Published = "GO-2026-2", "2026-11-12T00:00:00Z"
	r, ok := goRow([]osvVuln{v1, v2}, "1.27.4", now)
	if !ok || r.State != "open" || !strings.HasPrefix(r.Title, "2 standard library") || r.Added != "2026-11-12" {
		t.Errorf("goRow = %+v", r)
	}
	if _, ok := goRow(nil, "1.27.4", now); ok {
		t.Error("no advisories should be no row")
	}
}

func TestAdvisoriesSurviveARestart(t *testing.T) {
	path := t.TempDir() + "/advisories.json"
	now := time.Date(2026, 11, 18, 15, 0, 0, 0, time.UTC)
	saveAdvisories(path, Advisories{At: now, Checked: "10:00", Rows: []Advisory{{ID: "A", Tag: "EXPLOITED", State: "check", Added: "2026-11-18"}}})
	a, ok := loadAdvisories(path, now.Add(time.Hour))
	if !ok || len(a.Rows) != 1 || !a.Rows[0].Banner || a.Checked != "10:00" {
		t.Errorf("loaded %+v", a)
	}
	saveAdvisories(path, Advisories{At: now, Rows: []Advisory{{ID: "B", State: "patched", Note: "HOST 6.12.117-1"}}})
	if _, ok := loadAdvisories(path, now); ok {
		t.Error("a public row with a host verdict should not load")
	}
	if _, ok := loadAdvisories(t.TempDir()+"/none.json", now); ok {
		t.Error("a missing file should load nothing")
	}
}

func TestKEVEffect(t *testing.T) {
	cases := map[string]string{
		"OpenBSD OpenSSH on Linux contains an unexpected status code or return value vulnerability that could allow for authentication bypass.":                                                                 ", allows authentication bypass",
		"Linux Kernel contains a missing initialization of a variable vulnerability that could allow for unintended modification of page-cache-backed data via failure to preserve the shared-fragment marker.": ", allows unintended modification of page-cache-backed data",
		"Apache Axis contains an unsafe reflection vulnerability that could allow an unauthenticated remote attacker to execute arbitrary code via a crafted SOAP fault message.":                               ", lets an attacker execute arbitrary code",
		"Microsoft Office Outlook contains a missing user interaction for potentially unsafe action vulnerability that allows an unauthorized attacker to perform spoofing over a network.":                     ", lets an attacker perform spoofing over a network",
		"Some product contains a vulnerability.": "",
	}
	for in, want := range cases {
		if got := kevEffect(in); got != want {
			t.Errorf("kevEffect(%q) = %q, want %q", in[:40], got, want)
		}
	}
}

func TestHandledAdvisoriesFoldByProduct(t *testing.T) {
	rows := []Advisory{
		{ID: "k", Product: "LINUX KERNEL", State: "check", Status: "CHECK"},
		{ID: "b1", Product: "BUN", Title: "LSQUIC", State: "fixed", Status: "FIXED", Note: "IN 1.4.5", Age: "TODAY"},
		{ID: "c", Product: "CADDY", Title: "Root escape", State: "fixed", Status: "FIXED", Note: "IN 2.11.8", Age: "16D AGO"},
		{ID: "b2", Product: "BUN", Title: "QPACK", State: "fixed", Status: "FIXED", Note: "IN 1.4.4", Age: "27D AGO"},
		{ID: "o", Product: "OPENSSH", Title: "Auth bypass", State: "clear", Status: "NOT AFFECTED", Note: "TRIXIE"},
		{ID: "b3", Product: "BUN", Title: "brotli", State: "patched", Status: "PATCHED", Note: "HOST 1.4.5"},
	}
	if todo := todoAdvisories(rows); len(todo) != 1 || todo[0].ID != "k" {
		t.Fatalf("todo = %+v", todo)
	}
	got := handledAdvisories(rows)
	if len(got) != 3 || got[0].Product != "BUN" || got[1].Product != "CADDY" || got[2].Product != "OPENSSH" {
		t.Fatalf("groups = %+v", got)
	}
	if b := got[0]; len(b.Rows) != 3 || b.Status != "3 HANDLED" || b.State != "fixed" || b.Summary != "NEWEST FIXED IN 1.4.5 / TODAY" {
		t.Errorf("bun = %+v", b)
	}
	if c := got[1]; c.Status != "FIXED" || c.Summary != "Root escape / IN 2.11.8 / 16D AGO" {
		t.Errorf("caddy = %+v", c)
	}
	if o := got[2]; o.State != "clear" || o.Status != "NOT AFFECTED" {
		t.Errorf("openssh = %+v", o)
	}
}

func TestSecurityVerdict(t *testing.T) {
	quiet := []Incident{{Name: "CLOUDFLARE", State: "elsewhere", Text: "2 OPEN, NOT TUNNEL OR DNS"}}
	if v := securityVerdict(Advisories{}, quiet); v.State != "wait" {
		t.Errorf("before a check = %+v", v)
	}
	clear := Advisories{Checked: "08:24", Rows: []Advisory{{Product: "BUN", State: "fixed"}}}
	if v := securityVerdict(clear, quiet); v.State != "ok" || v.Head != "NOTHING TO DO" || v.When != "CHECKED 08:24" {
		t.Errorf("clear = %+v", v)
	}
	todo := Advisories{Checked: "08:24", Rows: []Advisory{
		{Product: "LINUX KERNEL", Tag: "EXPLOITED", State: "check", Status: "CHECK", Note: "DEBIAN UPDATE"},
		{Product: "A", State: "open", Status: "UPDATE"},
		{Product: "B", State: "check", Status: "CHECK"},
	}}
	hit := append(quiet, Incident{Name: "GITHUB", State: "hit", Text: "GIT OPERATIONS DEGRADED"})
	v := securityVerdict(todo, hit)
	if v.State != "down" || v.Head != "1 OUTAGE / 3 TO DO" {
		t.Errorf("busy = %+v", v)
	}
	want := []string{"GITHUB: GIT OPERATIONS DEGRADED", "LINUX KERNEL, EXPLOITED, DEBIAN UPDATE", "A, UPDATE", "AND 1 MORE"}
	if !slices.Equal(v.Say, want) {
		t.Errorf("say = %q", v.Say)
	}
}

func TestSemverVerdict(t *testing.T) {
	cases := []struct {
		have, fixed, state string
		ok                 bool
	}{
		{"1.37.5", "1.37.4", "patched", true},
		{"1.37.4", ">= 1.37.4", "patched", true},
		{"1.37.3", "1.37.4", "open", true},
		{"1.38.0", "1.37.10", "patched", true},
		{"", "1.37.4", "", false},
		{"1.37.5", "", "", false},
	}
	for _, c := range cases {
		v, ok := semverVerdict(c.have, c.fixed)
		if ok != c.ok || v.State != c.state {
			t.Errorf("%q against %q: %v %q, want %v %q", c.have, c.fixed, ok, v.State, c.ok, c.state)
		}
	}
}
