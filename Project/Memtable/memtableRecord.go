package Memtable

type MemtableRecord struct {
	Key       string
	Value     []byte
	Timestamp uint64
	Tombstone bool
}

func NewMemtableRecord(key string, value []byte, timestamp uint64, tombstone bool) *MemtableRecord {
	return &MemtableRecord{
		Key:       key,
		Value:     value,
		Timestamp: timestamp,
		Tombstone: tombstone,
	}
}