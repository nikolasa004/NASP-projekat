package Memtable

import "math"

type Table interface {
	FindRecord(key string) *MemtableRecord
	AddRecord(record *MemtableRecord)
	Iterator() <-chan *MemtableRecord
	GetState() string
	SetState(state string)
}

type Memtable struct {
	implementType string
	numOfTables   int
	tableSize     int
	tables        []Table
	activeTable   int
}

func NewMemtable(implementType string, numOfTables int, tableSize int) *Memtable {
	memtable := &Memtable{
		implementType: implementType,
		numOfTables:   numOfTables,
		tableSize:     tableSize,
		tables:        make([]Table, numOfTables),
		activeTable:   0,
	}

	for i := 0; i < numOfTables; i++ {
		switch implementType {
		case "SkipList":
			memtable.tables[i] = NewSkipList(int(math.Log2(float64(tableSize))))
		case "Hashmap":
			memtable.tables[i] = NewHashmap()
		case "BTree":
			memtable.tables[i] = NewTree(5)
		}
	}
	memtable.tables[0].SetState("active")
	return memtable
}

func (table *Memtable) GetRecord(kljuc string) *MemtableRecord {
	for _,t := range table.tables {
		if t.GetState() != "empty" {
			record := t.FindRecord(kljuc)
			if record != nil {
				return record
			}
		}
	}
	return nil
}

func (table *Memtable) PutRecord(record *MemtableRecord) {
	return
}

func (table *Memtable) CreateRecord(tombstone bool,kljuc string, vrednost []byte) *MemtableRecord {
	return nil
}