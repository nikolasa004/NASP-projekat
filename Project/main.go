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
	numOfTables := 3
	tableSize := 3

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
	reconstructRecs := blockManager.Reconstruct()

	memtable := Memtable.NewMemtable(implementType, numOfTables, tableSize)

	for _, record := range reconstructRecs {
		rec := memtable.CreateRecord(record.Tombstone, record.Key, record.Value)
		memtable.PutRecord(rec, blockManager, blockManager.GetRecordSegments()[record.Key], oneFile, summarySampleRate)
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
			record := memtable.GetRecord(kljuc)
			if record == nil {
				record, found := sstable.FindInSstables(kljuc)
				if found {
					fmt.Println(record.Timestamp)
					fmt.Println(string(record.Value))
					continue
				}
			}
			if record != nil {
				fmt.Println(string(record.Value))
			} else {
				fmt.Println("Nema takvog kljuca")
			}
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
			fmt.Println("One file: ", oneFile)
			memtable.PutRecord(record, blockManager, []int{blockManager.GetCurrentSegment()}, oneFile, summarySampleRate)
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
			memtable.PutRecord(record, blockManager, []int{blockManager.GetCurrentSegment()}, oneFile, summarySampleRate)
		} else if operacija == "/" {
			break
		} else {
			fmt.Println("Nepostojeca operacija")
		}
	}
}
