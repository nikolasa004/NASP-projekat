package main

import (
	"bufio"
	"fmt"
	"os"
	"project/BlockManager"
	"project/Memtable"
	"project/Types"
	"project/config"
	"project/sstable"
	"strings"
)

func main() {
	// bira se neka od metoda GET, PUT, DELETE
	// od svake operacije se pravi zapis

	config := config.ReadConfig()

	blockSize := config.BlockManager.BlockSize
	blocksPerSegment := config.BlockManager.BlocksInSegment
	cacheSize := config.BlockManager.CacheSize

	implementType := config.Memtable.ImplementType // SkipList, Hashmap ili B-stablo
	numOfTables := config.Memtable.NumOfTables
	tableSize := config.Memtable.TableSize

	summarySampleRate := config.SSTableManager.SummarySampleRate
	oneFile := config.SSTableManager.OneFile
	// compression := config.SSTableManager.Compresion

	blockManager := BlockManager.NewBlockManager(blockSize, blocksPerSegment, cacheSize)

	memtable := Memtable.NewMemtable(implementType, numOfTables, tableSize)
	flushedReconstructRecords, flushed := memtable.Reconstruct(blockManager) // rekonstrukcija engine-a unosom zapisa iz Wal-a prilikom pokretanja
	if flushed {                                                             // moze da dodje do flush-a ako je promenjena konfiguracija
		for _, recs := range flushedReconstructRecords {
			sstable.WriteSstable(recs, summarySampleRate, false, oneFile, blockManager)
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
			SSrecord, found := sstable.FindInSstables(kljuc, *blockManager)
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
			// kljuc := "key" + strconv.Itoa(pomocnik)

			// var filename string
			// switch pomocnik {
			// case 7:
			// 	filename = "primer.txt"
			// case 6:
			// 	filename = "primer1.txt"
			// case 5:
			// 	filename = "primer2.txt"
			// case 4:
			// 	filename = "primer3.txt"
			// case 3:
			// 	filename = "primer4.txt"
			// case 2:
			// 	filename = "primer5.txt"
			// case 1:
			// 	filename = "primer6.txt"
			// case 0:
			// 	continue
			// }

			// file, err := os.ReadFile(filename)
			// if err != nil {
			// 	fmt.Println("Greska pri citanju fajla:", err)
			// 	continue
			// }
			// vrednost := string(file)

			records := BlockManager.CreateBinaryRecordsWithFragmentation(false, kljuc, []byte(vrednost), blockSize, false) //vrednost ce mi biti serializovana struktura
			for _, record := range records {
				blockManager.PutRecord(record)
			}
			fmt.Println("SS - One file: ", oneFile)
			flushedRecords, flushed := memtable.PutRecord(false, kljuc, vrednost, blockManager, len(blockManager.GetSegments())-1)
			fmt.Println("Flushed", flushed)
			if flushed {
				sstable.WriteSstable(flushedRecords, summarySampleRate, false, oneFile, blockManager)
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
				sstable.WriteSstable(flushedRecords, summarySampleRate, false, oneFile, blockManager)
			}
		} else if operacija == "/" {
			blockManager.FlushUnsaved() //ako ima nesto u baferu a nema u wal-u, upisuje se u wal
			break
		} else if operacija == "Types" {
			for {
				fmt.Println("Sa čime želite da radite? Ukucajte cms za Count Min Sketch, hll za HyperLogLog, sh za SimHash ili bl za BloomFilter: ")
				var operacija string
				scanner.Scan()
				operacija = scanner.Text()
				if operacija == "cms" {

				cmsLoop:
					for {
						fmt.Println("Unesite željenju operaciju: dodaj za dodavanje elementa, izbrisi za brisanje, prebroj za prebrojavanje željenog elementa i novi za pravljenje novog count min sketch-a ili nazad za povratak na biranje strukture za korišćenje:  ")
						var operacija string
						scanner.Scan()
						operacija = scanner.Text()
						if operacija == "dodaj" {
							for {
								fmt.Println("Unesite ključ za CMS kojem će se dodati element ili ukucajte nazad da se vratite na odabir operacije: ")
								var kljucCMS string
								scanner.Scan()
								kljucCMS = scanner.Text()
								if kljucCMS == "nazad" {
									continue cmsLoop
								}
								if !strings.HasPrefix(kljucCMS, "cms_") {

									fmt.Println("Ključ mora da počinje sa cms_!")
									continue
								}
								record, found := memtable.GetRecord(kljucCMS)
								if found {
									value := record.Value
									cms, err := Types.Deserialize(value)
									if err != nil {
										fmt.Println("Greška pri deserializaciji: ", err)
									} else {
										var noviElement string
										for {
											fmt.Println(("Unesite željeni element: "))
											scanner.Scan()
											noviElement = scanner.Text()
											if noviElement == "" {
												fmt.Println("Element ne može biti prazan!")
												continue
											}
											break
										}
										cms.Add([]byte(noviElement))
										fmt.Println("Element uspešno dodat")

									}

									continue cmsLoop
								}

								SSrecord, found := sstable.FindInSstables(kljucCMS, *blockManager)
								if found {
									value := SSrecord.Value
									cms, err := Types.Deserialize(value)
									if err != nil {
										fmt.Println("Greška pri deserializaciji: ", err)
									} else {
										var noviElement string
										for {
											fmt.Println("Unesite željeni element: ")
											scanner.Scan()
											noviElement = scanner.Text()
											if noviElement == "" {
												fmt.Println("Element ne može biti prazan string!")
												continue
											}
											break
										}
										cms.Add([]byte(noviElement))
										fmt.Println("Element uspešno dodat")

									}
									continue cmsLoop

								}
								fmt.Println("Nema takvog kljuca")
							}

						}
					}
				}
			}
		} else {
			fmt.Println("Nepostojeca operacija")
		}
	}
}
