package Memtable

import (
	"encoding/binary"
	"fmt"
	"math"
	"os"
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
	GetLinkedSegments() []int // lista segmenata koji su povezani sa tabelom
	SetLinkedSegments(segments []int)
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
	reconstructRecs := blockManager.Reconstruct()
	var allFlushedRecords [][]*MemtableRecord
	for _, record := range reconstructRecs {
		rec := table.CreateRecord(record.Tombstone, record.Key, record.Value)
		flushedRecords, flushed := table.PutRecord(rec, blockManager, blockManager.GetRecordSegments()[record.Key])
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

func (table *Memtable) PutRecord(record *MemtableRecord, blockManager *BlockManager.BlockManager, currSegments []int) ([]*MemtableRecord, bool) {
	currTable := &table.tables[table.activeTable]
	currTableSegments := (*currTable).GetLinkedSegments()
	for _, currSegment := range currSegments {

		//provera da li je segment vec povezan sa tabelom
		contains := false
		for _, segment := range currTableSegments {
			if segment == currSegment {
				contains = true
				break
			}
		}
		if !contains {
			currTableSegments = append(currTableSegments, currSegment)
			(*currTable).SetLinkedSegments(currTableSegments)
		}
	}

	(*currTable).AddRecord(record)
	fmt.Println("MEM - record added :", record.Key)

	if (*currTable).GetLength() >= table.tableSize {
		(*currTable).SetState("full")
		table.activeTable = (table.activeTable + 1) % table.numOfTables
		(*currTable).SetLastBlockPosition(blockManager.GetCurrentBlock())
		fmt.Println("Prva korisna nakon flushovanja ove tabele:", blockManager.GetCurrentBlock())

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
	if len((*table).GetLinkedSegments()) > 1 {
		blockManager.DeleteSegments(len((*table).GetLinkedSegments()) - 1)
	}

	fmt.Println("MEM - FLUSHING TABLE")
	fmt.Println("MEM- brisem prvih ", len((*table).GetLinkedSegments())-1, " segmenata")
	//ovo je lista za SStable
	listOfRecords := make([]*MemtableRecord, 0)

	for record := range (*table).Iterator() {
		fmt.Println(record.Key + "-" + string(record.Value))
		listOfRecords = append(listOfRecords, record)
	}

	//sortiranje liste
	sort.Slice(listOfRecords, func(i, j int) bool {
		return listOfRecords[i].Key < listOfRecords[j].Key
	})

	(*table).Clear() //brisem sve iz tabele

	// menjam prvi korisni blok u segmentu
	segment := blockManager.GetSegments()[0]

	file, err := os.OpenFile(segment, os.O_WRONLY, 0644)

	if err != nil {
		panic(err)
	}

	defer file.Close()
	fmt.Println("MEM - prvi korisni blok: ", (*table).GetLastBlockPosition())
	file.Seek(12, 0)
	header := make([]byte, 4)
	binary.LittleEndian.PutUint32(header, uint32((*table).GetLastBlockPosition()))
	_, err = file.Write(header)
	if err != nil {
		panic(err)
	}

	return listOfRecords
}

func (table *Memtable) CreateRecord(tombstone bool, kljuc string, vrednost []byte) *MemtableRecord {
	timeStamp := uint64(time.Now().Unix())
	return NewMemtableRecord(kljuc, vrednost, timeStamp, tombstone)
}
