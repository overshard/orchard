package main

import (
	"bufio"
	"os"
	"regexp"
	"runtime"
	"strings"
	"unicode"
)

// The host's dpkg database, bind mounted read only by compose. Containers
// share the host kernel, so /proc/version already describes the host.
const (
	hostDpkgStatus = "/host/dpkg/status"
	procVersion    = "/proc/version"
)

// hostVersions is what the VPS runs, as far as this container can see it.
type hostVersions struct {
	// Installed version per Debian source package, and per binary package.
	debian   map[string]string
	packages map[string]string

	// The running kernel's Debian version, which is what counts until a reboot.
	kernel string

	// The Go this binary was built with, which every site here shares.
	goVersion string
}

func readHostVersions() hostVersions {
	h := hostVersions{
		goVersion: strings.TrimPrefix(runtime.Version(), "go"),
	}
	if f, err := os.Open(hostDpkgStatus); err == nil {
		h.debian, h.packages = parseDpkgStatus(bufio.NewScanner(f))
		f.Close()
	}
	if b, err := os.ReadFile(procVersion); err == nil {
		h.kernel = kernelVersion(string(b))
	}
	return h
}

// version is the installed version of a source package and where it was read.
func (h hostVersions) version(pkg string) (string, string) {
	if pkg == "linux" {
		if h.kernel == "" {
			return "", ""
		}
		return h.kernel, "HOST"
	}
	if v, ok := h.debian[pkg]; ok {
		return v, "HOST"
	}
	return "", ""
}

// The compiler's version is in the same line, so the match wants the build date
// Debian stamps after the kernel's own version.
var debianKernel = regexp.MustCompile(`Debian (\d[^ ]*) \(\d{4}-\d{2}-\d{2}\)`)

func kernelVersion(proc string) string {
	if m := debianKernel.FindStringSubmatch(proc); m != nil {
		return m[1]
	}
	return ""
}

// parseDpkgStatus maps each installed source package and each installed
// binary package to its version. A binary package names its source when the two
// differ, with the source's own version in brackets when that differs too.
func parseDpkgStatus(sc *bufio.Scanner) (map[string]string, map[string]string) {
	sc.Buffer(make([]byte, 64<<10), 1<<20)
	out, bins := map[string]string{}, map[string]string{}
	var pkg, source, version, status string
	flush := func() {
		if pkg != "" && version != "" && strings.HasSuffix(status, " installed") {
			bins[pkg] = version
			src, ver := pkg, version
			if source != "" {
				f := strings.Fields(source)
				src = f[0]
				if len(f) > 1 {
					ver = strings.Trim(f[1], "()")
				}
			}
			if cur, ok := out[src]; !ok || compareDebian(ver, cur) < 0 {
				out[src] = ver
			}
		}
		pkg, source, version, status = "", "", "", ""
	}
	for sc.Scan() {
		line := sc.Text()
		if line == "" {
			flush()
			continue
		}
		k, v, ok := strings.Cut(line, ": ")
		if !ok {
			continue
		}
		switch k {
		case "Package":
			pkg = v
		case "Source":
			source = v
		case "Version":
			version = v
		case "Status":
			status = v
		}
	}
	flush()
	return out, bins
}

// HostRow is one line of what the VPS runs, under the incidents.
type HostRow struct {
	Name  string `json:"name"`
	Value string `json:"value"`
	State string `json:"state"`
}

// upstreamVersion drops the epoch and the Debian revision, since the panel
// names what the project calls the release.
func upstreamVersion(v string) string {
	_, up, _ := splitDebian(v)
	if i := strings.Index(up, "~"); i > 0 {
		up = up[:i]
	}
	return up
}

func hostRows(h hostVersions) []HostRow {
	var rows []HostRow
	if h.kernel != "" {
		row := HostRow{Name: "KERNEL", Value: h.kernel, State: "ok"}
		if newest := newestKernel(h.packages); newest != "" && compareDebian(newest, h.kernel) > 0 {
			row.Value, row.State = "REBOOT FOR "+newest, "warn"
		}
		rows = append(rows, row)
	}
	if v, ok := h.packages["base-files"]; ok {
		rows = append(rows, HostRow{Name: "DEBIAN", Value: strings.ToUpper(debianRelease) + " " + upstreamVersion(v), State: "ok"})
	}
	for _, p := range []struct{ name, pkg string }{
		{"OPENSSH", "openssh-server"},
		{"DOCKER", "docker-ce"},
		{"CONTAINERD", "containerd.io"},
	} {
		if v, ok := h.packages[p.pkg]; ok {
			rows = append(rows, HostRow{Name: p.name, Value: strings.ToUpper(upstreamVersion(v)), State: "ok"})
		}
	}
	if h.goVersion != "" {
		rows = append(rows, HostRow{Name: "GO", Value: h.goVersion, State: "ok"})
	}
	return rows
}

// newestKernel is the newest linux-image package installed, which unattended
// upgrades puts in place and only a reboot runs.
func newestKernel(packages map[string]string) string {
	newest := ""
	for name, v := range packages {
		if strings.HasPrefix(name, "linux-image-") && name != "linux-image-amd64" && name != "linux-image-cloud-amd64" {
			if newest == "" || compareDebian(v, newest) > 0 {
				newest = v
			}
		}
	}
	return newest
}

// compareDebian orders two Debian versions the way dpkg does, epoch, then
// upstream, then revision, with ~ sorting before anything including the end.
func compareDebian(a, b string) int {
	ea, ua, ra := splitDebian(a)
	eb, ub, rb := splitDebian(b)
	if c := compareNumeric(ea, eb); c != 0 {
		return c
	}
	if c := compareFragment(ua, ub); c != 0 {
		return c
	}
	return compareFragment(ra, rb)
}

func splitDebian(v string) (epoch, upstream, revision string) {
	epoch = "0"
	if e, rest, ok := strings.Cut(v, ":"); ok {
		epoch, v = e, rest
	}
	if i := strings.LastIndex(v, "-"); i >= 0 {
		return epoch, v[:i], v[i+1:]
	}
	return epoch, v, ""
}

func compareNumeric(a, b string) int {
	a, b = strings.TrimLeft(a, "0"), strings.TrimLeft(b, "0")
	if len(a) != len(b) {
		if len(a) < len(b) {
			return -1
		}
		return 1
	}
	return strings.Compare(a, b)
}

func charOrder(c byte) int {
	switch {
	case c == '~':
		return -1
	case unicode.IsLetter(rune(c)):
		return int(c)
	default:
		return int(c) + 256
	}
}

func compareFragment(a, b string) int {
	for a != "" || b != "" {
		for (a != "" && !isDigit(a[0])) || (b != "" && !isDigit(b[0])) {
			ca, cb := 0, 0
			if a != "" && !isDigit(a[0]) {
				ca = charOrder(a[0])
			}
			if b != "" && !isDigit(b[0]) {
				cb = charOrder(b[0])
			}
			if ca != cb {
				if ca < cb {
					return -1
				}
				return 1
			}
			if a != "" && !isDigit(a[0]) {
				a = a[1:]
			}
			if b != "" && !isDigit(b[0]) {
				b = b[1:]
			}
		}
		da, db := leadingDigits(a), leadingDigits(b)
		if c := compareNumeric(da, db); c != 0 {
			return c
		}
		a, b = a[len(da):], b[len(db):]
	}
	return 0
}

func isDigit(c byte) bool { return c >= '0' && c <= '9' }

func leadingDigits(s string) string {
	i := 0
	for i < len(s) && isDigit(s[i]) {
		i++
	}
	return s[:i]
}
