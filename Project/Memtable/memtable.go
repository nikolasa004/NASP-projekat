package Memtable

import (
	"fmt"
	"math"
	"project/BlockManager"
	"time"
)

//napravi enumeraciju za implementation type
const (
	SkipListType = "SkipList"
	HashmapType  = "Hashmap"
	BTreeType    = "BTree"
)

// interfejs za tabelu
type Table interface {
	FindRecord(key string) *MemtableRecord
	AddRecord(record *MemtableRecord)
	Iterator() <-chan *MemtableRecord
	GetSortedRecords() []*MemtableRecord // vraca sve vrednosti u tabeli
	GetState() string // stanja tabele - empty, full, active
	SetState(state string)
	GetLinkedSegment() int // oznaka poslednjeg segmenta wall-a u koji je upisan neki zapis koji je zabelezen u ovoj tabeli
	SetLinkedSegment(segment int)
	GetLength() int
	GetLastBlockPosition() int // poslednji korisni blok u aktuelnom segmentu ove tabele
	SetLastBlockPosition(position int)
	Clear()
}

type Memtable struct {
	implementType string // SkipList, Hashmap ili B-stablo
	NumOfTables   int
	tableSize     int
	tables        []Table
	activeTable   int // indeks aktivne tabele
}

func (table *Memtable) GetType() string {
	return table.implementType
}

func (table *Memtable) GetActiveTable() int {
	return table.activeTable
}

func NewMemtable(implementType string, NumOfTables int, tableSize int) *Memtable {

	memtable := &Memtable{
		implementType: implementType,
		NumOfTables:   NumOfTables,
		tableSize:     tableSize,
		tables:        make([]Table, NumOfTables),
		activeTable:   0,
	}

	for i := 0; i < NumOfTables; i++ {

		switch implementType {
		case SkipListType:
			memtable.tables[i] = NewSkipList(int(math.Log2(float64(tableSize))))
		case HashmapType:
			memtable.tables[i] = NewHashmap()
		case BTreeType:
			memtable.tables[i] = NewTree(5)
		}

	}
	memtable.tables[0].SetState("active")

	return memtable
}

func (table *Memtable) Reconstruct(blockManager *BlockManager.BlockManager) ([][]*MemtableRecord, bool) { // rekonstrukcija tabela zapisima iz Wal-a nakon pokretanja engine-a
	reconstructRecs, recordSegments := blockManager.Reconstruct() // vraca rekonstruisane zapise i mapu koja mapira te zapise na njihove segmente

	// dodavanje rekonstruisanih zapisa
	// moze da dodje do flush-a ako je promenjena konfiguracija
	var allFlushedRecords [][]*MemtableRecord //skladisti sve flushovane zapise(ako bude vise flush-ova)
	for _, record := range reconstructRecs {
		flushedRecords, flushed := table.PutRecord(record.Tombstone, record.Key, string(record.Value), blockManager, recordSegments[record.Key])
		if flushed {
			allFlushedRecords = append(allFlushedRecords, flushedRecords)
		}
	}

	return allFlushedRecords, len(allFlushedRecords) > 0
}

func (table *Memtable) GetRecord(kljuc string) (*MemtableRecord, bool) {
	currTable := table.activeTable
	for i := 0; i < table.NumOfTables; i++ {
		if table.tables[currTable].GetState() != "empty" {
			record := table.tables[currTable].FindRecord(kljuc)

			if record != nil && !record.IsTombstone() {
				return record, true
			} else if record != nil && record.IsTombstone() { // unisten zapis
				return record, false
			}
		}
		currTable = (currTable - 1 + table.NumOfTables) % table.NumOfTables
	}

	return nil, false
}

func (table *Memtable) PutRecord(tombstone bool, key string, value string, blockManager *BlockManager.BlockManager, currSegment int) ([]*MemtableRecord, bool) {
	currTable := &table.tables[table.activeTable]
	(*currTable).SetLinkedSegment(currSegment) // poslednji zapis u tabeli ce uvek biti skladisten u najnoviji segment

	foundRecord, found := table.GetRecord(key)
	if found { // ako postoji zapis dolazi do azuriranja
		newTimeStamp := uint64(time.Now().Unix())
		foundRecord.SetTimestamp(newTimeStamp)
		foundRecord.SetValue([]byte(value))
		foundRecord.SetTombstone(tombstone)
		fmt.Println("MEM - record updated :", foundRecord.Key)
		return nil, false
	}

	record := table.createRecord(tombstone, key, []byte(value)) // kreira se novi zapis
	(*currTable).AddRecord(record)
	fmt.Println("MEM - record added :", record.GetKey())

	if (*currTable).GetLength() >= table.tableSize { // trenutna tabela je napunjena
		(*currTable).SetState("full")
		(*currTable).SetLastBlockPosition(blockManager.GetCurrentBlock()) // cuvamo poslednji blok vezan za ovu tabelu
		table.activeTable = (table.activeTable + 1) % table.NumOfTables   // rotiramo tabele
		fmt.Println("Prva korisna nakon flushovanja ove tabele:segment-", currSegment, ",blok-", blockManager.GetCurrentBlock())

		currTable = &table.tables[table.activeTable]
		if (*currTable).GetState() == "empty" { // ako je nova tabela prazna postaje aktivna
			(*currTable).SetState("active")
			return nil, false
		}
		records := table.flushTable(currTable, blockManager) // puna tabela pa dolazi do flush-a na SStable
		(*currTable).SetState("active")
		return records, true
	}
	return nil, false
}

func (t *Memtable) flushTable(table *Table, blockManager *BlockManager.BlockManager) []*MemtableRecord { // Prazni se tabela u SStable
	lowWatermark := (*table).GetLinkedSegment()
	blockManager.DeleteSegments(lowWatermark) // brisemo sve segmente do poslednjeg povezanog za ovu tabelu

	fmt.Println("MEM - FLUSHING TABLE")
	fmt.Println("MEM- brisem ", (*table).GetLinkedSegment(), " segmenata")

	listOfRecords := (*table).GetSortedRecords()

	// azuriram segmente ostalih tabela
	for i := 0; i < t.NumOfTables; i++ {
		t.tables[i].SetLinkedSegment(t.tables[i].GetLinkedSegment() - lowWatermark)
	}

	blockManager.UpdateWalStart((*table).GetLastBlockPosition()) // postavljam prvi koristan blok za novi prvi segment

	(*table).Clear()
	return listOfRecords
}

func (table *Memtable) createRecord(tombstone bool, kljuc string, vrednost []byte) *MemtableRecord {
	timeStamp := uint64(time.Now().Unix())
	return NewMemtableRecord(kljuc, vrednost, timeStamp, tombstone)
}

func (table *Memtable) Iterator() <-chan Table {
	ch := make(chan Table)

	go func() {
		for i := 0; i < table.NumOfTables; i++ {
			currTable := (table.activeTable - i + table.NumOfTables) % table.NumOfTables
			ch <- table.tables[currTable]
		}
		close(ch)
	}()

	return ch
}

func (table *Memtable) GetTable(index int) Table {
	return table.tables[index]
}
