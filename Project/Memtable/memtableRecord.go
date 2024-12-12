package Memtable

type MemtableRecord struct {
	Key            string
	Value          []byte
	Timestamp      uint64
	Tombstone      bool
	LinkedSegments []int
}