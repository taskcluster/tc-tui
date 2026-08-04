package shell

import (
	"time"

	"github.com/taskcluster/tc-tui/resource"
)

// cacheKey identifies one (resource, scope, facet) list result. scope is ""
// for an unscoped List(), and facet is "" for anything other than a
// ServerFaceted FacetList/FacetCounts pair.
type cacheKey struct {
	resource string
	scope    string
	facet    string
}

func cacheKeyFor(res resource.Resource, scope, facetValue string) cacheKey {
	return cacheKey{resource: res.Name(), scope: scope, facet: facetValue}
}

// cacheEntry holds one cached list fetch. counts is only populated for
// ServerFaceted entries (FacetCounts is fetched paired with FacetList in
// loadList, so it rides along in the same entry rather than getting its own
// cache); it is nil otherwise. subtitle is similarly only populated for a
// ScopeSubtitle resource; "" otherwise. settledIDs records which of rows
// had actually FINISHED augmenting — their owning Augmentable.Augment batch
// reached its final tick and confirmed they weren't rejected by wanted — as
// of this snapshot (nil/empty right after the base fetch, before any
// Augment call). Deliberately NOT "requested as of this snapshot": a row
// can be marked requested the instant its batch is dispatched, well before
// it has any real data, so caching that broader set would let a
// still-in-flight row's placeholder get treated as settled forever if the
// user navigates away and back within the cache TTL before its batch
// finishes. A cache hit restores this into both Shell.augmentedRowIDs and
// Shell.settledRowIDs, so it resumes augmenting whatever hadn't actually
// finished yet instead of either re-requesting already-done rows or wrongly
// treating still-unfinished (or never-even-requested) ones as settled.
// truncated records whether rows was capped at the safe fetch limit with
// more left unfetched server-side (see resource.PartialLister) — restored on
// a cache hit so the "N+" count indicator survives navigating away and back, and
// checked by loadList so a capped snapshot can't satisfy a load once the
// user has asked for everything.
type cacheEntry struct {
	rows       []resource.Row
	counts     map[string]int
	subtitle   string
	fetchedAt  time.Time
	settledIDs map[string]bool
	truncated  bool
}

// listCache is a short-lived, session-lifetime cache of list/facet fetches,
// keyed by (resource, scope, facet). Entries are never evicted outright —
// staleness is checked entirely by TTL at read time in get(), and the
// working set of distinct keys in one TUI session is small enough that this
// is fine.
type listCache struct {
	entries map[cacheKey]cacheEntry
}

func newListCache() *listCache {
	return &listCache{entries: make(map[cacheKey]cacheEntry)}
}

// get returns the cached entry for key if one exists and is still within
// ttl. ttl <= 0 (a resource with auto-refresh disabled) always misses —
// mirroring RefreshInterval's existing meaning of "don't apply a freshness
// cadence to this resource" so the cache doesn't silently serve old data for
// it either.
func (c *listCache) get(key cacheKey, ttl time.Duration) (cacheEntry, bool) {
	if ttl <= 0 {
		return cacheEntry{}, false
	}

	entry, ok := c.entries[key]
	if !ok || time.Since(entry.fetchedAt) >= ttl {
		return cacheEntry{}, false
	}

	return entry, true
}

func (c *listCache) set(key cacheKey, entry cacheEntry) {
	c.entries[key] = entry
}

// invalidate drops every cached list entry for resourceName across all
// scopes and facets, forcing the next load of any of its views to re-fetch.
// Used after a successful mutation so a stale, pre-mutation row set isn't
// served from cache once the user navigates back to (or auto-refreshes) an
// affected list.
func (c *listCache) invalidate(resourceName string) {
	for key := range c.entries {
		if key.resource == resourceName {
			delete(c.entries, key)
		}
	}
}
