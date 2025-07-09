package Iterator

import (
	"project/Memtable"
)

type MemtableIterator struct {
	MemtableIndex      int
	currentRecordIndex int
	memtableSize       int
	isScanned          bool
	memtable 		*Memtable.Memtable
	isPrefixScan	  bool
	prefix string
	isRangeScan	  bool
	lowerBound string
	upperBound string
}

func NewMemtableIterator(memtableIndex int) *MemtableIterator {
	return &MemtableIterator{
		MemtableIndex:      memtableIndex,
		currentRecordIndex: 0,
	}
}

func (m *MemtableIterator) Next() {
	if m.currentRecordIndex == m.memtableSize-1 {
		m.isScanned = true
		return
	}
	m.currentRecordIndex++
}

func (m *MemtableIterator) GetValue() *ScanRecord {
	if m.isScanned {
		return nil
	}
	table := m.memtable.GetTable(m.MemtableIndex)
	iterationCounter := 0
	for record:= range table.Iterator() {
		if iterationCounter == m.currentRecordIndex {
			if m.isPrefixScan && checkPrefixMatch(m.prefix,record.Key) {
				return NewScanRecord(record.Key, record.Value, record.Tombstone)
			} else if m.isRangeScan && record.Key >= m.lowerBound && record.Key <= m.upperBound {
				return NewScanRecord(record.Key, record.Value, record.Tombstone)
			}
			return nil
		}
		iterationCounter++
	}
	return nil
}

func checkPrefixMatch(prefix string, key string) bool {
	if len(prefix) == 0 {
		return true
	}
	return len(key) >= len(prefix) && key[:len(prefix)] == prefix
}