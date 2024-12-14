package Memtable

import (
	"sort"
)

type Hashmap struct {
	records map[string]*MemtableRecord
	state string
}

func (m *Hashmap) SetState(state string) {
	m.state = state
}

func (m *Hashmap) GetState() string {
	return m.state
}

func NewHashmap() *Hashmap {
	return &Hashmap{records: make(map[string]*MemtableRecord), state: "empty"}
}

func (m *Hashmap) FindRecord(key string) *MemtableRecord {
	return m.records[key]
}

func (m *Hashmap) AddRecord(record *MemtableRecord) {
	m.records[record.Key] = record
}


func (m *Hashmap) Iterator() <-chan *MemtableRecord {
	ch := make(chan *MemtableRecord)
	go func() {

		keys := make([]string, 0, len(m.records))

		for k := range m.records {
			keys = append(keys, k)
		}

		sort.Strings(keys)

		for _, k := range keys {
			ch <- m.records[k]
		}
		close(ch)
	}()
	return ch
}