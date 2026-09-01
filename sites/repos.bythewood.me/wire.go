package main

// The git smart HTTP wire: clone, fetch and push, over net/http/cgi. Basic auth
// because git's credential subsystem natively fills, stores and replays no other
// scheme; the password field carries a random token. The browser UI is in auth.go.

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

// gitHTTPBackend finds git's own CGI, which is not on PATH and moves between
// distributions.
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
	// allowCreate turns push-to-create on.
	allowCreate bool
}

// Router puts the wire in front of the browse mux. A wrapper rather than mux
// patterns because a ServeMux wildcard matches a whole segment, so "{name}.git"
// cannot be registered.
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
	if isDumbPath(rest) {
		http.NotFound(w, r)
		return
	}

	// The service=git-receive-pack probe must be challenged too: answer it
	// anonymously and git never asks for a credential, so the push that follows
	// fails with a 401 the user cannot act on.
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
		// Push to create, reachable only after the authentication above.
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

	// A push into a mirror would diverge from upstream, and the next sync would
	// clobber it or fail.
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
		// repository to serve; GIT_DIR does not support the /info/refs form.
		"GIT_PROJECT_ROOT=" + wr.store.Root,
		// Without this each repository needs a git-daemon-export-ok file.
		"GIT_HTTP_EXPORT_ALL=1",
		// Do not raise GIT_HTTP_MAX_REQUEST_BUFFER: it bounds what http-backend
		// reads into memory during anonymous ref negotiation, and does nothing
		// for the push path, which is the spooling below.
		"GIT_CONFIG_NOSYSTEM=1",
		"HOME=" + os.TempDir(),
	}

	// http-backend derives the committer identity from REMOTE_USER, so the reflog
	// records which token made a push.
	if user := userFrom(r.Context()); user != "" {
		env = append(env, "REMOTE_USER="+user)
		// Enabled per request, never in repository config, so no repository is
		// left in a state an unauthenticated route could push to.
		env = append(env, "GIT_HTTP_RECEIVE_PACK=1")
	}

	h := &cgi.Handler{
		Path: wr.backend,
		Dir:  wr.store.Root,
		Env:  env,
		// http-backend reads PATH_INFO relative to GIT_PROJECT_ROOT, so it needs
		// /<name>.git/<rest>.
		Root:   "/",
		Logger: nil,
	}

	r2 := r.Clone(r.Context())
	r2.URL.Path = "/" + name + ".git/" + rest

	// net/http/cgi rejects a chunked request outright, and git switches to chunked
	// for any pack over http.postBuffer, so a real push must be spooled to a
	// length-delimited body first. An unauthenticated body gets the smaller cap.
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

	// http-backend has written the refs by now, so the listing card for this
	// repository is stale the moment a push finishes.
	if writing {
		wr.store.InvalidateOverview(name)
	}
}

// spoolBody reads a chunked body to a temporary file, unlinked at creation so it
// has no name to open and vanishes with the handle even on a kill mid-push.
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

// maxPushBytes matches Cloudflare's request body ceiling, so a push the edge would
// refuse is refused here rather than spooled to disk first.
const maxPushBytes = cloudflareBodyLimit

// maxNegotiationBytes caps an unauthenticated body, matching git's own
// GIT_HTTP_MAX_REQUEST_BUFFER default.
const maxNegotiationBytes = 10 << 20

type spooledFile struct{ *os.File }

func (s *spooledFile) Close() error { return s.File.Close() }

// authenticate checks a Basic credential and returns the token's label. The
// username is ignored: git requires a field, and the token alone identifies.
func (wr *wire) authenticate(r *http.Request) (string, bool) {
	_, password, ok := r.BasicAuth()
	if !ok || password == "" {
		return "", false
	}
	label, err := wr.db.VerifyToken(password)
	if err != nil {
		// Never log the credential that failed.
		slog.Info("push authentication failed",
			slog.String("ip", clientIPOf(r)))
		return "", false
	}
	return label, true
}

// isDumbPath matches the dumb-HTTP fallback paths. http-backend would serve loose
// objects and packed-refs as static files, bypassing every check in serve.
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

// repoNameFromPath pulls "orchard" out of "orchard.git".
func repoNameFromPath(seg string) (string, bool) {
	name, ok := strings.CutSuffix(seg, ".git")
	if !ok {
		return "", false
	}
	if !validName(name) {
		// Return nothing rather than the rejected name, which would be one
		// refactor away from a traversal.
		return "", false
	}
	return name, true
}

// cloneURL is what the repository page tells a person to type.
func cloneURL(base, name string) string {
	return strings.TrimSuffix(base, "/") + "/" + name + ".git"
}

// bridgeCloneURL reaches the repository over the Docker bridge instead of through
// Cloudflare, which is how a first push too large for the tunnel gets seeded.
func bridgeCloneURL(container, name string) string {
	return "http://" + container + ":8000/" + name + ".git"
}

// archiveName is the top level directory inside a downloaded tarball.
func archiveName(repo, ref string) string {
	ref = strings.ReplaceAll(ref, "/", "-")
	return filepath.Base(repo) + "-" + ref
}
