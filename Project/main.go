package main

import (
	"bufio"
	"fmt"
	"os"
	"project/BlockManager"
	"project/Memtable"
	"project/cache"
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
	blockCacheSize := config.BlockManager.BlockCacheSize

	implementType := config.Memtable.ImplementType // SkipList, Hashmap ili B-stablo
	numOfTables := config.Memtable.NumOfTables
	tableSize := config.Memtable.TableSize

	summarySampleRate := config.SSTableManager.SummarySampleRate
	oneFile := config.SSTableManager.OneFile
	hasCompression := config.SSTableManager.Compression

	cacheSize := config.Cache.CacheSize

	os.MkdirAll("sstable/dict", os.ModePerm)

	blockManager := BlockManager.NewBlockManager(blockSize, blocksPerSegment, blockCacheSize)
	var compressionDictionary *sstable.Dictionary
	fmt.Println(hasCompression)
	if hasCompression {
		compressionDictionary := sstable.NewDictionary()
		compressionDictionary.Deserialize(blockManager) // ucitavanje rečnika iz fajla
	}
	cache := cache.NewLRUCache(cacheSize)

	memtable := Memtable.NewMemtable(implementType, numOfTables, tableSize)
	flushedReconstructRecords, flushed := memtable.Reconstruct(blockManager) // rekonstrukcija engine-a unosom zapisa iz Wal-a prilikom pokretanja
	if flushed {                                                             // moze da dodje do flush-a ako je promenjena konfiguracija
		for _, recs := range flushedReconstructRecords {
			if hasCompression {
				compressionDictionary.StoreKeys(blockManager, recs)
			}
			sstable.WriteSstable(recs, summarySampleRate, false, oneFile, blockManager, hasCompression)
		}
	}

	scanner := bufio.NewScanner(os.Stdin)
	pomocnik := 8
	for {
		fmt.Println("Unesite operaciju ili / za kraj: ")
		var operacija string
		scanner.Scan()
		operacija = scanner.Text()
		if pomocnik > 0 && operacija == "PUT" {
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
			} else if record != nil && !found {
				fmt.Println("Nema takvog kljuca")
				continue
			}

			if value, found := cache.GetFromCache(kljuc); found {
				fmt.Println("Iz cache-a" + string(value))
				continue
			}

			SSrecord, found := sstable.FindInSstables(kljuc, *blockManager, compressionDictionary)
			if found {
				fmt.Println(string(SSrecord.Value))
				cache.PutInCache(kljuc, SSrecord.Value) // dodavanje u keš
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

			records := BlockManager.CreateBinaryRecordsWithFragmentation(false, kljuc, []byte(vrednost), blockSize, false)
			for _, record := range records {
				blockManager.PutRecord(record)
			}
			fmt.Println("SS - One file: ", oneFile)
			flushedRecords, flushed := memtable.PutRecord(false, kljuc, vrednost, blockManager, len(blockManager.GetSegments())-1)
			fmt.Println("Flushed", flushed)
			if flushed {
				if hasCompression {
					compressedRecords := compressionDictionary.StoreKeys(blockManager, flushedRecords) // čuvanje ključeva u rečniku
					sstable.WriteSstable(compressedRecords, summarySampleRate, false, oneFile, blockManager, hasCompression)
				} else {
					sstable.WriteSstable(flushedRecords, summarySampleRate, false, oneFile, blockManager, hasCompression)
				}
				//ovde sam ostavio da se keš ažurira sa flushed zapisima, a ne sa kompresovanim
				cache.UpdateCache(flushedRecords) // ažuriranje keša sa flushed zapisima
			}
		} else if operacija == "DELETE" {
			fmt.Println("Unesite kljuc: ")
			scanner.Scan()
			kljuc := scanner.Text()

			//napraviti zapis sa tombstone = true
			records := BlockManager.CreateBinaryRecordsWithFragmentation(true, kljuc, []byte(""), blockSize, false)
			for _, record := range records {
				blockManager.PutRecord(record)
			}
			flushedRecords, flushed := memtable.PutRecord(true, kljuc, "", blockManager, len(blockManager.GetSegments())-1)
			if flushed {
				if hasCompression {
					compressionDictionary.StoreKeys(blockManager, flushedRecords) // čuvanje ključeva u rečniku
				}
				sstable.WriteSstable(flushedRecords, summarySampleRate, false, oneFile, blockManager, hasCompression)
			}
		} else if operacija == "VERIFY" {
			fmt.Println("Unesite ime SSTable TOC fajla: ")
			scanner.Scan()
			userInput := scanner.Text()

			err := sstable.VerifySstableMerkle(userInput, blockManager)
			if err != nil {
				fmt.Println(err)
			}
		} else if operacija == "/" {
			blockManager.FlushUnsaved() //ako ima nesto u baferu a nema u wal-u, upisuje se u wal
			break
		} else {
			fmt.Println("Nepostojeca operacija")
		}
	}
}
