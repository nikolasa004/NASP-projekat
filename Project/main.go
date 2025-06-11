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

	config := config.ReadConfig()

	blockSize := config.BlockManager.BlockSize
	blocksPerSegment := config.BlockManager.BlocksInSegment
	cacheSize := config.BlockManager.CacheSize

	implementType := config.Memtable.ImplementType
	numOfTables := config.Memtable.NumOfTables
	tableSize := config.Memtable.TableSize

	summarySampleRate := config.SSTableManager.SummarySampleRate
	oneFile := config.SSTableManager.OneFile
	// compression := config.SSTableManager.Compresion

	blockManager := BlockManager.NewBlockManager(blockSize, blocksPerSegment, cacheSize)
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
			if record != nil {
				fmt.Println(string(record.Value))
				continue
			} else {
				record, found := sstable.FindInSstables(kljuc)
				if found {
					fmt.Println(string(record.Value))
					continue
				}
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
			blockManager.FlushUnsaved() //ako ima nesto u baferu a nema u wal-u, upisuje se u wal
			break
		} else {
			fmt.Println("Nepostojeca operacija")
		}
	}
}
