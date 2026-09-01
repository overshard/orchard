package main

// The mirror lane. Pushed repositories are the working set and mirrored ones are
// the backup, with one browse UI over both. Every call to GitHub is
// unauthenticated, so there is no access token anywhere in this site.

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"log/slog"
	"net/http"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"sync"
	"sync/atomic"
	"time"
)

// syncTimeout bounds one repository's fetch, whose first run is a full clone.
const syncTimeout = 10 * time.Minute

// GitHubRepo is the subset of the API response this uses.
type GitHubRepo struct {
	Name     string   `json:"name"`
	FullName string   `json:"full_name"`
	CloneURL string   `json:"clone_url"`
	Desc     string   `json:"description"`
	Homepage string   `json:"homepage"`
	Topics   []string `json:"topics"`
	Archived bool     `json:"archived"`
	Fork     bool     `json:"fork"`
	Private  bool     `json:"private"`
	Size     int      `json:"size"`
	Owner    struct {
		Login string `json:"login"`
	} `json:"owner"`

	// explicit is true when a source named this repository outright. An account
	// sweep skips forks; a repository asked for by name is not skipped.
	explicit bool
}

// Mirror keeps the local copies in step with the account.
type Mirror struct {
	store *Store
	db    *DB

	// Set while a manual sync is in flight, so a second click is declined rather
	// than parked on the mutex below.
	running atomic.Bool

	// One sync at a time: two `git remote update` runs against one repository
	// fight over the lock file.
	mu sync.Mutex
}

func NewMirror(store *Store, db *DB) *Mirror {
	return &Mirror{store: store, db: db}
}

// Run syncs on a ticker until the context is cancelled.
func (m *Mirror) Run(ctx context.Context, every time.Duration) {
	// A sync at startup too, or a container that restarts daily never syncs.
	if err := m.Sync(ctx); err != nil {
		slog.Error("initial mirror sync failed", slog.Any("err", err))
	}

	t := time.NewTicker(every)
	defer t.Stop()

	for {
		select {
		case <-ctx.Done():
			return
		case <-t.C:
			if err := m.Sync(ctx); err != nil {
				slog.Error("mirror sync failed", slog.Any("err", err))
			}
		}
	}
}

// Sync lists the account and brings every mirrored repository up to date.
func (m *Mirror) Sync(ctx context.Context) error {
	m.mu.Lock()
	defer m.mu.Unlock()

	sources, err := m.db.MirrorSources()
	if err != nil {
		return err
	}
	if len(sources) == 0 {
		slog.Info("mirror sync skipped: no sources configured")
		return nil
	}

	remote, err := m.list(ctx, sources)
	if err != nil {
		return err
	}
	slog.Info("mirror sync starting",
		slog.Int("sources", len(sources)), slog.Int("upstream_repos", len(remote)))

	seen := make(map[string]bool, len(remote))
	for _, gh := range remote {
		// A private repository needs a token this site does not have, and an
		// account sweep skips forks unless one was named outright.
		if gh.Private || (gh.Fork && !gh.explicit) {
			continue
		}
		// Ownership of a name is decided by what is on disk, not by a database
		// row: a bare repository with no mirror row is a pushed one, offered to
		// adopt and skipped if it declines.
		if repo, onDisk := m.store.Open(gh.Name); onDisk {
			if meta, err := m.db.Repo(gh.Name); err != nil || !meta.Mirror {
				adopted, err := m.adopt(ctx, gh, repo)
				if err != nil {
					slog.Error("adopt failed",
						slog.String("repo", gh.Name), slog.Any("err", err))
				}
				if !adopted {
					seen[gh.Name] = true
					continue
				}
			}
		}
		// Directories are named for the repository alone, so two accounts can
		// collide on one name. First claim wins and the loser is refused.
		if meta, err := m.db.Repo(gh.Name); err == nil && meta.Mirror &&
			meta.Upstream != "" && meta.Upstream != gh.CloneURL {
			slog.Warn("name is already mirrored from a different upstream",
				slog.String("repo", gh.Name),
				slog.String("mirroring", meta.Upstream),
				slog.String("refused", gh.CloneURL))
			continue
		}
		seen[gh.Name] = true

		if err := m.syncOne(ctx, gh); err != nil {
			slog.Error("mirror repo failed",
				slog.String("repo", gh.Name), slog.Any("err", err))
			_ = m.db.RecordSync(gh.Name, err)
			continue
		}
		_ = m.db.RecordSync(gh.Name, nil)
	}

	// Anything mirrored that upstream no longer lists is gone from GitHub. The
	// local copy is never deleted.
	all, err := m.db.AllRepos()
	if err != nil {
		return err
	}
	for name, meta := range all {
		if !meta.Mirror {
			continue
		}
		// A repository whose source was removed is not gone from GitHub, only
		// no longer watched.
		if !coveredBySource(meta.Upstream, sources) {
			continue
		}
		gone := !seen[name]
		if gone != meta.UpstreamGone {
			if gone {
				slog.Warn("upstream repository is gone; local mirror is now the only copy",
					slog.String("repo", name))
			}
			_ = m.db.MarkUpstreamGone(name, gone)
		}
	}
	return nil
}

// list reads every configured source. One source failing is logged and the rest
// still run, so a typo does not stop the other sources being backed up.
func (m *Mirror) list(ctx context.Context, sources []MirrorSource) ([]GitHubRepo, error) {
	client := &http.Client{Timeout: 30 * time.Second}

	var all []GitHubRepo
	// Keyed by full_name, since a sweep and a named repository can reach the same
	// one. An explicit entry replaces a swept one, overriding the fork skip.
	seen := make(map[string]int)
	add := func(gh GitHubRepo) {
		if i, ok := seen[gh.FullName]; ok {
			if gh.explicit {
				all[i] = gh
			}
			return
		}
		seen[gh.FullName] = len(all)
		all = append(all, gh)
	}

	var failed int
	for _, src := range sources {
		var err error
		switch src.Kind {
		case sourceRepo:
			var gh GitHubRepo
			if err = getJSON(ctx, client,
				fmt.Sprintf("https://api.github.com/repos/%s/%s", src.Owner, src.Name),
				&gh); err == nil {
				gh.explicit = true
				add(gh)
			}
		default:
			var repos []GitHubRepo
			if repos, err = m.listAccount(ctx, client, src.Owner); err == nil {
				for _, gh := range repos {
					add(gh)
				}
			}
		}
		if err != nil {
			failed++
			slog.Error("mirror source failed",
				slog.String("source", src.Label()), slog.Any("err", err))
		}
	}

	if len(all) == 0 && failed > 0 {
		return nil, fmt.Errorf("every mirror source failed (%d)", failed)
	}
	return all, nil
}

// listAccount pages through one account's public repositories.
func (m *Mirror) listAccount(ctx context.Context, client *http.Client, owner string) ([]GitHubRepo, error) {
	var all []GitHubRepo
	for page := 1; page <= 10; page++ {
		url := fmt.Sprintf(
			"https://api.github.com/users/%s/repos?per_page=100&type=owner&page=%d",
			owner, page)

		var got []GitHubRepo
		if err := getJSON(ctx, client, url, &got); err != nil {
			return nil, err
		}
		all = append(all, got...)
		if len(got) < 100 {
			break
		}
	}
	return all, nil
}

// getJSON is one unauthenticated GitHub call, rate limited by IP at 60 an hour.
// The API requires a User-Agent and answers a missing one with a 403.
func getJSON(ctx context.Context, client *http.Client, url string, out any) error {
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, url, nil)
	if err != nil {
		return err
	}
	req.Header.Set("Accept", "application/vnd.github+json")
	req.Header.Set("User-Agent", "repos.bythewood.me")

	resp, err := client.Do(req)
	if err != nil {
		return err
	}
	defer resp.Body.Close()

	if resp.StatusCode != http.StatusOK {
		return fmt.Errorf("github returned %d for %s", resp.StatusCode, url)
	}
	if err := json.NewDecoder(resp.Body).Decode(out); err != nil {
		return fmt.Errorf("decode %s: %w", url, err)
	}
	return nil
}

// coveredBySource reports whether a mirrored repository still belongs to a
// configured source, which scopes the upstream_gone check.
func coveredBySource(upstream string, sources []MirrorSource) bool {
	path := strings.TrimSuffix(strings.TrimPrefix(upstream, "https://github.com/"), ".git")
	owner, name, ok := strings.Cut(path, "/")
	if !ok {
		return false
	}
	for _, src := range sources {
		if !strings.EqualFold(src.Owner, owner) {
			continue
		}
		if src.Kind == sourceAccount || strings.EqualFold(src.Name, name) {
			return true
		}
	}
	return false
}

// adopt converts a pushed repository into a mirror in place, but only if upstream
// already contains every local branch and matches every tag exactly. The probe
// fetch writes to refs/adopt/*, so a repository that fails is left untouched.
func (m *Mirror) adopt(ctx context.Context, gh GitHubRepo, repo Repo) (bool, error) {
	ctx, cancel := context.WithTimeout(ctx, syncTimeout)
	defer cancel()

	// Removing origin first makes this safe to re-run after a probe that failed
	// partway.
	_, _ = run(ctx, repo, "remote", "remove", "origin")
	if _, err := run(ctx, repo, "remote", "add", "origin", gh.CloneURL); err != nil {
		return false, err
	}

	// Without --no-tags the fetch follows tags into refs/tags/*, which are real
	// refs, and the probe stops being read-only.
	cmd := gitCmd(ctx, repo, "fetch", "--no-tags", "--prune", "origin",
		"+refs/heads/*:refs/adopt/heads/*", "+refs/tags/*:refs/adopt/tags/*")
	var stderr bytes.Buffer
	cmd.Stderr = &stderr
	if err := cmd.Run(); err != nil {
		_, _ = run(ctx, repo, "remote", "remove", "origin")
		return false, fmt.Errorf("probe fetch %s: %w: %s",
			gh.Name, err, strings.TrimSpace(stderr.String()))
	}

	upstream, err := refMap(ctx, repo, "refs/adopt")
	if err != nil {
		return false, err
	}
	local, err := refMap(ctx, repo, "refs/heads", "refs/tags")
	if err != nil {
		return false, err
	}

	ok := true
	for name, sha := range local {
		// refs/heads/main is probed as refs/adopt/heads/main.
		up, found := upstream["refs/adopt/"+strings.TrimPrefix(name, "refs/")]
		switch {
		case !found:
			slog.Warn("not adopting: ref is not on upstream",
				slog.String("repo", gh.Name), slog.String("ref", name))
			ok = false
		case up == sha:
		case !strings.HasPrefix(name, "refs/heads/"):
			slog.Warn("not adopting: tag differs from upstream",
				slog.String("repo", gh.Name), slog.String("ref", name))
			ok = false
		default:
			// --is-ancestor is a predicate: the exit status is the answer.
			if err := gitCmd(ctx, repo,
				"merge-base", "--is-ancestor", sha, up).Run(); err != nil {
				slog.Warn("not adopting: local commits are not on upstream",
					slog.String("repo", gh.Name), slog.String("ref", name))
				ok = false
			}
		}
	}

	// The probe refs come out either way.
	for name := range upstream {
		if _, err := run(ctx, repo, "update-ref", "-d", name); err != nil {
			return false, err
		}
	}
	if !ok {
		_, _ = run(ctx, repo, "remote", "remove", "origin")
		return false, nil
	}

	// Only now does origin become a mirror remote, which is what makes the
	// `remote update --prune` in syncOne rewrite refs rather than add to them.
	for _, kv := range [][2]string{
		{"remote.origin.fetch", "+refs/*:refs/*"},
		{"remote.origin.mirror", "true"},
		// The same backup settings clone gives a fresh mirror.
		{"core.logAllRefUpdates", "true"},
		{"gc.reflogExpire", "never"},
		{"gc.reflogExpireUnreachable", "never"},
		{"receive.autogc", "false"},
	} {
		if _, err := run(ctx, repo, "config", kv[0], kv[1]); err != nil {
			return false, err
		}
	}

	slog.Info("adopted pushed repository as a mirror",
		slog.String("repo", gh.Name), slog.String("upstream", gh.CloneURL))
	return true, nil
}

// refMap reads refs under the given prefixes as full refname to object id.
func refMap(ctx context.Context, repo Repo, prefixes ...string) (map[string]string, error) {
	args := append([]string{"for-each-ref", "--format=%(objectname)%00%(refname)"}, prefixes...)
	out, err := run(ctx, repo, args...)
	if err != nil {
		return nil, err
	}
	refs := make(map[string]string)
	for _, line := range strings.Split(strings.TrimSpace(string(out)), "\n") {
		if line == "" {
			continue
		}
		sha, name, found := strings.Cut(line, "\x00")
		if !found {
			continue
		}
		refs[name] = sha
	}
	return refs, nil
}

// syncOne clones or updates one mirror.
func (m *Mirror) syncOne(ctx context.Context, gh GitHubRepo) error {
	if !validName(gh.Name) {
		return fmt.Errorf("upstream name is not usable here: %q", gh.Name)
	}

	ctx, cancel := context.WithTimeout(ctx, syncTimeout)
	defer cancel()

	path := filepath.Join(m.store.Root, gh.Name+".git")

	if _, err := os.Stat(path); os.IsNotExist(err) {
		if err := m.clone(ctx, gh, path); err != nil {
			return err
		}
	} else {
		repo := Repo{Name: gh.Name, Path: path}
		// --prune propagates an upstream deletion, which is why every mirror is
		// created with a never-expiring reflog. gitCmd rather than run, because
		// run imposes gitTimeout, which is far too short for a fetch.
		cmd := gitCmd(ctx, repo, "remote", "update", "--prune")
		var stderr bytes.Buffer
		cmd.Stderr = &stderr
		if err := cmd.Run(); err != nil {
			return fmt.Errorf("remote update %s: %w: %s",
				gh.Name, err, strings.TrimSpace(stderr.String()))
		}
	}

	m.store.InvalidateOverview(gh.Name)

	if err := m.db.MarkMirror(gh.Name, gh.CloneURL, gh.Archived); err != nil {
		return err
	}
	// Upstream owns a mirror's description and topics, and the UI will not edit
	// them, so overwriting on each sync is correct.
	return m.db.SetDescription(gh.Name, gh.Desc, gh.Topics, gh.Homepage)
}

func (m *Mirror) clone(ctx context.Context, gh GitHubRepo, path string) error {
	slog.Info("cloning mirror",
		slog.String("repo", gh.Name), slog.Int("upstream_kb", gh.Size))

	cmd := exec.CommandContext(ctx, "git", "clone", "--mirror", gh.CloneURL, path)
	cmd.Env = append(os.Environ(),
		"GIT_TERMINAL_PROMPT=0", "GIT_CONFIG_NOSYSTEM=1", "HOME="+os.TempDir())

	if out, err := cmd.CombinedOutput(); err != nil {
		return fmt.Errorf("clone %s: %w: %s", gh.Name, err, strings.TrimSpace(string(out)))
	}

	// core.logAllRefUpdates is off by default in a bare repository, and it is what
	// leaves an upstream force push's old tip recoverable in the reflog.
	repo := Repo{Name: gh.Name, Path: path}
	for _, kv := range [][2]string{
		{"core.logAllRefUpdates", "true"},
		{"gc.reflogExpire", "never"},
		{"gc.reflogExpireUnreachable", "never"},
		{"receive.autogc", "false"},
	} {
		if _, err := run(ctx, repo, "config", kv[0], kv[1]); err != nil {
			return err
		}
	}
	return nil
}

// RunGC repacks every repository on a ticker, which is what makes
// receive.autogc=false safe to set.
func RunGC(ctx context.Context, store *Store, every time.Duration) {
	t := time.NewTicker(every)
	defer t.Stop()

	for {
		select {
		case <-ctx.Done():
			return
		case <-t.C:
			repos, err := store.Discover()
			if err != nil {
				slog.Error("gc: discover failed", slog.Any("err", err))
				continue
			}
			for _, repo := range repos {
				if err := store.GC(ctx, repo); err != nil {
					slog.Error("gc failed",
						slog.String("repo", repo.Name), slog.Any("err", err))
				}
			}
		}
	}
}

// TrySync runs a sync in the background and reports whether it started one. It
// declines rather than queues, so two clicks do not mean two syncs.
func (m *Mirror) TrySync(ctx context.Context) bool {
	if !m.running.CompareAndSwap(false, true) {
		return false
	}
	go func() {
		defer m.running.Store(false)
		if err := m.Sync(ctx); err != nil {
			slog.Error("manual mirror sync failed", slog.Any("err", err))
		}
	}()
	return true
}
