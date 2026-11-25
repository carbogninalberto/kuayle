package service

import (
	"container/list"
	"sync"
	"time"
)

// The git trees a task's file listings read (KUA-319) are cached by tree
// SHA. A tree's SHA names its content, so an entry never goes stale: a
// directory that did not change keeps its SHA across commits, and every task
// of the workspace's repository that lists it again reads it from memory.
// The cache holds no authority: a request is authorised by the task before
// anything is looked up, and keys carry the workspace and repository so one
// workspace's entries are never served to another.
//
// Memory is bounded three ways: entries least recently used are dropped
// beyond treeCacheItems entries or treeCacheWeight listed names in total, and
// an entry not read for treeCacheTTL is dropped, since a directory nobody
// lists any more should not hold memory.
const (
	// treeCacheItems bounds the cached trees, treeCacheWeight their entries
	// together (about 150 bytes each: 200,000 entries are roughly 30 MB).
	treeCacheItems  = 2000
	treeCacheWeight = 200_000
	// treeCacheTTL is how long an unread entry stays.
	treeCacheTTL = time.Hour
)

type treeCacheLimits struct {
	items, weight int
	ttl           time.Duration
}

var defaultTreeCacheLimits = treeCacheLimits{items: treeCacheItems, weight: treeCacheWeight, ttl: treeCacheTTL}

// treeCache is a least-recently-used cache of immutable values by key,
// bounded by item count, total weight and idle time.
type treeCache[V any] struct {
	limits treeCacheLimits
	now    func() time.Time

	mu     sync.Mutex
	items  map[string]*list.Element
	order  *list.List // front: most recently used
	weight int
}

type treeCacheItem[V any] struct {
	key    string
	value  V
	weight int
	used   time.Time
}

func newTreeCache[V any](limits treeCacheLimits) *treeCache[V] {
	return &treeCache[V]{limits: limits, now: time.Now, items: map[string]*list.Element{}, order: list.New()}
}

// get returns the value of key and counts it as used now.
func (c *treeCache[V]) get(key string) (V, bool) {
	c.mu.Lock()
	defer c.mu.Unlock()
	c.expire()
	element, ok := c.items[key]
	if !ok {
		var zero V
		return zero, false
	}
	item := element.Value.(*treeCacheItem[V])
	item.used = c.now()
	c.order.MoveToFront(element)
	return item.value, true
}

// put stores value under key with its weight (at least 1). A value heavier
// than the whole cache is not kept.
func (c *treeCache[V]) put(key string, value V, weight int) {
	weight = max(weight, 1)
	if weight > c.limits.weight {
		return
	}
	c.mu.Lock()
	defer c.mu.Unlock()
	if element, ok := c.items[key]; ok {
		c.remove(element)
	}
	c.items[key] = c.order.PushFront(&treeCacheItem[V]{key: key, value: value, weight: weight, used: c.now()})
	c.weight += weight
	c.expire()
	for c.order.Len() > c.limits.items || c.weight > c.limits.weight {
		c.remove(c.order.Back())
	}
}

// size reports the entries held and their total weight.
func (c *treeCache[V]) size() (items, weight int) {
	c.mu.Lock()
	defer c.mu.Unlock()
	c.expire()
	return c.order.Len(), c.weight
}

// expire drops the idle entries; they sit at the back, least recently used.
func (c *treeCache[V]) expire() {
	now := c.now()
	for back := c.order.Back(); back != nil; back = c.order.Back() {
		if now.Sub(back.Value.(*treeCacheItem[V]).used) < c.limits.ttl {
			return
		}
		c.remove(back)
	}
}

func (c *treeCache[V]) remove(element *list.Element) {
	item := c.order.Remove(element).(*treeCacheItem[V])
	delete(c.items, item.key)
	c.weight -= item.weight
}
