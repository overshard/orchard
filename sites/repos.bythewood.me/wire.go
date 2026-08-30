package main

// The git smart HTTP wire: clone, fetch and push.
//
// This is the whole of it, and it is short because `net/http/cgi` already does
// the hard part. The Rust build carried 180 lines here: a hand written CGI
// header parser, an async_stream body pump, and a kill_on_drop fix for orphaned
// upload-pack processes. cgi.Handler parses Status: itself, streams with
// io.Copy, and kills the child when the request context is cancelled, which is
// the exact bug kill_on_drop existed to fix.
//
// Authentication is HTTP Basic, and that is not a security preference. Git
// authenticates over HTTP by answering a WWW-Authenticate challenge through its
// credential subsystem, and Basic is the only scheme that subsystem natively
// fills, stores and replays. What goes in the password field here is a random
// token, not a human password, which is the same thing a GitHub PAT is. The
// browser UI does not use any of this; it has session cookies. See auth.go.

import (
	"io"
	"log/slog"
	"net/http"
	"net/http/cgi"
	"os"
	"os/exec"
	"path/filepath"
	"strconv"
	"strings"
)

// gitHTTPBackend is the path to git's own CGI. Alpine ships it under libexec
// rather than on PATH, and it moves between distributions, so it is resolved
// once at startup rather than guessed per request.
func gitHTTPBackend() string {
	candidates := []string{
		"/usr/libexec/git-core/git-http-backend",
		"/usr/lib/git-core/git-http-backend",
	}
	for _, p := range candidates {
		if _, err := os.Stat(p); err == nil {
			return p
		}
	}
	// A PATH lookup as the last resort, so a distribution that puts it
	// somewhere else still works rather than failing at the first clone.
	if p, err := exec.LookPath("git-http-backend"); err == nil {
		return p
	}
	return ""
}

// wire serves everything under /{name}.git/.
type wire struct {
	store   *Store
	db      *DB
	backend string
	// allowCreate turns push-to-create on. Off would mean a repository has
	// to exist before it can be pushed to, which is a UI nobody needs when
	// there is exactly one person pushing.
	allowCreate bool
}

// Router puts the wire in front of the browse mux.
//
// It is a wrapper rather than a set of mux patterns because Go's ServeMux
// wildcards match a whole path segment: "{name}.git" is not a legal pattern, so
// there is no way to register the clone URL shape declaratively. Splitting the
// first segment here costs one strings.Cut per request and keeps the clone URL
// as /<name>.git/, which is what every git client and every muscle memory
// expects.
//
// The upside over the Rust build is unchanged: there is no registration-order
// footgun, because the wire either claims a path outright or does not.
func (wr *wire) Router(next http.Handler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		seg, rest, _ := strings.Cut(strings.TrimPrefix(r.URL.Path, "/"), "/")
		name, ok := repoNameFromPath(seg)
		if !ok {
			next.ServeHTTP(w, r)
			return
		}
		wr.serve(w, r, name, rest)
	})
}

// serve handles one git wire request for an already-parsed repository name.
func (wr *wire) serve(w http.ResponseWriter, r *http.Request, name, rest string) {
	// Dumb HTTP would serve loose objects and packed-refs as plain files,
	// which bypasses every check below. Nothing here speaks it.
	if isDumbPath(rest) {
		http.NotFound(w, r)
		return
	}

	// What is being asked for decides whether this needs a credential.
	// service=git-receive-pack on the info/refs probe is git saying "I am
	// about to push", and it is the request that must be challenged: if it
	// is answered anonymously, git never asks for a credential and the push
	// itself then fails with a 401 the user cannot act on.
	writing := rest == "git-receive-pack" ||
		r.URL.Query().Get("service") == "git-receive-pack"

	if writing {
		user, ok := wr.authenticate(r)
		if !ok {
			// The realm is what git shows when it prompts.
			w.Header().Set("WWW-Authenticate", `Basic realm="repos"`)
			http.Error(w, "authentication required", http.StatusUnauthorized)
			return
		}
		r = r.WithContext(withUser(r.Context(), user))
	}

	repo, ok := wr.store.Open(name)
	if !ok {
		// Push to create. Only ever reached after the authentication above,
		// because writing is the only branch that can get here without an
		// existing repository.
		if !writing || !wr.allowCreate {
			http.NotFound(w, r)
			return
		}
		created, err := wr.store.InitBare(r.Context(), name)
		if err != nil {
			slog.Error("push-to-create failed",
				slog.String("repo", name), slog.Any("err", err))
			http.Error(w, "could not create repository", http.StatusInternalServerError)
			return
		}
		slog.Info("repository created by push", slog.String("repo", name))
		if err := wr.db.EnsureRepo(name); err != nil {
			slog.Error("record new repo", slog.String("repo", name), slog.Any("err", err))
		}
		repo = created
	}

	// A mirror is a copy of an upstream, and accepting a push into one would
	// make it diverge from the thing it is mirroring. The next sync would
	// then either clobber the push or fail. Read only is the correct answer
	// rather than a limitation.
	if writing {
		if meta, err := wr.db.Repo(name); err == nil && meta.Mirror {
			http.Error(w, "this repository is a mirror of an upstream and does not accept pushes",
				http.StatusForbidden)
			return
		}
	}

	wr.serveBackend(w, r, repo, name, rest, writing)
}

// serveBackend hands the request to git.
func (wr *wire) serveBackend(w http.ResponseWriter, r *http.Request, repo Repo, name, rest string, writing bool) {
	if wr.backend == "" {
		http.Error(w, "git http-backend not available", http.StatusInternalServerError)
		return
	}
	_ = repo

	env := []string{
		// GIT_PROJECT_ROOT plus PATH_INFO is how http-backend is told which
		// repository to serve. The alternative, GIT_DIR, does not support
		// the /info/refs form.
		"GIT_PROJECT_ROOT=" + wr.store.Root,
		// Without this each repository needs a git-daemon-export-ok file.
		// Everything served here is public by design, and the fence on what
		// exists at all is the directory, so the marker file would be a
		// second place to forget something.
		"GIT_HTTP_EXPORT_ALL=1",
		// GIT_HTTP_MAX_REQUEST_BUFFER is deliberately not raised here. It
		// caps what http-backend reads into memory during ref negotiation,
		// which is the anonymous clone path, so raising it to 100M let any
		// stranger ask this process for a 100MB allocation. It does nothing
		// for the push path, which is the spooling above. git's 10MiB
		// default is ample for negotiation on repositories of this size.
		"GIT_CONFIG_NOSYSTEM=1",
		"HOME=" + os.TempDir(),
	}

	// REMOTE_USER is what git writes into the reflog for a push, so a force
	// push six months from now still says which token did it. http-backend
	// derives GIT_COMMITTER_NAME and GIT_COMMITTER_EMAIL from it.
	if user := userFrom(r.Context()); user != "" {
		env = append(env, "REMOTE_USER="+user)
		// receivepack is enabled per request rather than in the repository
		// config, so a repository is never left in a state where an
		// unauthenticated route could push to it.
		env = append(env, "GIT_HTTP_RECEIVE_PACK=1")
	}

	h := &cgi.Handler{
		Path: wr.backend,
		Dir:  wr.store.Root,
		Env:  env,
		// http-backend reads PATH_INFO relative to GIT_PROJECT_ROOT, so the
		// path it needs is /<name>.git/<rest>, which is the request path
		// with this handler's own prefix already stripped by the mux.
		Root:   "/",
		Logger: nil,
	}

	// The path the mux matched is not necessarily the path http-backend
	// needs, so it is rebuilt rather than passed through.
	r2 := r.Clone(r.Context())
	r2.URL.Path = "/" + name + ".git/" + rest

	// Chunked bodies have to be spooled to a length-delimited buffer before
	// CGI sees them, and without this the site cannot accept a real push at
	// all.
	//
	// net/http/cgi rejects a chunked request outright:
	//
	//	if len(req.TransferEncoding) > 0 && req.TransferEncoding[0] == "chunked" {
	//		rw.WriteHeader(http.StatusBadRequest)
	//
	// and git switches to chunked for any pack larger than http.postBuffer,
	// which defaults to 1MB. So every push except a trivial one would have
	// come back "Chunked request bodies are not supported by CGI." A small
	// test push does not reveal this, because it stays under the buffer and
	// arrives with a Content-Length.
	//
	// The cap depends on who is asking, and that distinction is the whole
	// point of the parameter.
	//
	// A push is authenticated and may legitimately be 100MB, the same ceiling
	// Cloudflare allows in one request. A read is not authenticated: anonymous
	// clone negotiation reaches here too, and its body is a ref list that git
	// itself bounds at GIT_HTTP_MAX_REQUEST_BUFFER, 10MiB by default. Spooling
	// an anonymous body to the push ceiling meant any stranger could open N
	// connections and write N x 100MB into the container's filesystem, which
	// is the host's Docker storage shared with everything else on the box.
	// There is no read timeout on this server (see web/server.go, deliberately,
	// for large pushes), so those writes could also be held open indefinitely.
	limit := int64(maxNegotiationBytes)
	if writing {
		limit = maxPushBytes
	}
	if len(r2.TransferEncoding) > 0 && r2.TransferEncoding[0] == "chunked" {
		spooled, size, err := spoolBody(r2.Body, limit)
		if err != nil {
			slog.Error("spooling push body failed",
				slog.String("repo", name), slog.Any("err", err))
			http.Error(w, "could not read request body", http.StatusBadRequest)
			return
		}
		defer spooled.Close()

		r2.Body = spooled
		r2.ContentLength = size
		r2.TransferEncoding = nil
		r2.Header.Set("Content-Length", strconv.FormatInt(size, 10))
	}

	h.ServeHTTP(w, r2)
}

// spoolBody reads a chunked body to a temporary file and hands back a reader
// that deletes it on Close.
//
// A file rather than memory: a push is up to 100MB and this process has a 512MB
// container limit, so two concurrent pushes buffered in RAM would be an
// out-of-memory kill. The file is unlinked immediately after creation, so it
// has no name for anything else to open and disappears when the handle closes,
// including if the process is killed mid-push.
func spoolBody(body io.ReadCloser, limit int64) (*spooledFile, int64, error) {
	defer body.Close()

	f, err := os.CreateTemp("", "repos-push-*")
	if err != nil {
		return nil, 0, err
	}
	// Unlink now; the open handle keeps it alive.
	_ = os.Remove(f.Name())

	size, err := io.Copy(f, http.MaxBytesReader(nil, body, limit))
	if err != nil {
		f.Close()
		return nil, 0, err
	}
	if _, err := f.Seek(0, io.SeekStart); err != nil {
		f.Close()
		return nil, 0, err
	}
	return &spooledFile{f}, size, nil
}

// maxPushBytes matches the ceiling Cloudflare puts on one request body, so a
// push that the edge would have refused is also refused here rather than
// spooled to disk first.
const maxPushBytes = cloudflareBodyLimit

// maxNegotiationBytes is the ceiling for an unauthenticated body, matching
// git's own GIT_HTTP_MAX_REQUEST_BUFFER default. Clone negotiation is a list of
// refs; anything approaching this is not a client trying to clone.
const maxNegotiationBytes = 10 << 20

type spooledFile struct{ *os.File }

func (s *spooledFile) Close() error { return s.File.Close() }

// authenticate checks a Basic credential and returns the token's label.
//
// The username is ignored entirely. Git requires one and prompts for one, so
// there has to be a field, but there is exactly one account here and the token
// alone identifies it. Accepting any username means `git push` prompts can be
// answered with anything, and the credential helper stores whatever was typed.
func (wr *wire) authenticate(r *http.Request) (string, bool) {
	_, password, ok := r.BasicAuth()
	if !ok || password == "" {
		return "", false
	}
	label, err := wr.db.VerifyToken(password)
	if err != nil {
		// Deliberately not logged with the token in it. A failed push is
		// worth a line; the credential that failed is not.
		slog.Info("push authentication failed",
			slog.String("ip", clientIPOf(r)))
		return "", false
	}
	return label, true
}

// dumbHTTPPaths are the paths a client falls back to when the smart protocol is
// unavailable. Serving them would expose loose objects and the packed-refs file
// directly as static files, bypassing every check above. http-backend does
// handle them, so they are refused here rather than left to it.
func isDumbPath(rest string) bool {
	switch {
	case strings.HasPrefix(rest, "objects/"),
		rest == "HEAD",
		rest == "packed-refs",
		strings.HasPrefix(rest, "refs/"):
		return true
	}
	return false
}

// repoNameFromPath pulls "orchard" out of "orchard.git". Used by the router
// pattern, which matches the .git suffix as part of the segment.
func repoNameFromPath(seg string) (string, bool) {
	name, ok := strings.CutSuffix(seg, ".git")
	if !ok {
		return "", false
	}
	if !validName(name) {
		// Return nothing rather than the rejected name. Every caller checks
		// the bool, but a function that hands back "../x" alongside a false
		// is one refactor away from being a traversal.
		return "", false
	}
	return name, true
}

// cloneURL is what the repository page tells a person to type.
func cloneURL(base, name string) string {
	return strings.TrimSuffix(base, "/") + "/" + name + ".git"
}

// bridgeCloneURL is the same repository reached over the Docker bridge instead
// of through Cloudflare, which is the seeding path documented in the README.
// Shown on the repository page next to the public URL because the moment a
// person needs it is the moment a first push has just failed with a 413.
func bridgeCloneURL(container, name string) string {
	return "http://" + container + ":8000/" + name + ".git"
}

// archiveName is the top level directory inside a downloaded tarball.
func archiveName(repo, ref string) string {
	ref = strings.ReplaceAll(ref, "/", "-")
	return filepath.Base(repo) + "-" + ref
}
