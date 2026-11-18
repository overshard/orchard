package main

import (
	"bufio"
	"os"
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

func TestHostRows(t *testing.T) {
	h := hostVersions{
		kernel:    "6.12.117-1",
		goVersion: "1.27.4",
		packages: map[string]string{
			"base-files": "13.2", "openssh-server": "1:10.0p1-7+deb13u4",
			"docker-ce": "5:29.10.0-1~debian.13~trixie", "containerd.io": "2.4.2-4~debian.13~trixie",
			"linux-image-6.12.117+deb13-amd64": "6.12.117-1", "linux-image-6.12.86+deb13-amd64": "6.12.86-1",
		},
	}
	var got []string
	for _, r := range hostRows(h) {
		got = append(got, r.Name+" "+r.Value+" "+r.State)
	}
	want := "KERNEL 6.12.117-1 ok|DEBIAN TRIXIE 13.2 ok|OPENSSH 10.0P1 ok|DOCKER 29.10.0 ok|CONTAINERD 2.4.2 ok|GO 1.27.4 ok"
	if strings.Join(got, "|") != want {
		t.Errorf("host rows\n got %s\nwant %s", strings.Join(got, "|"), want)
	}
	h.packages["linux-image-6.12.120+deb13-amd64"] = "6.12.120-1"
	if r := hostRows(h)[0]; r.State != "warn" || r.Value != "REBOOT FOR 6.12.120-1" {
		t.Errorf("a newer kernel on disk should ask for a reboot, got %+v", r)
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
	if _, ok := loadAdvisories(t.TempDir()+"/none.json", now); ok {
		t.Error("a missing file should load nothing")
	}
}
