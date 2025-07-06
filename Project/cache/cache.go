package cache

import (
	"container/list"
)

// Entry predstavlja jedan zapis u cache-u
type Entry struct {
	Key   string
	Value []byte
}

// LRUCache struktura
type LRUCache struct {
	capacity int
	cache    map[string]*list.Element
	list     *list.List
}

// Kreiraj novi LRU cache
func NewLRUCache(capacity int) *LRUCache {
	return &LRUCache{
		capacity: capacity,
		cache:    make(map[string]*list.Element),
		list:     list.New(),
	}
}

// Uzmi vrednost po kljucu (ako postoji)
func (c *LRUCache) GetFromCache(key string) ([]byte, bool) {
	if elem, ok := c.cache[key]; ok {
		c.list.MoveToFront(elem)
		return elem.Value.(*Entry).Value, true
	}
	return nil, false
}

// Ubaci ili ažuriraj podatak
func (c *LRUCache) PutInCache(key string, value []byte) {
	if elem, ok := c.cache[key]; ok {
		elem.Value.(*Entry).Value = value
		c.list.MoveToFront(elem)
	} else {
		if c.list.Len() >= c.capacity {
			back := c.list.Back()
			if back != nil {
				evicted := back.Value.(*Entry)
				delete(c.cache, evicted.Key)
				c.list.Remove(back)
			}
		}
		newElem := c.list.PushFront(&Entry{key, value})
		c.cache[key] = newElem
	}
}

// Izbaci podatak iz cache-a
func (c *LRUCache) RemoveFromCache(key string) {
	if elem, ok := c.cache[key]; ok {
		c.list.Remove(elem)
		delete(c.cache, key)
	}
}
