package shell

import (
	"testing"
	"time"

	"github.com/taskcluster/tc-tui/resource"
)

func TestListCacheHitWithinTTL(t *testing.T) {
	c := newListCache()
	key := cacheKey{resource: "workerpools"}
	c.set(key, cacheEntry{
		rows:      []resource.Row{{ID: "a"}},
		fetchedAt: time.Now(),
	})

	entry, ok := c.get(key, time.Minute)

	if !ok {
		t.Fatalf("expected a cache hit within TTL")
	}
	if len(entry.rows) != 1 || entry.rows[0].ID != "a" {
		t.Fatalf("expected cached rows to round-trip, got %+v", entry.rows)
	}
}

func TestListCacheMissWhenNeverSet(t *testing.T) {
	c := newListCache()

	if _, ok := c.get(cacheKey{resource: "roles"}, time.Minute); ok {
		t.Fatalf("expected a miss for a key that was never set")
	}
}

func TestListCacheMissAfterTTLExpires(t *testing.T) {
	c := newListCache()
	key := cacheKey{resource: "roles"}
	c.set(key, cacheEntry{fetchedAt: time.Now().Add(-time.Minute)})

	if _, ok := c.get(key, time.Second); ok {
		t.Fatalf("expected a miss once the entry is older than the TTL")
	}
}

func TestListCacheAlwaysMissesForZeroOrNegativeTTL(t *testing.T) {
	c := newListCache()
	key := cacheKey{resource: "roles"}
	c.set(key, cacheEntry{fetchedAt: time.Now()})

	if _, ok := c.get(key, 0); ok {
		t.Fatalf("expected ttl=0 (auto-refresh disabled) to always miss")
	}
	if _, ok := c.get(key, -time.Second); ok {
		t.Fatalf("expected a negative ttl to always miss")
	}
}

func TestListCacheKeysAreDistinctByScopeAndFacet(t *testing.T) {
	c := newListCache()
	c.set(cacheKey{resource: "workers", scope: "pool-a", facet: "running"}, cacheEntry{
		rows:      []resource.Row{{ID: "a"}},
		fetchedAt: time.Now(),
	})

	if _, ok := c.get(cacheKey{resource: "workers", scope: "pool-a", facet: "stopped"}, time.Minute); ok {
		t.Fatalf("expected a different facet value to be a distinct cache miss")
	}
	if _, ok := c.get(cacheKey{resource: "workers", scope: "pool-b", facet: "running"}, time.Minute); ok {
		t.Fatalf("expected a different scope to be a distinct cache miss")
	}
	if _, ok := c.get(cacheKey{resource: "workers", scope: "pool-a", facet: "running"}, time.Minute); !ok {
		t.Fatalf("expected the exact matching key to be a hit")
	}
}

func TestCacheKeyForBuildsFromResourceNameScopeAndFacet(t *testing.T) {
	res := fakeResource{name: "workers"}

	got := cacheKeyFor(res, "pool-a", "running")

	want := cacheKey{resource: "workers", scope: "pool-a", facet: "running"}
	if got != want {
		t.Fatalf("expected %+v, got %+v", want, got)
	}
}

func TestListCacheInvalidateDropsAllEntriesForResource(t *testing.T) {
	c := newListCache()
	now := time.Now()
	c.set(cacheKey{resource: "secrets"}, cacheEntry{fetchedAt: now})
	c.set(cacheKey{resource: "secrets", scope: "proj"}, cacheEntry{fetchedAt: now})
	c.set(cacheKey{resource: "workers", scope: "pool-a"}, cacheEntry{fetchedAt: now})

	c.invalidate("secrets")

	if _, ok := c.get(cacheKey{resource: "secrets"}, time.Minute); ok {
		t.Fatalf("expected the unscoped secrets entry to be dropped")
	}
	if _, ok := c.get(cacheKey{resource: "secrets", scope: "proj"}, time.Minute); ok {
		t.Fatalf("expected the scoped secrets entry to be dropped")
	}
	if _, ok := c.get(cacheKey{resource: "workers", scope: "pool-a"}, time.Minute); !ok {
		t.Fatalf("expected an unrelated resource's entry to survive invalidation")
	}
}
