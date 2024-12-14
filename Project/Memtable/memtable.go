package Memtable

import "math"

type Table interface {
	FindRecord(key string) *MemtableRecord
	AddRecord(record *MemtableRecord)
	Iterator() <-chan *MemtableRecord
}

type Memtable struct {
	implementType string
	numOfTables   int
	tableSize     int
	tables        []Table
}

func NewMemtable(implementType string, numOfTables int, tableSize int) *Memtable {
	memtable := &Memtable{
		implementType: implementType,
		numOfTables:   numOfTables,
		tableSize:     tableSize,
		tables:        make([]Table, numOfTables),
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

	return memtable
}

func (table *Memtable) GetRecord(kljuc string) *MemtableRecord {
	return nil
}

func (table *Memtable) PutRecord(record *MemtableRecord) {
	return
}

func (table *Memtable) CreateRecord(tombstone bool,kljuc string, vrednost []byte) *MemtableRecord {
	return nil
}