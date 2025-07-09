package main

import (
	"bufio"
	"fmt"
	"os"
	"project/BlockManager"
	"project/Memtable"
	"project/cache"
	"project/config"
	lsmtree "project/lsm_tree"
	"project/sstable"
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

	maxLevels := config.LSMTree.MaxLevels
	filesPerLevel := config.LSMTree.MaxFilesPerLevel
	tieredCompaction := config.LSMTree.TieredCompaction

	cacheSize := config.Cache.CacheSize

	lsmtree := lsmtree.NewLSMTree(maxLevels, filesPerLevel, tieredCompaction)

	os.MkdirAll("sstable/dict", os.ModePerm)

	blockManager := BlockManager.NewBlockManager(blockSize, blocksPerSegment, blockCacheSize)
	var compressionDictionary = sstable.NewDictionary()
	compressionDictionary.Deserialize(blockManager) // ucitavanje rečnika iz fajla
	fmt.Printf("Sadrzaj recnika: %v\n", compressionDictionary)

	cache := cache.NewLRUCache(cacheSize)

	memtable := Memtable.NewMemtable(implementType, numOfTables, tableSize)
	flushedReconstructRecords, flushed := memtable.Reconstruct(blockManager) // rekonstrukcija engine-a unosom zapisa iz Wal-a prilikom pokretanja
	if flushed {                                                             // moze da dodje do flush-a ako je promenjena konfiguracija
		for _, recs := range flushedReconstructRecords {
			if hasCompression {
				compressionDictionary.StoreKeys(blockManager, recs)
				fmt.Println("Kompresovani ključevi su sačuvani u rečniku")
			}
			sstable.WriteSstable(recs, summarySampleRate, false, oneFile, blockManager, hasCompression, 0)
		}
	}

	scanner := bufio.NewScanner(os.Stdin)
	pomocnik := 8

	if lsmtree.TieredCompaction {
		// Ako je size-tiered, dodajemo SSTable u LSM stablo
		lsmtree.AddSstable(0, summarySampleRate, oneFile, blockManager, hasCompression, compressionDictionary)
	} else {
		// Ako je leveled, dodajemo SSTable u LSM stablo
		lsmtree.AddSstableLeveled(0, summarySampleRate, oneFile, blockManager, hasCompression, compressionDictionary)
	}

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
			// Ako nije u memtable-u i kešu, tražimo u SSTable-ima, radi i sa kompresovanim ključevima i pravim

			SSrecord, keyFound := sstable.FindInSstables(kljuc, *blockManager, compressionDictionary)
			if keyFound {
				fmt.Println(string(SSrecord.Value))
				cache.PutInCache(kljuc, SSrecord.Value)
				continue
			}

			fmt.Println("Nema takvog kljuca u memtable-u, kešu i SSTable-ima")

		} else if operacija == "PUT" {
			fmt.Println("Unesite kljuc: ")
			scanner.Scan()
			kljuc := scanner.Text()
			fmt.Println("Unesite vrednost: ")
			scanner.Scan()
			vrednost := scanner.Text()

			/*
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
			*/
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
					fmt.Printf("Kompresovani ključevi su sačuvani u rečniku: %v\n", compressedRecords)
					sstable.WriteSstable(compressedRecords, summarySampleRate, false, oneFile, blockManager, hasCompression, 0) // upisivanje u SSTable
				} else {
					sstable.WriteSstable(flushedRecords, summarySampleRate, false, oneFile, blockManager, hasCompression, 0)
				}

				if lsmtree.TieredCompaction {
					// Ako je size-tiered, dodajemo SSTable u LSM stablo
					lsmtree.AddSstable(0, summarySampleRate, oneFile, blockManager, hasCompression, compressionDictionary)
				} else {
					// Ako je leveled, dodajemo SSTable u LSM stablo
					lsmtree.AddSstableLeveled(0, summarySampleRate, oneFile, blockManager, hasCompression, compressionDictionary)
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
					fmt.Println("Kompresovani ključevi su sačuvani u rečniku")
				}
				sstable.WriteSstable(flushedRecords, summarySampleRate, false, oneFile, blockManager, hasCompression, 0) // upisivanje u SSTable
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
