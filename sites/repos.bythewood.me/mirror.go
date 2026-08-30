package main

// The mirror lane.
//
// This site has two ways a repository arrives. A push makes it a real remote,
// which is the reason the site exists. A mirror pulls from GitHub, and it is
// here because of one finding that the push half does not address: nine
// repositories on the account exist only on GitHub, with no local copy at all,
// and `code-sync` will never fetch them because it clones non-archived repos by
// design and all nine are archived. That gap is structural, not an oversight,
// and pushing to this site by hand does not close it.
//
// So: pushed repositories are the working set, mirrored ones are the backup,
// one browse UI over both, and the index says which is which.
//
// No credentials. Every repository on the account is public, which is what
// keeps this site in the blog tier rather than the analytics tier: no personal
// access token, nothing in .env that is worth stealing.

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
	"time"
)

// syncTimeout bounds one repository's fetch. Generous because the first sync of
// a repository is a full clone.
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
}

// Mirror keeps the local copies in step with the account.
type Mirror struct {
	store *Store
	db    *DB
	user  string

	// One sync at a time. Two concurrent `git remote update` runs against
	// the same repository would fight over the lock file, and running every
	// repository in parallel would saturate the uplink for no gain when
	// twenty of twenty-one are frozen.
	mu sync.Mutex
}

func NewMirror(store *Store, db *DB, user string) *Mirror {
	return &Mirror{store: store, db: db, user: user}
}

// Run syncs on a ticker until the context is cancelled. In process rather than
// a sidecar container, matching how status already does scheduled work: it
// keeps this to one image, and lets the site render its own sync state, which a
// sidecar cannot do without a side channel.
func (m *Mirror) Run(ctx context.Context, every time.Duration) {
	// A sync at startup, then on the ticker. Without the first one, a
	// container that restarts daily never syncs at all.
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

	remote, err := m.list(ctx)
	if err != nil {
		return err
	}
	slog.Info("mirror sync starting", slog.Int("upstream_repos", len(remote)))

	seen := make(map[string]bool, len(remote))
	for _, gh := range remote {
		// Forks are somebody else's code with a pointer to it, and a
		// private repository cannot be cloned without a token this site
		// deliberately does not have.
		if gh.Fork || gh.Private {
			continue
		}
		// A pushed repository of the same name wins, and ownership is decided
		// by what is on disk rather than by a database row.
		//
		// The row-only version of this check had a hole that mattered: the
		// first sync runs at startup, before anything has been pushed, so on a
		// fresh deploy the mirror cloned every active repository first and
		// flagged it as a mirror. Every later push to those names was then
		// refused with 403 as a push to a mirror, and the repositories anybody
		// actually pushes here are exactly the ones that would be taken over.
		//
		// A bare repository present with no mirror row is a pushed repository.
		// It is offered to adopt first, and skipped if it declines: a pushed
		// repository that also exists on GitHub loses nothing by becoming a
		// mirror, and gains the description and topics the push lane has no
		// source for.
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
		seen[gh.Name] = true

		if err := m.syncOne(ctx, gh); err != nil {
			slog.Error("mirror repo failed",
				slog.String("repo", gh.Name), slog.Any("err", err))
			_ = m.db.RecordSync(gh.Name, err)
			continue
		}
		_ = m.db.RecordSync(gh.Name, nil)
	}

	// Anything mirrored that upstream no longer lists has gone from GitHub.
	// This is the flag the whole backup half exists for, so it is set loudly
	// rather than logged quietly, and the local copy is never deleted.
	all, err := m.db.AllRepos()
	if err != nil {
		return err
	}
	for name, meta := range all {
		if !meta.Mirror {
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

// list reads the account's public repositories. Paginated because the default
// page is 30 and the account has more than that.
func (m *Mirror) list(ctx context.Context) ([]GitHubRepo, error) {
	client := &http.Client{Timeout: 30 * time.Second}

	var all []GitHubRepo
	for page := 1; page <= 10; page++ {
		url := fmt.Sprintf(
			"https://api.github.com/users/%s/repos?per_page=100&type=owner&page=%d",
			m.user, page)

		req, err := http.NewRequestWithContext(ctx, http.MethodGet, url, nil)
		if err != nil {
			return nil, err
		}
		req.Header.Set("Accept", "application/vnd.github+json")
		// Unauthenticated requests are rate limited by IP at 60 an hour.
		// One sync is a handful of requests and the ticker is hours apart,
		// so this never approaches it, but the User-Agent is required by
		// the API and a missing one is a 403 that reads like a ban.
		req.Header.Set("User-Agent", "repos.bythewood.me")

		resp, err := client.Do(req)
		if err != nil {
			return nil, fmt.Errorf("list repos: %w", err)
		}

		var page_ []GitHubRepo
		err = json.NewDecoder(resp.Body).Decode(&page_)
		status := resp.StatusCode
		resp.Body.Close()

		if status != http.StatusOK {
			return nil, fmt.Errorf("list repos: github returned %d", status)
		}
		if err != nil {
			return nil, fmt.Errorf("decode repo list: %w", err)
		}
		all = append(all, page_...)
		if len(page_) < 100 {
			break
		}
	}
	return all, nil
}

// adopt converts a pushed repository into a mirror of the same GitHub
// repository, in place and without re-downloading the objects it already has.
//
// The skip in Sync exists so the mirror lane never takes over something only
// this site holds, and that is still the rule. But a repository pushed here
// that also lives on GitHub is not that case: upstream already has everything
// it has, so mirroring it loses nothing and wins the description, topics,
// homepage and archived flag that the push lane has nowhere to read from.
//
// The test is containment rather than equality. Every local branch must exist
// upstream and point at a commit upstream already contains; being behind is
// fine, because the fetch fast-forwards it. Holding a commit upstream does not
// is the case this refuses, and such a repository stays a pushed one forever.
// Tags are held to exact equality, because a moved tag is a rewrite rather than
// a fast-forward and there is no ancestry to appeal to.
//
// The probe fetch writes into refs/adopt/* rather than to real refs, so a
// repository that fails the test is left byte for byte as it was found.
func (m *Mirror) adopt(ctx context.Context, gh GitHubRepo, repo Repo) (bool, error) {
	ctx, cancel := context.WithTimeout(ctx, syncTimeout)
	defer cancel()

	// A pushed repository has no origin, but removing one first makes this
	// safe to re-run after a probe that failed partway.
	_, _ = run(ctx, repo, "remote", "remove", "origin")
	if _, err := run(ctx, repo, "remote", "add", "origin", gh.CloneURL); err != nil {
		return false, err
	}

	// --no-tags matters: without it the fetch follows tags into refs/tags/*,
	// which are real refs, and the probe would no longer be read-only.
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
			// Exit status alone is the answer; --is-ancestor is a predicate.
			if err := gitCmd(ctx, repo,
				"merge-base", "--is-ancestor", sha, up).Run(); err != nil {
				slog.Warn("not adopting: local commits are not on upstream",
					slog.String("repo", gh.Name), slog.String("ref", name))
				ok = false
			}
		}
	}

	// The probe refs come out either way, so nothing here is left behind.
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
		// The same backup settings clone() gives a fresh mirror. A pushed
		// repository already has the reflog on, but not the expiry.
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
		// --prune is what makes the copy faithful rather than an
		// accumulation of every ref that ever existed. It is also what
		// propagates an upstream deletion, which is why every mirror is
		// created with a reflog and a never-expiring gc below: a
		// destructive upstream change stays recoverable here instead of
		// being mirrored into oblivion.
		// gitCmd with syncOne's own context rather than run(), because run()
		// imposes gitTimeout (20 seconds). That is right for a page render and
		// far too short for a fetch: the initial clone honoured the 10 minute
		// syncTimeout only because it calls exec.CommandContext directly, so
		// every incremental update of a repository larger than a few megabytes
		// failed on deadline and the mirror silently stopped tracking after
		// its first clone.
		cmd := gitCmd(ctx, repo, "remote", "update", "--prune")
		var stderr bytes.Buffer
		cmd.Stderr = &stderr
		if err := cmd.Run(); err != nil {
			return fmt.Errorf("remote update %s: %w: %s",
				gh.Name, err, strings.TrimSpace(stderr.String()))
		}
	}

	if err := m.db.MarkMirror(gh.Name, gh.CloneURL, gh.Archived); err != nil {
		return err
	}
	// Upstream owns the description and topics for a mirror; the UI does not
	// offer to edit them, so overwriting on each sync is correct.
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

	// The settings that make a mirror a backup rather than a copy of what
	// upstream says today. core.logAllRefUpdates is off by default in a bare
	// repository, and it is the one that matters: with it, an upstream force
	// push leaves the old tip in the reflog and recoverable.
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

// RunGC repacks every repository on its own schedule, which is what makes
// receive.autogc=false safe to set.
//
// A gc inside a push is work done while Cloudflare counts to 100, and a repack
// of a large repository can exceed that on its own. Here it costs nobody a
// request.
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
