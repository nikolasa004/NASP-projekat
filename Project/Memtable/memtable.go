package Memtable

import (
	"fmt"
	"math"
	"project/BlockManager"

	// "project/sstable"
	"sort"
	"time"
)

// interfejs za tabelu
type Table interface {
	FindRecord(key string) *MemtableRecord
	AddRecord(record *MemtableRecord)
	Iterator() <-chan *MemtableRecord
	GetState() string // empty, active, full
	SetState(state string)
	// GetLinkedSegments() []int // lista segmenata koji su povezani sa tabelom
	// SetLinkedSegments(segments []int)
	GetLinkedSegment() int // oznaka poslednjeg segmenta wall-a u koji je upisan neki zapis koji je zabelezen u ovoj tabeli
	SetLinkedSegment(segment int)
	GetLength() int
	GetLastBlockPosition() int // poslednji korisni blok u segmentu
	SetLastBlockPosition(position int)
	Clear()
}

type Memtable struct {
	implementType string
	numOfTables   int
	tableSize     int
	tables        []Table
	activeTable   int // indeks aktivne tabele
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

func (table *Memtable) Reconstruct(blockManager *BlockManager.BlockManager) ([][]*MemtableRecord, bool) {
	reconstructRecs, recordSegments := blockManager.Reconstruct()
	var allFlushedRecords [][]*MemtableRecord
	for _, record := range reconstructRecs {
		rec := table.CreateRecord(record.Tombstone, record.Key, record.Value)
		flushedRecords, flushed := table.PutRecord(rec, blockManager, recordSegments[record.Key])
		if flushed {
			allFlushedRecords = append(allFlushedRecords, flushedRecords)
		}
	}
	return allFlushedRecords, len(allFlushedRecords) > 0
}

func (table *Memtable) GetRecord(kljuc string) (*MemtableRecord, bool) {
	currTable := table.activeTable
	for i := 0; i < table.numOfTables; i++ {
		if table.tables[currTable].GetState() != "empty" {
			record := table.tables[currTable].FindRecord(kljuc)
			if record != nil && !record.Tombstone {
				return record, true
			}
		}
		currTable = (currTable - 1 + table.numOfTables) % table.numOfTables
	}

	return nil, false
}

func (table *Memtable) PutRecord(record *MemtableRecord, blockManager *BlockManager.BlockManager, currSegment int) ([]*MemtableRecord, bool) {
	currTable := &table.tables[table.activeTable]
	(*currTable).SetLinkedSegment(currSegment)
	// for _, currSegment := range currSegments {

	// 	//provera da li je segment vec povezan sa tabelom
	// 	// contains := false
	// 	// for _, segment := range currTableSegments {
	// 	// 	if segment == currSegment {
	// 	// 		contains = true
	// 	// 		break
	// 	// 	}
	// 	// }
	// 	// if !contains {
	// 	// 	currTableSegments = append(currTableSegments, currSegment)
	// 	// 	(*currTable).SetLinkedSegments(currTableSegments)
	// 	// }

	// }


	//dodaj da radis pretragu da vidis da li vec postoji u nekoj tabeli i ako postoji izmenis ga
	// najvrv cu onda morati jos neke argumente da saljem u ovu funkciju a ne da odmah vani pravim novi rekord
	(*currTable).AddRecord(record)
	fmt.Println("MEM - record added :", record.Key)

	if (*currTable).GetLength() >= table.tableSize {
		(*currTable).SetState("full")
		table.activeTable = (table.activeTable + 1) % table.numOfTables
		(*currTable).SetLastBlockPosition(blockManager.GetCurrentBlock())
		fmt.Println("Prva korisna nakon flushovanja ove tabele:segment-",currSegment,",blok-", blockManager.GetCurrentBlock())

		currTable = &table.tables[table.activeTable]
		if (*currTable).GetState() == "empty" {
			(*currTable).SetState("active")
			return nil, false
		}
		records := table.flushTable(currTable, blockManager)
		(*currTable).SetState("active")
		return records, true
	}
	return nil, false
}

func (t *Memtable) flushTable(table *Table, blockManager *BlockManager.BlockManager) []*MemtableRecord {
	// if len((*table).GetLinkedSegments()) > 1 {
	// 	blockManager.DeleteSegments(len((*table).GetLinkedSegments()) - 1)
	// }
	lowWatermark := (*table).GetLinkedSegment()
	blockManager.DeleteSegments(lowWatermark)

	fmt.Println("MEM - FLUSHING TABLE")
	fmt.Println("MEM- brisem ", (*table).GetLinkedSegment(), " segmenata")
	//ovo je lista za SStable
	listOfRecords := make([]*MemtableRecord, 0)

	for record := range (*table).Iterator() {
		listOfRecords = append(listOfRecords, record)
	}

	//sortiranje liste
	sort.Slice(listOfRecords, func(i, j int) bool {
		return listOfRecords[i].Key < listOfRecords[j].Key
	})

	// pomeram segmente ostalih tabela
	for i:= 0; i < t.numOfTables; i++ {
		t.tables[i].SetLinkedSegment(t.tables[i].GetLinkedSegment() - lowWatermark)
	}
	
	blockManager.UpdateWalStart((*table).GetLastBlockPosition())

	(*table).Clear() //brisem sve iz tabele


	return listOfRecords
}

func (table *Memtable) CreateRecord(tombstone bool, kljuc string, vrednost []byte) *MemtableRecord {
	timeStamp := uint64(time.Now().Unix())
	return NewMemtableRecord(kljuc, vrednost, timeStamp, tombstone)
}
