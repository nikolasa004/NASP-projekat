package Iterator

type ScanRecord struct {
	Key       string
	Value     []byte
	Tombstone bool
}

func NewScanRecord(key string, value []byte, tombstone bool) *ScanRecord {
	return &ScanRecord{
		Key:       key,
		Value:     value,
		Tombstone: tombstone,
	}
}

type InterfaceIterator interface {
	Next()
	GetValue() ScanRecord
}