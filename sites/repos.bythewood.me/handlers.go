package main

import (
	"context"
	"fmt"
	"html/template"
	"log/slog"
	"net/http"
	"net/url"
	"sort"
	"strconv"
	"strings"
	"time"

	"repos.bythewood.me/web"
)

// site holds everything a handler needs.
type site struct {
	renderer *web.Renderer
	store    *Store
	db       *DB
	cfg      Config
	backend  string
	script   string
	styles   []string
	// mirror is here only so the settings page can run a sync on demand.
	mirror *Mirror
	auth   *web.Authenticator
}

// page is the data every template gets, so base.html can render the chrome.
type page struct {
	Title       string
	Description string
	Script      string
	Styles      []string
	LoggedIn    bool
	Staging     bool
	// Analytics gates the collector snippet, so staging never files traffic under
	// the real property.
	Analytics   bool
	AnalyticsID string
	SiteName    string
	SiteTagline string
	FooterLinks []FooterLink
	SourceURL   string
	Canonical   string
	AuthorName  string
	// RepoName and RepoRev drive the header's context line. Filled by
	// pageForRepo, empty on the index and on settings.
	RepoName string
	RepoRev  string
	Data     any
}

func (s *site) page(r *http.Request, title, description string, data any) page {
	return page{
		Title:       title,
		Description: description,
		Script:      s.script,
		Styles:      s.styles,
		LoggedIn:    s.auth.Authenticated(r),
		Staging:     Staging,
		Analytics:   !Staging,
		AnalyticsID: analyticsID,
		SiteName:    siteName,
		SiteTagline: siteTagline,
		FooterLinks: footerLinks,
		SourceURL:   sourceURL,
		Canonical:   baseURL + r.URL.Path,
		AuthorName:  authorName,
		Data:        data,
	}
}

// pageForRepo is page plus the repository header context.
func (s *site) pageForRepo(r *http.Request, rc *repoContext, title, description string, data any) page {
	p := s.page(r, title, description, data)
	p.RepoName = rc.Repo.Name
	if !rc.Empty {
		p.RepoRev = rc.Rev
	}
	return p
}

func (s *site) notFound(w http.ResponseWriter, r *http.Request) {
	s.renderer.Render(w, http.StatusNotFound, "notfound.html",
		s.page(r, "Not found", "", nil))
}

// requireLogin gates the routes that change something.
func (s *site) requireLogin(next http.HandlerFunc) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		if !s.auth.Authenticated(r) {
			http.Redirect(w, r, web.LoginURL(r), http.StatusSeeOther)
			return
		}
		next(w, r)
	}
}

// RepoCard is one row on the index.
type RepoCard struct {
	RepoMeta
	Size     int64
	LastPush time.Time
	Branches int
	Tags     int
	Empty    bool
	// PushPercent is how much of one push through the tunnel a from-scratch push
	// of this repository would use.
	PushPercent int
}

func (s *site) index(w http.ResponseWriter, r *http.Request) {
	ctx := r.Context()

	repos, err := s.store.Discover()
	if err != nil {
		slog.Error("discover repos", slog.Any("err", err))
		http.Error(w, "internal server error", http.StatusInternalServerError)
		return
	}
	meta, err := s.db.AllRepos()
	if err != nil {
		slog.Error("read repo metadata", slog.Any("err", err))
		meta = map[string]RepoMeta{}
	}

	cards := make([]RepoCard, 0, len(repos))
	for _, repo := range repos {
		m := meta[repo.Name]
		m.Name = repo.Name
		if m.Hidden {
			continue
		}
		o := s.store.Overview(ctx, repo)
		cards = append(cards, RepoCard{
			RepoMeta:    m,
			Size:        o.Size,
			LastPush:    o.LastPush,
			Branches:    o.Branches,
			Tags:        o.Tags,
			Empty:       o.Empty,
			PushPercent: percentOf(o.Size, cloudflareBodyLimit),
		})
	}

	sort.SliceStable(cards, func(i, j int) bool {
		return cards[i].LastPush.After(cards[j].LastPush)
	})

	s.renderer.Render(w, http.StatusOK, "index.html", s.page(r, "", siteTagline, map[string]any{
		"Repos":     cards,
		"HasTokens": s.db.HasTokens(),
		"CloneURL":  strings.TrimSuffix(baseURL, "/"),
		"Bridge":    containerName,
	}))
}

// repoContext is the shared header every repository page renders.
type repoContext struct {
	Repo     Repo
	Meta     RepoMeta
	Rev      string
	IsBranch bool
	Head     string
	Branches []Ref
	Tags     []Ref
	Empty    bool
	Size     int64

	CloneURL  string
	BridgeURL string
	// BridgeContainer is the container name alone, which is what the push help
	// builds its commands from.
	BridgeContainer string
	// OverLimit says a from-scratch push of this repository is larger than one
	// request through Cloudflare, so seeding it needs the bridge or slices.
	OverLimit   bool
	PushPercent int
}

// resolveRepo loads the shared header, or writes a 404 and reports false.
func (s *site) resolveRepo(w http.ResponseWriter, r *http.Request) (*repoContext, bool) {
	ctx := r.Context()

	name := r.PathValue("name")
	repo, ok := s.store.Open(name)
	if !ok {
		s.notFound(w, r)
		return nil, false
	}

	meta, _ := s.db.Repo(name)
	meta.Name = name

	rc := &repoContext{
		Repo:            repo,
		Meta:            meta,
		Head:            s.store.Head(ctx, repo),
		Empty:           s.store.IsEmpty(ctx, repo),
		Size:            s.store.Size(ctx, repo),
		CloneURL:        cloneURL(baseURL, name),
		BridgeURL:       bridgeCloneURL(containerName, name),
		BridgeContainer: containerName,
	}
	rc.OverLimit = rc.Size > cloudflareBodyLimit
	rc.PushPercent = percentOf(rc.Size, cloudflareBodyLimit)

	if !rc.Empty {
		rc.Branches, _ = s.store.Branches(ctx, repo)
		rc.Tags, _ = s.store.Tags(ctx, repo)
	}

	// Either way the revision is resolved before use, so anything that does not
	// name a commit is a 404 rather than an argument to a later git command.
	rev := r.PathValue("rev")
	if rev == "" {
		rev = rc.Head
	}
	rc.Rev = rev

	for _, b := range rc.Branches {
		if b.Name == rev {
			rc.IsBranch = true
			break
		}
	}
	return rc, true
}

func (s *site) repo(w http.ResponseWriter, r *http.Request) {
	rc, ok := s.resolveRepo(w, r)
	if !ok {
		return
	}
	ctx := r.Context()

	data := map[string]any{"Ctx": rc}

	if !rc.Empty {
		if _, err := s.store.Resolve(ctx, rc.Repo, rc.Rev); err != nil {
			s.notFound(w, r)
			return
		}
		commits, _ := s.store.Log(ctx, rc.Repo, rc.Rev, 0, 10)
		data["Commits"] = commits
		data["CommitCount"] = s.store.CountCommits(ctx, rc.Repo, rc.Rev)

		entries, err := s.store.Tree(ctx, rc.Repo, rc.Rev, "")
		if err == nil {
			data["Entries"] = entries
			if name, html, ok := s.readme(ctx, rc.Repo, rc.Rev, entries); ok {
				data["Readme"] = html
				data["ReadmeName"] = name
			}
		}
	}

	title := rc.Repo.Name
	desc := rc.Meta.Description
	if desc == "" {
		desc = "git repository " + rc.Repo.Name
	}
	s.renderer.Render(w, http.StatusOK, "repo.html", s.pageForRepo(r, rc, title, desc, data))
}

// readme finds and renders the README in a directory listing.
func (s *site) readme(ctx context.Context, repo Repo, rev string, entries []TreeEntry) (string, template.HTML, bool) {
	present := make(map[string]bool, len(entries))
	for _, e := range entries {
		if !e.IsDir() {
			present[e.Name] = true
		}
	}

	for _, want := range readmeNames {
		if !present[want] {
			continue
		}
		src, _, err := s.store.Blob(ctx, repo, rev, want)
		if err != nil {
			continue
		}
		if IsMarkdown(want) {
			html, err := RenderMarkdown(src)
			if err != nil {
				continue
			}
			return want, html, true
		}
		return want, template.HTML("<pre>" +
			template.HTMLEscapeString(string(src)) + "</pre>"), true
	}
	return "", "", false
}

func (s *site) tree(w http.ResponseWriter, r *http.Request) {
	rc, ok := s.resolveRepo(w, r)
	if !ok {
		return
	}
	ctx := r.Context()

	if _, err := s.store.Resolve(ctx, rc.Repo, rc.Rev); err != nil {
		s.notFound(w, r)
		return
	}

	path := strings.Trim(r.PathValue("path"), "/")
	entries, err := s.store.Tree(ctx, rc.Repo, rc.Rev, path)
	if err != nil {
		s.notFound(w, r)
		return
	}

	data := map[string]any{"Ctx": rc, "Entries": entries, "Path": path}
	if name, html, ok := s.readme(ctx, rc.Repo, rc.Rev, entries); ok {
		data["Readme"] = html
		data["ReadmeName"] = name
	}

	title := rc.Repo.Name + "/" + path
	s.renderer.Render(w, http.StatusOK, "tree.html", s.pageForRepo(r, rc, title, "", data))
}

func (s *site) blob(w http.ResponseWriter, r *http.Request) {
	rc, ok := s.resolveRepo(w, r)
	if !ok {
		return
	}
	ctx := r.Context()

	if _, err := s.store.Resolve(ctx, rc.Repo, rc.Rev); err != nil {
		s.notFound(w, r)
		return
	}

	path := strings.Trim(r.PathValue("path"), "/")
	if path == "" {
		s.notFound(w, r)
		return
	}

	src, size, err := s.store.Blob(ctx, rc.Repo, rc.Rev, path)
	data := map[string]any{
		"Ctx":  rc,
		"Path": path,
		"Size": size,
	}

	switch {
	case err == errTooLarge:
		// Not an error page: the file exists and is still downloadable.
		data["TooLarge"] = true
	case err != nil:
		s.notFound(w, r)
		return
	case IsBinary(src):
		data["Binary"] = true
	default:
		if html, ok := Highlight(path, src); ok {
			data["Highlighted"] = html
		} else {
			data["Plain"] = string(src)
		}
		data["Lines"] = strings.Count(string(src), "\n") + 1
		data["Language"] = languageOf(path)
	}

	s.renderer.Render(w, http.StatusOK, "blob.html",
		s.pageForRepo(r, rc, rc.Repo.Name+"/"+path, "", data))
}

// raw streams a file's bytes as text/plain with nosniff. Serving a .html or .svg
// blob with its real content type would be stored XSS on the origin that also
// holds the session cookie.
func (s *site) raw(w http.ResponseWriter, r *http.Request) {
	rc, ok := s.resolveRepo(w, r)
	if !ok {
		return
	}
	ctx := r.Context()

	sha, err := s.store.Resolve(ctx, rc.Repo, rc.Rev)
	if err != nil {
		s.notFound(w, r)
		return
	}

	path := strings.Trim(r.PathValue("path"), "/")
	if path == "" {
		s.notFound(w, r)
		return
	}

	w.Header().Set("Content-Type", "text/plain; charset=utf-8")
	w.Header().Set("X-Content-Type-Options", "nosniff")
	// A blob at a resolved SHA cannot change; at a branch name it can.
	if sha == rc.Rev {
		w.Header().Set("Cache-Control", "public, max-age=31536000, immutable")
	} else {
		w.Header().Set("Cache-Control", "public, max-age=300")
	}

	if err := s.store.StreamBlob(ctx, rc.Repo, rc.Rev, path, w); err != nil {
		// The header is already written, so there is nothing to say to the client.
		slog.Info("raw blob failed",
			slog.String("repo", rc.Repo.Name), slog.String("path", path))
	}
}

func (s *site) log(w http.ResponseWriter, r *http.Request) {
	rc, ok := s.resolveRepo(w, r)
	if !ok {
		return
	}
	ctx := r.Context()

	if rc.Empty {
		s.renderer.Render(w, http.StatusOK, "log.html",
			s.pageForRepo(r, rc, rc.Repo.Name+" log", "", map[string]any{"Ctx": rc}))
		return
	}
	if _, err := s.store.Resolve(ctx, rc.Repo, rc.Rev); err != nil {
		s.notFound(w, r)
		return
	}

	const perPage = 50
	page := 1
	if v := r.URL.Query().Get("page"); v != "" {
		if n, err := strconv.Atoi(v); err == nil && n > 0 && n <= 1000 {
			page = n
		}
	}

	path := strings.Trim(r.PathValue("path"), "/")

	var commits []Commit
	var err error
	if path != "" {
		commits, err = s.store.LogFile(ctx, rc.Repo, rc.Rev, path, (page-1)*perPage, perPage+1)
	} else {
		commits, err = s.store.Log(ctx, rc.Repo, rc.Rev, (page-1)*perPage, perPage+1)
	}
	if err != nil {
		s.notFound(w, r)
		return
	}

	// One extra row rather than counting the whole history, which is a graph walk.
	hasNext := len(commits) > perPage
	if hasNext {
		commits = commits[:perPage]
	}

	s.renderer.Render(w, http.StatusOK, "log.html",
		s.pageForRepo(r, rc, rc.Repo.Name+" log", "", map[string]any{
			"Ctx":     rc,
			"Commits": commits,
			"Path":    path,
			"Page":    page,
			"HasNext": hasNext,
			"HasPrev": page > 1,
		}))
}

func (s *site) commit(w http.ResponseWriter, r *http.Request) {
	rc, ok := s.resolveRepo(w, r)
	if !ok {
		return
	}
	ctx := r.Context()

	sha, err := s.store.Resolve(ctx, rc.Repo, r.PathValue("sha"))
	if err != nil {
		s.notFound(w, r)
		return
	}

	commit, err := s.store.CommitOne(ctx, rc.Repo, sha)
	if err != nil {
		s.notFound(w, r)
		return
	}
	diff, err := s.store.Diff(ctx, rc.Repo, sha)
	if err != nil {
		slog.Error("diff failed", slog.String("repo", rc.Repo.Name),
			slog.String("sha", sha), slog.Any("err", err))
	}

	s.renderer.Render(w, http.StatusOK, "commit.html",
		s.pageForRepo(r, rc, commit.Subject, "", map[string]any{
			"Ctx":    rc,
			"Commit": commit,
			"Diff":   diff,
		}))
}

func (s *site) refsPage(templateName string) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		rc, ok := s.resolveRepo(w, r)
		if !ok {
			return
		}
		s.renderer.Render(w, http.StatusOK, templateName,
			s.pageForRepo(r, rc, rc.Repo.Name, "", map[string]any{"Ctx": rc}))
	}
}

// archive streams a tarball or zip of a revision.
func (s *site) archive(w http.ResponseWriter, r *http.Request) {
	rc, ok := s.resolveRepo(w, r)
	if !ok {
		return
	}
	ctx := r.Context()

	// The format rides as the file extension on the last URL segment.
	rev, format := rc.Rev, ""
	switch {
	case strings.HasSuffix(rev, ".tar.gz"):
		rev, format = strings.TrimSuffix(rev, ".tar.gz"), "tar.gz"
	case strings.HasSuffix(rev, ".zip"):
		rev, format = strings.TrimSuffix(rev, ".zip"), "zip"
	default:
		s.notFound(w, r)
		return
	}

	if _, err := s.store.Resolve(ctx, rc.Repo, rev); err != nil {
		s.notFound(w, r)
		return
	}

	prefix := archiveName(rc.Repo.Name, rev)
	filename := prefix + "." + format

	if format == "zip" {
		w.Header().Set("Content-Type", "application/zip")
	} else {
		w.Header().Set("Content-Type", "application/gzip")
	}
	w.Header().Set("Content-Disposition", `attachment; filename="`+filename+`"`)
	w.Header().Set("X-Content-Type-Options", "nosniff")

	if err := s.store.Archive(ctx, rc.Repo, rev, format, prefix, w); err != nil {
		slog.Error("archive failed",
			slog.String("repo", rc.Repo.Name), slog.Any("err", err))
	}
}

// newTokenCookie carries a freshly minted token from the POST to the page that
// shows it, exactly once.
const newTokenCookie = "new_token"

func (s *site) settings(w http.ResponseWriter, r *http.Request) {
	// This page can render a live push credential, so it must never reach a
	// shared cache. EdgeCache leaves a handler's own choice alone.
	w.Header().Set("Cache-Control", "no-store, private")

	// Read once and clear, so a refresh does not show the token again.
	fresh := ""
	if c, err := r.Cookie(newTokenCookie); err == nil {
		fresh = c.Value
		http.SetCookie(w, &http.Cookie{
			Name: newTokenCookie, Value: "", Path: "/settings",
			Secure: true, HttpOnly: true, SameSite: http.SameSiteStrictMode, MaxAge: -1,
		})
	}

	notice := ""
	if c, err := r.Cookie(mirrorNoticeCookie); err == nil {
		notice, _ = url.QueryUnescape(c.Value)
		http.SetCookie(w, &http.Cookie{
			Name: mirrorNoticeCookie, Value: "", Path: "/settings",
			Secure: true, HttpOnly: true, SameSite: http.SameSiteStrictMode, MaxAge: -1,
		})
	}

	tokens, err := s.db.Tokens()
	if err != nil {
		slog.Error("list tokens", slog.Any("err", err))
	}
	sources, err := s.mirrorSources()
	if err != nil {
		slog.Error("list mirror sources", slog.Any("err", err))
	}
	s.renderer.Render(w, http.StatusOK, "settings.html",
		s.page(r, "Settings", "", map[string]any{
			"Tokens":        tokens,
			"CloneURL":      strings.TrimSuffix(baseURL, "/"),
			"Bridge":        containerName,
			"Sources":       sources,
			"MirrorEnabled": s.mirrorReady(),
			"MirrorEvery":   shortDuration(s.cfg.MirrorEvery),
			"Notice":        notice,
			"NewToken":      fresh,
		}))
}

func (s *site) createToken(w http.ResponseWriter, r *http.Request) {
	token, err := s.db.CreateToken(r.FormValue("label"))
	if err != nil {
		slog.Error("create token", slog.Any("err", err))
		http.Error(w, "could not create token", http.StatusInternalServerError)
		return
	}
	// A one-shot cookie rather than a query string: a bearer credential in a URL
	// lands in the edge access log, a Referer header and browser history.
	http.SetCookie(w, &http.Cookie{
		Name:     newTokenCookie,
		Value:    token,
		Path:     "/settings",
		Secure:   true,
		HttpOnly: true,
		SameSite: http.SameSiteStrictMode,
		MaxAge:   60,
	})
	http.Redirect(w, r, "/settings", http.StatusSeeOther)
}

func (s *site) revokeToken(w http.ResponseWriter, r *http.Request) {
	id, err := strconv.ParseInt(r.PathValue("id"), 10, 64)
	if err != nil {
		s.notFound(w, r)
		return
	}
	if err := s.db.RevokeToken(id); err != nil {
		slog.Error("revoke token", slog.Any("err", err))
	}
	http.Redirect(w, r, "/settings", http.StatusSeeOther)
}

// editRepo saves the description and topics, which git has nowhere to put.
func (s *site) editRepo(w http.ResponseWriter, r *http.Request) {
	name := r.PathValue("name")
	if _, ok := s.store.Open(name); !ok {
		s.notFound(w, r)
		return
	}

	// A mirror's description is overwritten on the next sync, so it is not editable.
	if meta, err := s.db.Repo(name); err == nil && meta.Mirror {
		http.Error(w, "this repository is a mirror; its description comes from upstream",
			http.StatusForbidden)
		return
	}

	var topics []string
	for _, t := range strings.Split(r.FormValue("topics"), ",") {
		if t = strings.TrimSpace(t); t != "" {
			topics = append(topics, t)
		}
	}
	if err := s.db.SetDescription(name,
		strings.TrimSpace(r.FormValue("description")), topics,
		strings.TrimSpace(r.FormValue("homepage"))); err != nil {
		slog.Error("save description", slog.Any("err", err))
	}
	http.Redirect(w, r, "/"+name, http.StatusSeeOther)
}

// atom is the per-repository commit feed.
func (s *site) atom(w http.ResponseWriter, r *http.Request) {
	rc, ok := s.resolveRepo(w, r)
	if !ok {
		return
	}
	ctx := r.Context()

	if rc.Empty {
		s.notFound(w, r)
		return
	}
	commits, err := s.store.Log(ctx, rc.Repo, rc.Head, 0, 20)
	if err != nil {
		s.notFound(w, r)
		return
	}

	w.Header().Set("Content-Type", "application/atom+xml; charset=utf-8")
	w.Header().Set("Cache-Control", "public, max-age=600")

	repoURL := baseURL + "/" + rc.Repo.Name
	updated := time.Now().UTC()
	if len(commits) > 0 {
		updated = commits[0].Commit
	}

	fmt.Fprintf(w, `<?xml version="1.0" encoding="utf-8"?>`+"\n")
	fmt.Fprintf(w, `<feed xmlns="http://www.w3.org/2005/Atom">`+"\n")
	fmt.Fprintf(w, "  <title>%s</title>\n", xmlEscape(rc.Repo.Name))
	fmt.Fprintf(w, "  <id>%s</id>\n", xmlEscape(repoURL))
	fmt.Fprintf(w, `  <link href="%s"/>`+"\n", xmlEscape(repoURL))
	fmt.Fprintf(w, `  <link rel="self" href="%s/atom.xml"/>`+"\n", xmlEscape(repoURL))
	fmt.Fprintf(w, "  <updated>%s</updated>\n", updated.Format(time.RFC3339))

	for _, c := range commits {
		url := repoURL + "/commit/" + c.SHA
		fmt.Fprintf(w, "  <entry>\n")
		fmt.Fprintf(w, "    <title>%s</title>\n", xmlEscape(c.Subject))
		fmt.Fprintf(w, "    <id>%s</id>\n", xmlEscape(url))
		fmt.Fprintf(w, `    <link href="%s"/>`+"\n", xmlEscape(url))
		fmt.Fprintf(w, "    <updated>%s</updated>\n", c.Commit.Format(time.RFC3339))
		fmt.Fprintf(w, "    <author><name>%s</name></author>\n", xmlEscape(c.Author))
		fmt.Fprintf(w, "    <content type=\"text\">%s</content>\n", xmlEscape(c.Body))
		fmt.Fprintf(w, "  </entry>\n")
	}
	fmt.Fprintf(w, "</feed>\n")
}

func xmlEscape(s string) string {
	r := strings.NewReplacer(
		"&", "&amp;", "<", "&lt;", ">", "&gt;", `"`, "&quot;", "'", "&apos;")
	return r.Replace(s)
}

// mirrorNoticeCookie carries the outcome of a settings POST back to the page that
// renders it, keeping the message out of the URL and the access log.
const mirrorNoticeCookie = "mirror_notice"

func (s *site) setMirrorNotice(w http.ResponseWriter, msg string) {
	http.SetCookie(w, &http.Cookie{
		Name: mirrorNoticeCookie, Value: url.QueryEscape(msg), Path: "/settings",
		Secure: true, HttpOnly: true, SameSite: http.SameSiteStrictMode, MaxAge: 60,
	})
}

// SourceView is one configured source with what it has actually brought in.
type SourceView struct {
	MirrorSource
	Repos    int
	LastSync time.Time
	Failing  int
}

// mirrorSources joins the configured sources against the repository rows.
func (s *site) mirrorSources() ([]SourceView, error) {
	sources, err := s.db.MirrorSources()
	if err != nil {
		return nil, err
	}
	repos, err := s.db.AllRepos()
	if err != nil {
		return nil, err
	}

	out := make([]SourceView, 0, len(sources))
	for _, src := range sources {
		v := SourceView{MirrorSource: src}
		for _, meta := range repos {
			if !meta.Mirror || !coveredBySource(meta.Upstream, []MirrorSource{src}) {
				continue
			}
			v.Repos++
			if meta.LastSyncErr != "" {
				v.Failing++
			}
			if meta.LastSync.After(v.LastSync) {
				v.LastSync = meta.LastSync
			}
		}
		out = append(out, v)
	}
	return out, nil
}

// addMirrorSource accepts either "owner" or "owner/repo" from one field.
func (s *site) addMirrorSource(w http.ResponseWriter, r *http.Request) {
	src, err := ParseMirrorSource(r.FormValue("source"))
	if err != nil {
		s.setMirrorNotice(w, err.Error())
		http.Redirect(w, r, "/settings", http.StatusSeeOther)
		return
	}
	if err := s.db.AddMirrorSource(src); err != nil {
		slog.Error("add mirror source", slog.Any("err", err))
		s.setMirrorNotice(w, "could not save that source")
		http.Redirect(w, r, "/settings", http.StatusSeeOther)
		return
	}

	// Sync now rather than at the next tick, so a typo'd name is reported here.
	msg := "Added " + src.Label() + ". Syncing now."
	if !s.mirrorReady() {
		msg = "Added " + src.Label() + ". The mirror lane is disabled, so nothing will sync."
	} else if !s.mirror.TrySync(context.Background()) {
		msg = "Added " + src.Label() + ". A sync is already running; it will be picked up next time."
	}
	s.setMirrorNotice(w, msg)
	http.Redirect(w, r, "/settings", http.StatusSeeOther)
}

// deleteMirrorSource stops watching a source; nothing on disk is touched.
func (s *site) deleteMirrorSource(w http.ResponseWriter, r *http.Request) {
	id, err := strconv.ParseInt(r.PathValue("id"), 10, 64)
	if err != nil {
		http.Error(w, "bad source id", http.StatusBadRequest)
		return
	}
	if err := s.db.DeleteMirrorSource(id); err != nil {
		slog.Error("delete mirror source", slog.Any("err", err))
		s.setMirrorNotice(w, "could not remove that source")
	} else {
		s.setMirrorNotice(w, "Source removed. Everything it mirrored is still on disk.")
	}
	http.Redirect(w, r, "/settings", http.StatusSeeOther)
}

// syncMirrors runs the lane now instead of waiting for the ticker.
func (s *site) syncMirrors(w http.ResponseWriter, r *http.Request) {
	switch {
	case !s.mirrorReady():
		s.setMirrorNotice(w, "The mirror lane is disabled (REPOS_MIRROR=0).")
	case s.mirror.TrySync(context.Background()):
		s.setMirrorNotice(w, "Sync started. Reload in a moment to see it land.")
	default:
		s.setMirrorNotice(w, "A sync is already running.")
	}
	http.Redirect(w, r, "/settings", http.StatusSeeOther)
}

func (s *site) mirrorReady() bool { return s.mirror != nil && s.cfg.MirrorEnabled }

// shortDuration trims the zero tail off a Duration, so it reads "6h" not "6h0m0s".
func shortDuration(d time.Duration) string {
	out := d.String()
	out = strings.TrimSuffix(out, "0s")
	out = strings.TrimSuffix(out, "0m")
	return out
}
