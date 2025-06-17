package main

import (
	"bufio"
	"fmt"
	"os"
	"project/BlockManager"
	"project/Memtable"
	"project/config"
	"project/sstable"
	"strconv"
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
	pomocnik := 8
	for {
		fmt.Println("Unesite operaciju ili / za kraj: ")
		var operacija string
		scanner.Scan()
		operacija = scanner.Text()
		if pomocnik > 0 && operacija == "PUT"{
			pomocnik--
		}
		// if pomocnik > 1 {
		// 	operacija = "PUT" // za testiranje, moze se menjati na GET ili DELETE
		// 	pomocnik--
		// } else {
		// 	operacija = "/"
		// }
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
			// fmt.Println("Unesite kljuc: ")
			// scanner.Scan()
			// kljuc := scanner.Text()
			// fmt.Println("Unesite vrednost: ")
			// scanner.Scan()
			// vrednost := scanner.Text()
			kljuc := "key" + strconv.Itoa(pomocnik)

			var filename string
			switch pomocnik {
			case 7:
				filename = "primer.txt"
			case 6:
				filename = "primer1.txt"
			case 5:
				filename = "primer2.txt"
			case 4:
				filename = "primer3.txt"
			case 3:
				filename = "primer4.txt"
			case 2:
				filename = "primer5.txt"
			case 1:
				filename = "primer6.txt"
			case 0:
				continue
			}

			file, err := os.ReadFile(filename)
			if err != nil {
				fmt.Println("Greska pri citanju fajla:", err)
				continue
			}
			vrednost := string(file)

			records := BlockManager.CreateBinaryRecordsWithFragmentation(false, kljuc, []byte(vrednost), blockSize)
			for _, record := range records {
				blockManager.PutRecord(record)
			}
			record := memtable.CreateRecord(false, kljuc, []byte(vrednost))
			fmt.Println("SS - One file: ", oneFile)
			flushedRecords, flushed := memtable.PutRecord(record, blockManager, len(blockManager.GetSegments()) - 1)
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
			flushedRecords, flushed := memtable.PutRecord(record, blockManager, len(blockManager.GetSegments()) - 1)
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
