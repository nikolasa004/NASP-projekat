package Memtable

import (
	"sort"
)

type Hashmap struct {
	records map[string]*MemtableRecord
	state string
	linkedSegment int
	lastBlockPosition int
}

func (m *Hashmap) GetLastBlockPosition() int {
	return m.lastBlockPosition
}

func (m *Hashmap) SetLastBlockPosition(position int) {
	m.lastBlockPosition = position
}

func (m *Hashmap) Clear() {
	m.records = make(map[string]*MemtableRecord)
	m.state = "empty"
	m.linkedSegment = 0
	m.lastBlockPosition = 0
}


func (m *Hashmap) GetLength() int {
	return len(m.records)
}

func (m *Hashmap) GetLinkedSegment() int {
	return m.linkedSegment
}

func (m *Hashmap) SetLinkedSegment(segment int) {
	m.linkedSegment = segment
}

func (m *Hashmap) SetState(state string) {
	m.state = state
}

func (m *Hashmap) GetState() string {
	return m.state
}

func NewHashmap() *Hashmap {
	return &Hashmap{records: make(map[string]*MemtableRecord), state: "empty", linkedSegment: 0, lastBlockPosition: 0}
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