package handler

import (
	"context"
	"encoding/json"
	"fmt"
	"log/slog"
	"net/http"
	"strconv"
	"strings"
	"time"

	"github.com/Paca-AI/api/internal/config"
	"github.com/Paca-AI/api/internal/platform/cache"
	"github.com/Paca-AI/api/internal/transport/http/presenter"
)

// VersionHandler reports the running build version and whether newer releases
// exist upstream (Paca-AI/paca) and on the fork. It powers the "new version
// available" banner on the home screen.
type VersionHandler struct {
	current      string
	upstreamRepo string
	forkRepo     string
	ttl          time.Duration
	httpClient   *http.Client
	cache        *cache.Store
	log          *slog.Logger
}

// NewVersionHandler builds a VersionHandler from the release config.
func NewVersionHandler(cfg config.ReleaseConfig, store *cache.Store, log *slog.Logger) *VersionHandler {
	return &VersionHandler{
		current:      cfg.Version,
		upstreamRepo: cfg.UpstreamRepo,
		forkRepo:     cfg.ForkRepo,
		ttl:          cfg.CacheTTL,
		httpClient:   &http.Client{Timeout: cfg.Timeout},
		cache:        store,
		log:          log,
	}
}

type releaseInfo struct {
	Repo      string `json:"repo"`
	Latest    string `json:"latest"`
	URL       string `json:"url"`
	HasUpdate bool   `json:"hasUpdate"`
}

type versionResponse struct {
	Current  string       `json:"current"`
	Upstream *releaseInfo `json:"upstream,omitempty"`
	Fork     *releaseInfo `json:"fork,omitempty"`
}

// Check returns the current version and, for each configured repo, its latest
// release plus whether it is newer than the running build.
func (h *VersionHandler) Check(w http.ResponseWriter, r *http.Request) {
	ctx := r.Context()
	resp := versionResponse{Current: h.current}

	if h.upstreamRepo != "" {
		resp.Upstream = h.checkRepo(ctx, h.upstreamRepo)
	}
	if h.forkRepo != "" {
		resp.Fork = h.checkRepo(ctx, h.forkRepo)
	}

	presenter.OK(w, r, resp)
}

// checkRepo returns release info for repo, or nil when the latest release
// cannot be determined (network error, rate limit, or no releases yet). A nil
// result simply means "no banner" — the check is best-effort and never fails
// the request.
func (h *VersionHandler) checkRepo(ctx context.Context, repo string) *releaseInfo {
	latest, url, err := h.latestRelease(ctx, repo)
	if err != nil {
		h.log.Warn("release check failed", "repo", repo, "error", err)
		return nil
	}
	if latest == "" {
		return nil
	}
	return &releaseInfo{
		Repo:      repo,
		Latest:    latest,
		URL:       url,
		HasUpdate: isNewer(latest, h.current),
	}
}

// githubRelease is the subset of the GitHub releases/latest payload we use;
// it doubles as the cached representation.
type githubRelease struct {
	TagName string `json:"tag_name"`
	HTMLURL string `json:"html_url"`
}

// latestRelease returns the newest release tag and its URL for repo, serving a
// cached value when available and otherwise querying the GitHub API and caching
// the result for h.ttl.
func (h *VersionHandler) latestRelease(ctx context.Context, repo string) (tag, url string, err error) {
	cacheKey := "release:" + repo

	if h.cache != nil {
		var cached githubRelease
		if hit, _ := h.cache.Get(ctx, cacheKey, &cached); hit {
			return cached.TagName, cached.HTMLURL, nil
		}
	}

	endpoint := "https://api.github.com/repos/" + repo + "/releases/latest"
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, endpoint, http.NoBody)
	if err != nil {
		return "", "", err
	}
	req.Header.Set("Accept", "application/vnd.github+json")
	req.Header.Set("User-Agent", "paca-release-check")

	res, err := h.httpClient.Do(req)
	if err != nil {
		return "", "", err
	}
	defer func() { _ = res.Body.Close() }()

	if res.StatusCode != http.StatusOK {
		return "", "", fmt.Errorf("github: unexpected status %d for %s", res.StatusCode, repo)
	}

	var payload githubRelease
	if err := json.NewDecoder(res.Body).Decode(&payload); err != nil {
		return "", "", err
	}

	if h.cache != nil && payload.TagName != "" {
		_ = h.cache.Set(ctx, cacheKey, payload, h.ttl)
	}

	return payload.TagName, payload.HTMLURL, nil
}

// releaseItem is one changelog entry returned to the web app.
type releaseItem struct {
	Tag         string `json:"tag"`
	Name        string `json:"name"`
	URL         string `json:"url"`
	PublishedAt string `json:"publishedAt"`
	Body        string `json:"body"`
	IsCurrent   bool   `json:"isCurrent"`
}

type releasesResponse struct {
	Current  string        `json:"current"`
	Repo     string        `json:"repo"`
	Releases []releaseItem `json:"releases"`
}

// githubReleaseFull is the subset of the GitHub releases-list payload we use.
type githubReleaseFull struct {
	TagName     string `json:"tag_name"`
	Name        string `json:"name"`
	HTMLURL     string `json:"html_url"`
	Body        string `json:"body"`
	PublishedAt string `json:"published_at"`
	Prerelease  bool   `json:"prerelease"`
	Draft       bool   `json:"draft"`
}

// ListReleases returns the recent stable releases of the upstream repo as a
// changelog, flagging the one that matches the running build. Best-effort: on
// any error it returns an empty list instead of failing the request.
func (h *VersionHandler) ListReleases(w http.ResponseWriter, r *http.Request) {
	ctx := r.Context()
	repo := h.upstreamRepo
	resp := releasesResponse{Current: h.current, Repo: repo, Releases: []releaseItem{}}
	if repo == "" {
		presenter.OK(w, r, resp)
		return
	}

	releases, err := h.fetchReleases(ctx, repo)
	if err != nil {
		h.log.Warn("release list failed", "repo", repo, "error", err)
		presenter.OK(w, r, resp)
		return
	}

	curCore, curOK := parseVersion(h.current)
	for _, gr := range releases {
		if gr.Draft || gr.Prerelease {
			continue
		}
		name := gr.Name
		if name == "" {
			name = gr.TagName
		}
		item := releaseItem{
			Tag:         gr.TagName,
			Name:        name,
			URL:         gr.HTMLURL,
			PublishedAt: gr.PublishedAt,
			Body:        gr.Body,
		}
		if curOK {
			if rc, ok := parseVersion(gr.TagName); ok && rc == curCore {
				item.IsCurrent = true
			}
		}
		resp.Releases = append(resp.Releases, item)
	}

	presenter.OK(w, r, resp)
}

// fetchReleases returns up to 20 recent releases for repo, cached for h.ttl.
func (h *VersionHandler) fetchReleases(ctx context.Context, repo string) ([]githubReleaseFull, error) {
	cacheKey := "releases:" + repo

	if h.cache != nil {
		var cached []githubReleaseFull
		if hit, _ := h.cache.Get(ctx, cacheKey, &cached); hit {
			return cached, nil
		}
	}

	endpoint := "https://api.github.com/repos/" + repo + "/releases?per_page=20"
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, endpoint, http.NoBody)
	if err != nil {
		return nil, err
	}
	req.Header.Set("Accept", "application/vnd.github+json")
	req.Header.Set("User-Agent", "paca-release-check")

	res, err := h.httpClient.Do(req)
	if err != nil {
		return nil, err
	}
	defer func() { _ = res.Body.Close() }()

	if res.StatusCode != http.StatusOK {
		return nil, fmt.Errorf("github: unexpected status %d for %s", res.StatusCode, repo)
	}

	var payload []githubReleaseFull
	if err := json.NewDecoder(res.Body).Decode(&payload); err != nil {
		return nil, err
	}

	if h.cache != nil {
		_ = h.cache.Set(ctx, cacheKey, payload, h.ttl)
	}

	return payload, nil
}

// isNewer reports whether latest is a strictly higher version than current
// using a lenient semver comparison: a leading "v" and any pre-release/build
// suffix (e.g. "-evup.1", "-rc.2") are ignored. Unparseable versions such as
// "dev" never trigger an update.
func isNewer(latest, current string) bool {
	lv, ok1 := parseVersion(latest)
	cv, ok2 := parseVersion(current)
	if !ok1 || !ok2 {
		return false
	}
	for i := 0; i < 3; i++ {
		if lv[i] != cv[i] {
			return lv[i] > cv[i]
		}
	}
	return false
}

// parseVersion extracts the major.minor.patch core from a tag like "v0.9.4" or
// "0.9.4-evup.1". Returns ok=false when there is no numeric core to compare.
func parseVersion(v string) ([3]int, bool) {
	var out [3]int
	v = strings.TrimPrefix(strings.TrimSpace(v), "v")
	if i := strings.IndexAny(v, "-+"); i >= 0 {
		v = v[:i]
	}
	if v == "" {
		return out, false
	}
	for i, part := range strings.Split(v, ".") {
		if i >= 3 {
			break
		}
		n, err := strconv.Atoi(part)
		if err != nil {
			return out, false
		}
		out[i] = n
	}
	return out, true
}
