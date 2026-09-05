package main

import (
	"context"
	"fmt"
	"net/http"
	"regexp"
	"sort"
	"strings"
	"sync"
	"time"
)

// A model writing code from search results invents package names, and it is the
// one mistake in a code answer that a reader cannot see. A syntax error shows
// up the moment they run it. `pip install flask-yfinance-cache` fails at a point
// where they have already decided the answer was good.
//
// So every package the answer imports is looked up in the registry that would
// have to have it, the same way an entity link is fetched and checked to be the
// thing it claims. Four registries, all keyless.

// Dependency is one package named by the code, and whether it exists.
type Dependency struct {
	Name string
	Eco  string
	URL  string
	// Found is only meaningful when Checked is true. A registry that did not
	// answer proves nothing, and saying so is better than guessing either way.
	Found   bool
	Checked bool
}

const (
	maxDepChecks = 14
	depTimeout   = 6 * time.Second
)

// verifyDeps looks up every package the blocks import. It is safe to call with
// no network: an unreachable registry leaves Checked false.
func verifyDeps(ctx context.Context, client *http.Client, blocks []CodeBlock) []Dependency {
	found := map[string]Dependency{}
	for _, b := range blocks {
		for _, d := range depsIn(b) {
			key := d.Eco + " " + d.Name
			if _, seen := found[key]; !seen {
				found[key] = d
			}
		}
	}
	if len(found) == 0 {
		return nil
	}

	list := make([]Dependency, 0, len(found))
	for _, d := range found {
		list = append(list, d)
	}
	sort.Slice(list, func(a, b int) bool {
		if list[a].Eco != list[b].Eco {
			return list[a].Eco < list[b].Eco
		}
		return list[a].Name < list[b].Name
	})
	if len(list) > maxDepChecks {
		list = list[:maxDepChecks]
	}

	ctx, cancel := context.WithTimeout(ctx, depTimeout*2)
	defer cancel()

	var wg sync.WaitGroup
	sema := make(chan struct{}, 4)
	for i := range list {
		wg.Add(1)
		go func(d *Dependency) {
			defer wg.Done()
			sema <- struct{}{}
			defer func() { <-sema }()
			d.Found, d.Checked = registryHas(ctx, client, *d)
		}(&list[i])
	}
	wg.Wait()
	return list
}

// depsIn reads the packages out of one block. Shell blocks are read too, since
// an install line names the package exactly and an import only names the module
// it happens to expose.
func depsIn(b CodeBlock) []Dependency {
	switch normalLang(b.Lang, b.File) {
	case "python":
		return pythonDeps(b.Code)
	case "javascript":
		return jsDeps(b.Code)
	case "go":
		return goDeps(b.Code)
	case "dockerfile":
		return dockerDeps(b.Code)
	}
	switch b.Lang {
	case "sh", "bash", "shell", "console", "zsh", "":
		return shellDeps(b.Code)
	}
	return nil
}

var (
	pyImport   = regexp.MustCompile(`(?m)^\s*(?:from\s+([A-Za-z_][\w.]*)|import\s+([A-Za-z_][\w.]*(?:\s*,\s*[A-Za-z_][\w.]*)*))`)
	jsImport   = regexp.MustCompile(`(?:from\s+|require\(\s*|import\(\s*)["']([^"'\n]+)["']`)
	goImport   = regexp.MustCompile(`(?m)^\s*(?:_\s+|\w+\s+)?"([a-z0-9.\-]+\.[a-z]{2,}/[^"\n]+)"`)
	dockerFrom = regexp.MustCompile(`(?im)^\s*FROM\s+(?:--platform=\S+\s+)?(\S+)`)
	pipInstall = regexp.MustCompile(`(?i)\bpip3?\s+install\s+([^\n|&;]+)`)
	npmInstall = regexp.MustCompile(`(?i)\bnpm\s+(?:install|i|add)\s+([^\n|&;]+)|\b(?:bun|yarn|pnpm)\s+add\s+([^\n|&;]+)`)
	dockerRun  = regexp.MustCompile(`(?im)\bdocker\s+(?:run|pull|create)\s+(.+)$`)
)

func pythonDeps(code string) []Dependency {
	var out []Dependency
	for _, m := range pyImport.FindAllStringSubmatch(code, -1) {
		names := []string{m[1]}
		if m[2] != "" {
			names = strings.Split(m[2], ",")
		}
		for _, n := range names {
			mod := strings.TrimSpace(n)
			if i := strings.Index(mod, "."); i > 0 {
				mod = mod[:i]
			}
			if mod == "" || pyStdlib[mod] {
				continue
			}
			out = append(out, Dependency{Name: pyDistribution(mod), Eco: "pypi"})
		}
	}
	return out
}

// pyDistribution maps a module name to the package that installs it, for the
// handful where they differ. Everything else is imported by its own name.
func pyDistribution(mod string) string {
	switch mod {
	case "bs4":
		return "beautifulsoup4"
	case "yaml":
		return "pyyaml"
	case "cv2":
		return "opencv-python"
	case "PIL":
		return "pillow"
	case "dateutil":
		return "python-dateutil"
	case "sklearn":
		return "scikit-learn"
	case "dotenv":
		return "python-dotenv"
	case "serial":
		return "pyserial"
	case "OpenSSL":
		return "pyopenssl"
	case "jwt":
		return "pyjwt"
	case "psycopg2":
		return "psycopg2-binary"
	case "flask_cors":
		return "flask-cors"
	case "flask_caching":
		return "flask-caching"
	}
	return strings.ReplaceAll(mod, "_", "-")
}

func jsDeps(code string) []Dependency {
	var out []Dependency
	for _, m := range jsImport.FindAllStringSubmatch(code, -1) {
		spec := m[1]
		if strings.HasPrefix(spec, ".") || strings.HasPrefix(spec, "/") || strings.HasPrefix(spec, "http") {
			continue
		}
		spec = strings.TrimPrefix(spec, "node:")
		parts := strings.Split(spec, "/")
		name := parts[0]
		if strings.HasPrefix(spec, "@") && len(parts) > 1 {
			name = parts[0] + "/" + parts[1]
		}
		if nodeBuiltin[name] {
			continue
		}
		out = append(out, Dependency{Name: name, Eco: "npm"})
	}
	return out
}

func goDeps(code string) []Dependency {
	var out []Dependency
	for _, m := range goImport.FindAllStringSubmatch(code, -1) {
		if mod := goModuleRoot(m[1]); mod != "" {
			out = append(out, Dependency{Name: mod, Eco: "go"})
		}
	}
	return out
}

// goModuleRoot trims an import path back to the module the proxy knows about,
// which for the three big forges is the first three elements.
func goModuleRoot(path string) string {
	parts := strings.Split(path, "/")
	switch parts[0] {
	case "github.com", "gitlab.com", "bitbucket.org", "codeberg.org":
		if len(parts) < 3 {
			return ""
		}
		return strings.Join(parts[:3], "/")
	}
	return path
}

func dockerDeps(code string) []Dependency {
	var out []Dependency
	for _, m := range dockerFrom.FindAllStringSubmatch(code, -1) {
		if img := dockerRepo(m[1]); img != "" {
			out = append(out, Dependency{Name: img, Eco: "docker"})
		}
	}
	return out
}

func shellDeps(code string) []Dependency {
	var out []Dependency
	for _, m := range pipInstall.FindAllStringSubmatch(code, -1) {
		for _, name := range installArgs(m[1]) {
			out = append(out, Dependency{Name: name, Eco: "pypi"})
		}
	}
	for _, m := range npmInstall.FindAllStringSubmatch(code, -1) {
		for _, name := range installArgs(m[1] + m[2]) {
			if !nodeBuiltin[name] {
				out = append(out, Dependency{Name: name, Eco: "npm"})
			}
		}
	}
	for _, m := range dockerRun.FindAllStringSubmatch(code, -1) {
		if img := dockerRepo(imageArg(m[1])); img != "" {
			out = append(out, Dependency{Name: img, Eco: "docker"})
		}
	}
	return out
}

// installArgs takes the package names off an install line, dropping flags and
// version pins.
func installArgs(s string) []string {
	var out []string
	for _, f := range strings.Fields(s) {
		if strings.HasPrefix(f, "-") {
			continue
		}
		f = strings.Trim(f, `"'`)
		// A pin, an extra or a path is not a name the registry answers to.
		if i := strings.IndexAny(f, "=<>[!~@"); i > 0 {
			f = f[:i]
		}
		if f == "" || strings.ContainsAny(f, "/.$") && !strings.HasPrefix(f, "@") {
			continue
		}
		out = append(out, strings.ToLower(f))
	}
	return out
}

// takesValue is the docker run flags whose value is a separate word, so the
// token after one of them is not the image.
var takesValue = map[string]bool{
	"-p": true, "-v": true, "-e": true, "-u": true, "-w": true, "-h": true,
	"--name": true, "--network": true, "--net": true, "--gpus": true,
	"--restart": true, "--entrypoint": true, "--add-host": true, "--env": true,
	"--volume": true, "--publish": true, "--user": true, "--workdir": true,
	"--label": true, "--mount": true, "--device": true, "--memory": true,
}

// imageArg is the first word of a docker run line that is not a flag or a
// flag's value. Matching the image with a regex means matching docker's whole
// flag grammar, and it gets the container name instead about half the time.
func imageArg(rest string) string {
	fields := strings.Fields(rest)
	for i := 0; i < len(fields); i++ {
		f := fields[i]
		if !strings.HasPrefix(f, "-") {
			return f
		}
		if takesValue[f] {
			i++
		}
	}
	return ""
}

// dockerRepo normalises an image reference to what Docker Hub's API wants, and
// drops anything hosted somewhere else since only Hub is being asked.
func dockerRepo(ref string) string {
	ref = strings.TrimSpace(ref)
	if ref == "" || ref == "scratch" || strings.HasPrefix(ref, "$") {
		return ""
	}
	// A build stage name, not an image.
	if !strings.ContainsAny(ref, ":/") && !knownBareImage[ref] {
		return ""
	}
	if i := strings.Index(ref, "@"); i > 0 {
		ref = ref[:i]
	}
	name := ref
	if i := strings.LastIndex(name, ":"); i > 0 && !strings.Contains(name[i:], "/") {
		name = name[:i]
	}
	parts := strings.Split(name, "/")
	switch len(parts) {
	case 1:
		return "library/" + parts[0]
	case 2:
		// A registry host in front means it is not on Hub.
		if strings.Contains(parts[0], ".") {
			return ""
		}
		return name
	}
	return ""
}

// knownBareImage is the small set of official images written with no tag, where
// a bare word really is an image rather than a build stage.
var knownBareImage = map[string]bool{
	"alpine": true, "debian": true, "ubuntu": true, "python": true, "node": true,
	"golang": true, "nginx": true, "redis": true, "postgres": true, "mysql": true,
	"busybox": true, "ollama/ollama": true,
}

func registryHas(ctx context.Context, client *http.Client, d Dependency) (found, checked bool) {
	var url string
	switch d.Eco {
	case "pypi":
		url = "https://pypi.org/pypi/" + d.Name + "/json"
	case "npm":
		url = "https://registry.npmjs.org/" + d.Name
	case "go":
		url = "https://proxy.golang.org/" + goProxyEscape(d.Name) + "/@v/list"
	case "docker":
		url = "https://hub.docker.com/v2/repositories/" + d.Name + "/"
	default:
		return false, false
	}

	ctx, cancel := context.WithTimeout(ctx, depTimeout)
	defer cancel()
	req, err := http.NewRequestWithContext(ctx, "GET", url, nil)
	if err != nil {
		return false, false
	}
	req.Header.Set("User-Agent", browserUA)
	resp, err := client.Do(req)
	if err != nil {
		return false, false
	}
	defer resp.Body.Close()
	switch {
	case resp.StatusCode == http.StatusOK:
		return true, true
	case resp.StatusCode == http.StatusNotFound:
		return false, true
	}
	// A 403 or a 429 says the registry is not answering questions right now,
	// which is not evidence the package is missing.
	return false, false
}

// goProxyEscape is the proxy's own casing rule: an upper case letter becomes
// !lower, since the protocol is case insensitive on disk.
func goProxyEscape(path string) string {
	var b strings.Builder
	for _, r := range path {
		if r >= 'A' && r <= 'Z' {
			b.WriteByte('!')
			b.WriteRune(r + 32)
			continue
		}
		b.WriteRune(r)
	}
	return b.String()
}

// depWarnings names anything the registry says does not exist. A package that
// could not be checked is left silent, since a warning nobody can act on is
// worse than none.
func depWarnings(deps []Dependency) []string {
	var missing []string
	for _, d := range deps {
		if d.Checked && !d.Found {
			missing = append(missing, fmt.Sprintf("%s (%s)", d.Name, ecoName(d.Eco)))
		}
	}
	if len(missing) == 0 {
		return nil
	}
	return []string{fmt.Sprintf(
		"the code uses %s, which %s not there when checked, so the name is probably wrong",
		strings.Join(missing, ", "),
		map[bool]string{true: "was", false: "were"}[len(missing) == 1])}
}

func ecoName(eco string) string {
	switch eco {
	case "pypi":
		return "PyPI"
	case "npm":
		return "npm"
	case "go":
		return "Go modules"
	case "docker":
		return "Docker Hub"
	}
	return eco
}

// nodeBuiltin is what node ships with, so an import of it is not a package.
var nodeBuiltin = map[string]bool{
	"assert": true, "buffer": true, "child_process": true, "cluster": true,
	"console": true, "crypto": true, "dgram": true, "dns": true, "events": true,
	"fs": true, "http": true, "http2": true, "https": true, "net": true,
	"os": true, "path": true, "process": true, "querystring": true,
	"readline": true, "stream": true, "string_decoder": true, "timers": true,
	"tls": true, "tty": true, "url": true, "util": true, "v8": true, "vm": true,
	"worker_threads": true, "zlib": true, "perf_hooks": true, "test": true,
}

// pyStdlib is the standard library as of 3.13, near enough. A name missing from
// here costs one lookup that comes back found, which is the harmless direction.
var pyStdlib = map[string]bool{
	"abc": true, "argparse": true, "array": true, "ast": true, "asyncio": true,
	"base64": true, "binascii": true, "bisect": true, "builtins": true, "bz2": true,
	"calendar": true, "cmath": true, "cmd": true, "collections": true, "colorsys": true,
	"concurrent": true, "configparser": true, "contextlib": true, "copy": true,
	"csv": true, "ctypes": true, "dataclasses": true, "datetime": true, "decimal": true,
	"difflib": true, "dis": true, "email": true, "enum": true, "errno": true,
	"faulthandler": true, "filecmp": true, "fileinput": true, "fnmatch": true,
	"fractions": true, "ftplib": true, "functools": true, "gc": true, "getopt": true,
	"getpass": true, "gettext": true, "glob": true, "gzip": true, "hashlib": true,
	"heapq": true, "hmac": true, "html": true, "http": true, "imaplib": true,
	"importlib": true, "inspect": true, "io": true, "ipaddress": true, "itertools": true,
	"json": true, "keyword": true, "linecache": true, "locale": true, "logging": true,
	"lzma": true, "mailbox": true, "math": true, "mimetypes": true, "mmap": true,
	"multiprocessing": true, "netrc": true, "numbers": true, "operator": true,
	"os": true, "pathlib": true, "pickle": true, "pkgutil": true, "platform": true,
	"plistlib": true, "poplib": true, "pprint": true, "profile": true, "pty": true,
	"queue": true, "quopri": true, "random": true, "re": true, "readline": true,
	"reprlib": true, "resource": true, "runpy": true, "sched": true, "secrets": true,
	"select": true, "selectors": true, "shelve": true, "shlex": true, "shutil": true,
	"signal": true, "site": true, "smtplib": true, "socket": true, "socketserver": true,
	"sqlite3": true, "ssl": true, "stat": true, "statistics": true, "string": true,
	"struct": true, "subprocess": true, "sys": true, "sysconfig": true, "tarfile": true,
	"tempfile": true, "textwrap": true, "threading": true, "time": true, "timeit": true,
	"tkinter": true, "token": true, "tokenize": true, "tomllib": true, "trace": true,
	"traceback": true, "tracemalloc": true, "types": true, "typing": true,
	"unicodedata": true, "unittest": true, "urllib": true, "uuid": true, "venv": true,
	"warnings": true, "wave": true, "weakref": true, "webbrowser": true, "wsgiref": true,
	"xml": true, "xmlrpc": true, "zipfile": true, "zipimport": true, "zoneinfo": true,
}
