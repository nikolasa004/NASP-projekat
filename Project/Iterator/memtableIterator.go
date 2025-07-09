package Iterator

import (
	"project/Memtable"
)

type MemtableIterator struct {
	MemtableIndex      int
	currentRecordIndex int
	isScanned          bool
	memtable 		*Memtable.Memtable
	isPrefixScan	  bool
	prefix string
	isRangeScan	  bool
	lowerBound string
	upperBound string
}

func NewMemtableIterator(memtableIndex int,currentIndex int, memtable *Memtable.Memtable,
						 isPrefixScan bool, prefix string,
						 isRangeScan bool, lowerBound string, upperBound string) *MemtableIterator {
	return &MemtableIterator{
		MemtableIndex:      memtableIndex,
		currentRecordIndex: currentIndex,
		isScanned:          false,
		memtable:           memtable,
		isPrefixScan:       isPrefixScan,
		prefix:             prefix,
		isRangeScan:        isRangeScan,
		lowerBound:         lowerBound,
		upperBound:         upperBound,
	}
}

func (m *MemtableIterator) Next() {
	if m.currentRecordIndex == m.memtable.NumOfTables -1 {
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

func GenerateIteratorsPrefix(memtable *Memtable.Memtable, prefix string) []*MemtableIterator {
	var iterators []*MemtableIterator
	tableIndex := 0
	recordIndex := 0

	for table := range memtable.Iterator() {
		for record := range table.Iterator() {
			if checkPrefixMatch(prefix, record.Key) {
				iterators = append(iterators, NewMemtableIterator(tableIndex,recordIndex, memtable, true, prefix, false, "", ""))
				break // izlazimo iz unutrasnje petlje jer smo nasli prvi record koji odgovara prefiksu
			}
			recordIndex++
		}
		recordIndex = 0 // resetujemo indeks zapisa za sledecu tabelu
		tableIndex++
	}
	return iterators
}

func GenerateIteratorsRange(memtable *Memtable.Memtable, lowerBound string, upperBound string) []*MemtableIterator {
	var iterators []*MemtableIterator
	tableIndex := 0
	recordIndex := 0

	for table := range memtable.Iterator() {
		for record := range table.Iterator() {
			if record.Key >= lowerBound && record.Key <= upperBound {
				iterators = append(iterators, NewMemtableIterator(tableIndex,recordIndex, memtable, false, "", true, lowerBound, upperBound))
				break // izlazimo iz unutrasnje petlje jer smo nasli prvi record koji odgovara opsegu
			} else if record.Key > upperBound {
				// Ako je trenutni ključ veći od gornje granice, nema potrebe da nastavljamo dalje u ovoj tabeli
				break
			}
			recordIndex++
		}
		recordIndex = 0 // resetujemo indeks zapisa za sledecu tabelu
		tableIndex++
	}
	return iterators
}