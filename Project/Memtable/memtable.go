package Memtable

import (
	"encoding/binary"
	"fmt"
	"math"
	"os"
	"time"

	"project/BlockManager"
)

//interfejs za tabelu
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

func (table *Memtable) GetRecord(kljuc string) *MemtableRecord {
	currTable := table.activeTable
	for i := 0; i < table.numOfTables; i++ {
		if table.tables[currTable].GetState() != "empty" {
			record := table.tables[currTable].FindRecord(kljuc)
			if record != nil && !record.Tombstone {
				return record
			}
		}
		currTable = (currTable - 1 + table.numOfTables) % table.numOfTables
	}
	
	return nil
}

func (table *Memtable) PutRecord(record *MemtableRecord, blockManager *BlockManager.BlockManager, currSegments []int) {
	currTable := &table.tables[table.activeTable]
	currTableSegments := (*currTable).GetLinkedSegments()
	for _, currSegment := range currSegments {
		fmt.Println("Current segment: ", currSegment)

		//provera da li je segment vec povezan sa tabelom
		contains := false
		for _,segment := range currTableSegments {
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

	if (*currTable).GetLength() >= table.tableSize {
		(*currTable).SetState("full")
		table.activeTable = (table.activeTable + 1) % table.numOfTables
		(*currTable).SetLastBlockPosition(blockManager.GetCurrentBlock())
		
		currTable = &table.tables[table.activeTable]
		if (*currTable).GetState() == "empty" {
			(*currTable).SetState("active")
			return
		}
		table.flushTable(currTable, blockManager)
		(*currTable).SetState("active")
	}
}

func (t *Memtable) flushTable(table *Table, blockManager *BlockManager.BlockManager) {
	if len((*table).GetLinkedSegments()) > 1 {
		blockManager.DeleteSegments(len((*table).GetLinkedSegments()) - 1)
	}

	//ovo je lista za Bjelicu za SStable
	// listOfRecords := make([]*MemtableRecord, 0)
	fmt.Println("FLUSHING TABLE")
	for record := range (*table).Iterator() {
		fmt.Println(record.Key + "-" + string(record.Value))
		// listOfRecords = append(listOfRecords, record)
	}

	(*table).Clear() //brisem sve iz tabele

	// menjam prvi korisni blok u segmentu
	segment := blockManager.GetSegments()[0]

	file, err := os.OpenFile(segment, os.O_WRONLY, 0644)
	
	if err != nil {
		panic(err)
	}

	defer file.Close()

	file.Seek(12, 0)
	header := make([]byte, 4)
	binary.LittleEndian.PutUint32(header, uint32((*table).GetLastBlockPosition()))
	_, err = file.Write(header)
	if err != nil {
		panic(err)
	}
}

func (table *Memtable) CreateRecord(tombstone bool,kljuc string, vrednost []byte) *MemtableRecord {
	timeStamp := uint64(time.Now().Unix())
	return NewMemtableRecord(kljuc,vrednost,timeStamp,tombstone)
}