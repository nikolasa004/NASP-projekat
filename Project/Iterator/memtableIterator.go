package Iterator

import (
	"project/Memtable"
)

type MemtableIterator struct {
	MemtableIndex      int
	currentRecordIndex int
	isScanned          bool
	memtable           *Memtable.Memtable
	isPrefixScan       bool
	prefix             string
	isRangeScan        bool
	lowerBound         string
	upperBound         string
	pagesMap           map[int]int
}

func NewMemtableIterator(memtableIndex int, currentIndex int, memtable *Memtable.Memtable,
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
		pagesMap:           make(map[int]int),
	}
}

func (m *MemtableIterator) Next() {
	if m.currentRecordIndex == m.memtable.NumOfTables-1 {
		m.isScanned = true
		return
	}

	m.currentRecordIndex++
}

func (m *MemtableIterator) GetValue() *ScanRecord {
	if m.isScanned {
		return nil
	}
	realIndex := (m.memtable.GetActiveTable() - m.MemtableIndex + m.memtable.NumOfTables) % m.memtable.NumOfTables
	//ovaj indeks m.MemtableIndex je samo udaljenost od aktivne tabele, tako da moramo da ga prilagodimo
	table := m.memtable.GetTable(realIndex)
	iterationCounter := 0
	for record := range table.Iterator() {
		if iterationCounter == m.currentRecordIndex {
			if m.isPrefixScan && checkPrefixMatch(m.prefix, record.Key) {
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

func (m *MemtableIterator) ChangePage(targetPage int) {
	if m.currentRecordIndex != m.pagesMap[targetPage] {
		m.isScanned = false
	}

	m.currentRecordIndex = m.pagesMap[targetPage]
}

func (m *MemtableIterator) FinishPage() {
	m.pagesMap[len(m.pagesMap)] = m.currentRecordIndex // čuvanje trenutnog indeksa kao početka sledeće stranice
}

func checkPrefixMatch(prefix string, key string) bool {
	if len(prefix) == 0 {
		return true
	}
	return len(key) >= len(prefix) && key[:len(prefix)] == prefix
}

func GenerateIteratorsPrefix(memtable *Memtable.Memtable, prefix string) ([]InterfaceIterator, int) {
	var iterators []InterfaceIterator
	tableIndex := 0
	recordIndex := 0
	maxRecordIndex := -1

	for table := range memtable.Iterator() {
		for record := range table.Iterator() {
			if checkPrefixMatch(prefix, record.Key) {
				iterator := NewMemtableIterator(tableIndex, recordIndex, memtable, true, prefix, false, "", "")
				iterator.pagesMap[0] = recordIndex      // cuvanje indeksa prvog zapisa kao pocetka prve stranice
				iterators = append(iterators, iterator) // bez &, jer NewMemtableIterator već vraća *MemtableIterator
				if recordIndex > maxRecordIndex {
					maxRecordIndex = recordIndex // cuvamo maksimalni indeks zapisa koji odgovara prefiksu
				}
				break // izlazimo iz unutrasnje petlje jer smo nasli prvi record koji odgovara prefiksu
			}
			recordIndex++
		}
		recordIndex = 0 // resetujemo indeks zapisa za sledecu tabelu
		tableIndex++
	}
	return iterators, maxRecordIndex
}

func GenerateIteratorsRange(memtable *Memtable.Memtable, lowerBound string, upperBound string) ([]InterfaceIterator, int) {
	var iterators []InterfaceIterator
	tableIndex := 0
	recordIndex := 0
	maxRecordIndex := -1

	for table := range memtable.Iterator() {
		for record := range table.Iterator() {
			if record.Key >= lowerBound && record.Key <= upperBound {
				iterator := NewMemtableIterator(tableIndex, recordIndex, memtable, false, "", true, lowerBound, upperBound)
				iterator.pagesMap[0] = recordIndex      // cuvanje indeksa prvog zapisa kao pocetka prve stranice
				iterators = append(iterators, iterator) // bez &
				if recordIndex > maxRecordIndex {
					maxRecordIndex = recordIndex // cuvamo maksimalni indeks zapisa koji odgovara opsegu
				}
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
	return iterators, maxRecordIndex
}
