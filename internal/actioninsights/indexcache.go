package actioninsights

import (
	"container/list"
	"sync"
)

// IndexCache keeps the spawn indexes of execution log summaries, which do
// not change once ingested, up to a total size, dropping the least
// recently used.
type IndexCache struct {
	mu      sync.Mutex
	budget  int64
	size    int64
	entries map[int64]*list.Element
	lru     *list.List
}

type indexCacheEntry struct {
	summaryID int64
	index     SpawnIndex
}

// NewIndexCache returns an IndexCache holding up to budget bytes.
func NewIndexCache(budget int64) *IndexCache {
	return &IndexCache{budget: budget, entries: map[int64]*list.Element{}, lru: list.New()}
}

// Get returns the index of a summary, if cached.
func (c *IndexCache) Get(summaryID int64) (SpawnIndex, bool) {
	c.mu.Lock()
	defer c.mu.Unlock()
	e, ok := c.entries[summaryID]
	if !ok {
		return nil, false
	}
	c.lru.MoveToFront(e)
	return e.Value.(*indexCacheEntry).index, true
}

// Put caches the index of a summary.
func (c *IndexCache) Put(summaryID int64, index SpawnIndex) {
	size := int64(len(index))
	if size > c.budget {
		return
	}
	c.mu.Lock()
	defer c.mu.Unlock()
	if _, ok := c.entries[summaryID]; ok {
		return
	}
	c.entries[summaryID] = c.lru.PushFront(&indexCacheEntry{summaryID: summaryID, index: index})
	c.size += size
	for c.size > c.budget {
		e := c.lru.Back()
		entry := e.Value.(*indexCacheEntry)
		c.lru.Remove(e)
		delete(c.entries, entry.summaryID)
		c.size -= int64(len(entry.index))
	}
}

// Len returns the number of cached indexes.
func (c *IndexCache) Len() int {
	c.mu.Lock()
	defer c.mu.Unlock()
	return len(c.entries)
}
