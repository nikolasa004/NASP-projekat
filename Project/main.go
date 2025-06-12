package main

import (
	"bufio"
	"fmt"
	"os"
	"project/BlockManager"
	"project/Memtable"
	"project/config"
	"project/sstable"
)

func main() {
	// bira se neka od metoda GET, PUT, DELETE
	// od svake operacije se pravi zapis

	config, err := config.ReadConfig("config/config.json")

	//WAL
	blockSize := 1024
	blocksPerSegment := 10
	cacheSize := 10

	//Memtable
	implementType := "SkipList"
	numOfTables := 10
	tableSize := 10

	//SSTable
	summarySampleRate := 10
	oneFile := true
	// compression := false

	if err != nil {
		fmt.Println("Greska pri citanju konfiguracionog fajla")
	} else {
		blockSize = config.BlockManager.BlockSize
		blocksPerSegment = config.BlockManager.BlocksInSegment
		cacheSize = config.BlockManager.CacheSize
		implementType = config.Memtable.ImplementType
		numOfTables = config.Memtable.NumOfTables
		tableSize = config.Memtable.TableSize
		summarySampleRate = config.SSTableManager.SummarySampleRate
		oneFile = config.SSTableManager.OneFile
		// compression = config.SSTableManager.Compresion
	}

	walPath := "BlockManager/wal"
	blockManager := BlockManager.NewBlockManager(blockSize, blocksPerSegment, cacheSize, walPath)

	memtable := Memtable.NewMemtable(implementType, numOfTables, tableSize)
	flushedReconstructRecords, flushed := memtable.Reconstruct(blockManager) // moguce ako se promeni konfiguracija memtable-a
	if flushed {
		for _, recs := range flushedReconstructRecords {
	// ovde bjelica treba da napravi funkciju u koju ce da spakuje ovu logiku
			sstableRecords := make([]sstable.DataRecord, len(recs))
			//sada treba da se napravi sstable
			for i, record := range recs {
				rec := sstable.NewRecord(record.Key, record.Value, record.Tombstone, record.Timestamp)
				sstableRecords[i] = rec
			}
			sstable.WriteSstable(sstableRecords, summarySampleRate, false, oneFile)
		}
	}
	scanner := bufio.NewScanner(os.Stdin)
	for {
		fmt.Println("Unesite operaciju ili / za kraj: ")
		var operacija string
		scanner.Scan()
		operacija = scanner.Text()
		if operacija == "GET" {
			fmt.Println("Unesite kljuc: ")
			scanner.Scan()
			kljuc := scanner.Text()
			record, found := memtable.GetRecord(kljuc)
			if found {
				fmt.Println(string(record.Value))
				continue
			}
			SSrecord, found := sstable.FindInSstables(kljuc)
			if found {
				fmt.Println(string(SSrecord.Value))
				continue
			}
			fmt.Println("Nema takvog kljuca")
		} else if operacija == "PUT" {
			fmt.Println("Unesite kljuc: ")
			scanner.Scan()
			kljuc := scanner.Text()
			fmt.Println("Unesite vrednost: ")
			scanner.Scan()
			vrednost := scanner.Text()
			records := BlockManager.CreateBinaryRecordsWithFragmentation(false, kljuc, []byte(vrednost), blockSize)
			for _, record := range records {
				blockManager.PutRecord(record)
			}
			record := memtable.CreateRecord(false, kljuc, []byte(vrednost))
			fmt.Println("SS - One file: ", oneFile)
			flushedRecords,flushed := memtable.PutRecord(record, blockManager, []int{blockManager.GetCurrentSegment()})
			if flushed {
				// ovde bjelica treba da napravi funkciju u koju ce da spakuje ovu logiku
				sstableRecords := make([]sstable.DataRecord, len(flushedRecords))
				//sada treba da se napravi sstable
				for i, record := range flushedRecords {
					rec := sstable.NewRecord(record.Key, record.Value, record.Tombstone, record.Timestamp)
					sstableRecords[i] = rec
				}
				sstable.WriteSstable(sstableRecords, summarySampleRate, false, oneFile)
			}
		} else if operacija == "DELETE" {
			fmt.Println("Unesite kljuc: ")
			scanner.Scan()
			kljuc := scanner.Text()

			//napraviti zapis sa tombstone = true
			records := BlockManager.CreateBinaryRecordsWithFragmentation(true, kljuc, []byte(""), blockSize)
			for _, record := range records {
				blockManager.PutRecord(record)
			}
			record := memtable.CreateRecord(true, kljuc, []byte(""))
			flushedRecords,flushed := memtable.PutRecord(record, blockManager, []int{blockManager.GetCurrentSegment()})
			if flushed {
				// ovde bjelica treba da napravi funkciju u koju ce da spakuje ovu logiku
				sstableRecords := make([]sstable.DataRecord, len(flushedRecords))
				//sada treba da se napravi sstable
				for i, record := range flushedRecords {
					rec := sstable.NewRecord(record.Key, record.Value, record.Tombstone, record.Timestamp)
					sstableRecords[i] = rec
				}
				sstable.WriteSstable(sstableRecords, summarySampleRate, false, oneFile)
			}	
		} else if operacija == "/" {
			blockManager.FlushUnsaved() //ako ima nesto u baferu a nema u wal-u, upisuje se u wal
			break
		} else {
			fmt.Println("Nepostojeca operacija")
		}
	}
}
