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

func (record *MemtableRecord) GetKey() string {
	return record.Key
}
func (record *MemtableRecord) GetValue() []byte {
	return record.Value
}
func (record *MemtableRecord) GetTimestamp() uint64 {
	return record.Timestamp
}
func (record *MemtableRecord) IsTombstone() bool {
	return record.Tombstone
}
func (record *MemtableRecord) SetTombstone(tombstone bool) {
	record.Tombstone = tombstone
}
func (record *MemtableRecord) SetValue(value []byte) {
	record.Value = value
}
func (record *MemtableRecord) SetKey(key string) {
	record.Key = key
}
func (record *MemtableRecord) SetTimestamp(timestamp uint64) {
	record.Timestamp = timestamp
}