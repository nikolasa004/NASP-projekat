package Scanners

import (
	"container/list"
	"fmt"
	"project/Iterator"
)

// LRUCache za keširanje stranica
type LRUCache struct {
	capacity int
	cache    map[int]*list.Element
	order    *list.List // list.Element.Value == (int, []ScanRecord)
}

type cacheEntry struct {
	page   int
	values []Iterator.ScanRecord
}

func NewLRUCache(cap int) *LRUCache {
	return &LRUCache{
		capacity: cap,
		cache:    make(map[int]*list.Element),
		order:    list.New(),
	}
}

func (c *LRUCache) Get(page int) ([]Iterator.ScanRecord, bool) {
	if elem, ok := c.cache[page]; ok {
		c.order.MoveToFront(elem)
		return elem.Value.(cacheEntry).values, true
	}
	return nil, false
}

func (c *LRUCache) Put(page int, values []Iterator.ScanRecord) {
	if elem, ok := c.cache[page]; ok {
		elem.Value = cacheEntry{page: page, values: values}
		c.order.MoveToFront(elem)
		return
	}

	if c.order.Len() >= c.capacity {
		last := c.order.Back()
		if last != nil {
			entry := last.Value.(cacheEntry)
			delete(c.cache, entry.page)
			c.order.Remove(last)
		}
	}

	elem := c.order.PushFront(cacheEntry{page: page, values: values})
	c.cache[page] = elem
}
func (c *LRUCache) PrintCachePages() {
	fmt.Print("Cache pages: ")
	for e := c.order.Front(); e != nil; e = e.Next() {
		entry := e.Value.(cacheEntry)
		fmt.Printf("%d ", entry.page)
	}
	fmt.Println()
}
