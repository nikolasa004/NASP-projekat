package cache

import (
	"container/list"
	"fmt"
	"project/Memtable"
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

func (c *LRUCache) UpdateCache(flushedRecords []*Memtable.MemtableRecord) {
	for _, record := range flushedRecords {
		// Pogledaj da li taj ključ postoji u kešu
		cachedValue, found := c.GetFromCache(record.Key)
		if !found {
			// Ako ne postoji u kešu, ignoriši ovaj zapis
			continue
		}

		// Ako postoji, proveri da li je ista vrednost
		if string(cachedValue) == string(record.Value) {
			// Ako je ista, ništa ne radi
			continue
		}

		// Ako je različita vrednost:
		if record.Tombstone {
			// Ako je tombstone (brisanje), ukloni iz keša
			c.RemoveFromCache(record.Key)
			fmt.Println("[Cache] Removed record from cache:", record.Key)
		} else {
			// Inače ažuriraj vrednost u kešu
			c.RemoveFromCache(record.Key) // Ukloni staru vrednost ako postoji
			c.PutInCache(record.Key, record.Value)
			fmt.Println("[Cache] Updated value in cache for key:", record.Key)
		}
	}
}
