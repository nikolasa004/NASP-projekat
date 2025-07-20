package main

import (
	"bufio"
	"fmt"
	"os"
	"project/BlockManager"
	"project/Memtable"
	"project/RateLimit"
	"project/Scanners"
	"project/Types"
	"project/cache"
	"project/config"
	lsmtree "project/lsm_tree"
	"project/sstable"
	"strconv"
	"strings"
	"time"
)

func main() {
	// bira se neka od metoda GET, PUT, DELETE, TYPES
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
	multiplyer := config.LSMTree.Multiplyer

	cacheSize := config.Cache.CacheSize
	scannerCacheSize := config.ScannerCache.ScannerCacheSize

	lsmtree := lsmtree.NewLSMTree(maxLevels, filesPerLevel, tieredCompaction, multiplyer)
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

	const tokenBucketKey = "__rateLimit__"

	rateLimitState := &RateLimit.RateLimitState{
		StartTime: time.Now().Unix(),
		Count:     config.RateLimiter.Capacity,
		Capacity:  config.RateLimiter.Capacity,
		Interval:  config.RateLimiter.Interval,
	}

	var (
		existingSerialized []byte
		existingFound      bool
	)

	if record, found := memtable.GetRecord(tokenBucketKey); found {
		existingSerialized = []byte(record.Value)
		existingFound = true
	} else if record, found := sstable.FindInSstables(tokenBucketKey, *blockManager, compressionDictionary); found {
		existingSerialized = []byte(record.Value)
		existingFound = true
	}

	needToWrite := false

	if existingFound {
		existingState, err := RateLimit.DeserializeRateLimit(existingSerialized)
		if err != nil {
			fmt.Println("Greaka pri deserijalizaciji postojeceg token bucketa:", err)
			needToWrite = true
		} else {
			if existingState.Capacity != config.RateLimiter.Capacity || existingState.Interval != config.RateLimiter.Interval {
				fmt.Printf("Token bucket konfiguracija se promenila! Stara: cap=%d int=%d | Nova: cap=%d int=%d\n",
					existingState.Capacity, existingState.Interval,
					config.RateLimiter.Capacity, config.RateLimiter.Interval)
				needToWrite = true //treba true
			}
		}
	} else {
		fmt.Println("Token bucket nije pronadjen. Kreira se novi")
		needToWrite = true //treba true
	}

	if needToWrite {
		serialized, err := RateLimit.SerializeRateLimit(rateLimitState)
		if err != nil {
			fmt.Println("Greska pri serijalizaciji token bucketa:", err)
			os.Exit(1)
		}
		sysRecords := BlockManager.CreateBinaryRecordsWithFragmentation(false, tokenBucketKey, serialized, blockSize, false)
		for _, r := range sysRecords {
			blockManager.PutRecord(r)
		}
		flushedSys, sysFlushed := memtable.PutRecord(false, tokenBucketKey, string(serialized), blockManager, len(blockManager.GetSegments())-1)
		if sysFlushed {
			sstable.WriteSstable(flushedSys, summarySampleRate, false, oneFile, blockManager, hasCompression, 0)
		}
	}

	scanner := bufio.NewScanner(os.Stdin)

	if lsmtree.TieredCompaction {
		// Ako je size-tiered, dodajemo SSTable u LSM stablo
		lsmtree.AddSstable(0, summarySampleRate, oneFile, blockManager, hasCompression, compressionDictionary)
	} else {
		// Ako je leveled, dodajemo SSTable u LSM stablo
		lsmtree.AddSstableLeveled(0, summarySampleRate, oneFile, blockManager, hasCompression, compressionDictionary, multiplyer)
	}

	//pomocnik := 10
	for {
		fmt.Println("Unesite operaciju ili / za kraj: ")
		var operacija string
		scanner.Scan()
		operacija = scanner.Text()
		// if pomocnik > 0 {
		// 	operacija = "PUT" // za testiranje, da ne moram da unosim svaki put
		// 	pomocnik--
		// } else {
		// 	operacija = "/"
		// }

		if operacija == "GET" {

			if !RateLimit.CheckRateLimit(tokenBucketKey, memtable, blockManager, blockSize, summarySampleRate, oneFile, hasCompression, compressionDictionary) {
				continue
			}
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

			if !RateLimit.CheckRateLimit(tokenBucketKey, memtable, blockManager, blockSize, summarySampleRate, oneFile, hasCompression, compressionDictionary) {
				continue
			}

			fmt.Println("Unesite kljuc: ")
			scanner.Scan()
			kljuc := scanner.Text()
			//kljuc := "key" + strconv.Itoa(pomocnik)
			fmt.Println("Unesite vrednost: ")
			scanner.Scan()
			vrednost := scanner.Text()
			//vrednost := "value" + strconv.Itoa(pomocnik)

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
					lsmtree.AddSstableLeveled(0, summarySampleRate, oneFile, blockManager, hasCompression, compressionDictionary, multiplyer)
				}
				//ovde sam ostavio da se keš ažurira sa flushed zapisima, a ne sa kompresovanim
				cache.UpdateCache(flushedRecords) // ažuriranje keša sa flushed zapisima
			}
		} else if operacija == "DELETE" {
			if !RateLimit.CheckRateLimit(tokenBucketKey, memtable, blockManager, blockSize, summarySampleRate, oneFile, hasCompression, compressionDictionary) {
				continue
			}
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
		} else if operacija == "SCAN" {
			for {
				reader := bufio.NewReader(os.Stdin)
				fmt.Println("Odaberi tip skeniranja: ")
				fmt.Println("1. Prefix 2. Range 3. Prefix sa iteratorom 4. Range sa iteratorom ili 5. nazad: ")
				fmt.Println("Unesi za 1. prefix za 2. range za 3. iprefix  za 4. irange ili nazad za povratak: ")
				choiceInput, _ := reader.ReadString('\n')
				choice := strings.TrimSpace(choiceInput)
				if choice == "nazad" {
					break
				}
				var prefix, lowerBound, upperBound string
				useIterator := false
				pageSize := 0
				switch choice {
				case "prefix":
					for {
						fmt.Println("Unesite prefix: ")
						input, _ := reader.ReadString('\n')
						prefix = strings.TrimSpace(input)
						if prefix != "" {
							break
						}
						fmt.Println("Prefiks ne sme biti prazan.")
					}
				case "range":
					for {
						fmt.Println("Unesite donji limit: ")
						input, _ := reader.ReadString('\n')
						lowerBound = strings.TrimSpace(input)
						if lowerBound != "" {
							break
						}
						fmt.Println("Donji limit ne sme biti prazan.")
					}
					for {
						fmt.Println("Unesite gornji limit: ")
						input, _ := reader.ReadString('\n')
						upperBound = strings.TrimSpace(input)
						if upperBound != "" {
							break
						}
						fmt.Println("Gornji limit ne sme biti prazan.")
					}
				case "iprefix":
					for {
						fmt.Println("Unesite prefix: ")
						input, _ := reader.ReadString('\n')
						prefix = strings.TrimSpace(input)
						if prefix != "" {
							break
						}
						fmt.Println("Prefiks ne sme biti prazan.")
					}
					useIterator = true
					pageSize = 1

				case "irange":
					for {
						fmt.Println("Unesite donji limit: ")
						input, _ := reader.ReadString('\n')
						lowerBound = strings.TrimSpace(input)
						if lowerBound != "" {
							break
						}
						fmt.Println("Donji limit ne sme biti prazan.")
					}
					for {
						fmt.Println("Unesite gornji limit: ")
						input, _ := reader.ReadString('\n')
						upperBound = strings.TrimSpace(input)
						if upperBound != "" {
							break
						}
						fmt.Println("Gornji limit ne sme biti prazan.")
					}
					useIterator = true
					pageSize = 1
				default:
					fmt.Println("Nevazeci izbor.")
					continue
				}

				if !useIterator {
					for {
						fmt.Println("Unesite broj stranica: ")
						input, _ := reader.ReadString('\n')
						input = strings.TrimSpace(input)
						num, err := strconv.Atoi(input)
						if err == nil && num > 0 {
							pageSize = num
							break
						}
						fmt.Println("Unesite pozitivan ceo broj.")
					}
				}
				Scanners.Scan(prefix, lowerBound, upperBound, memtable, blockManager, hasCompression, compressionDictionary, pageSize, scannerCacheSize)
				break
			}
		} else if operacija == "/" {
			blockManager.FlushUnsaved() //ako ima nesto u baferu a nema u wal-u, upisuje se u wal
			break
		} else if operacija == "TYPES" {
			for {
				fmt.Println("Sa čime želite da radite? Ukucajte CMS za Count Min Sketch, HLL - HyperLogLog, SH - SimHash, BF - BloomFilter ili NAZAD: ")
				var operacijaTypes string
				scanner.Scan()
				operacijaTypes = scanner.Text()

				if operacijaTypes == "CMS" {
				cmsLoop:
					for {
						fmt.Println("Unesite željenju operaciju: dodaj, izbrisi, prebroj, novi, nazad")
						scanner.Scan()
						operacija := scanner.Text()

						if operacija == "dodaj" {
							if !RateLimit.CheckRateLimit(tokenBucketKey, memtable, blockManager, blockSize, summarySampleRate, oneFile, hasCompression, compressionDictionary) {
								continue
							}
							for {
								fmt.Println("Unesite ključ za CMS kojem će se dodati element ili ukucajte nazad da se vratite na odabir operacije: ")
								var keyCMS string
								scanner.Scan()
								keyCMS = scanner.Text()
								if keyCMS == "nazad" {
									continue cmsLoop
								}
								if !strings.HasPrefix(keyCMS, "cms_") {

									fmt.Println("Ključ mora da počinje sa cms_!")
									continue
								}

								var value []byte
								found := false

								if memRec, ok := memtable.GetRecord(keyCMS); ok {
									value = memRec.Value
									found = true
								} else if ssRec, ok := sstable.FindInSstables(keyCMS, *blockManager, compressionDictionary); ok {
									value = ssRec.Value
									found = true
								}

								if found {
									cms, err := Types.Deserialize(value)
									if err != nil {
										fmt.Println("Greška pri deserializaciji:", err)
									} else {
										var noviElement string
										for {
											fmt.Println("Unesite željeni element: ")
											scanner.Scan()
											noviElement = scanner.Text()
											if noviElement == "" {
												fmt.Println("Element ne može biti prazan!")
												continue
											}
											break
										}

										cms.Add([]byte(noviElement))
										serialized, err := cms.Serialize()
										if err != nil {
											fmt.Println("Greška pri serializaciji:", err)
											return
										}
										records := BlockManager.CreateBinaryRecordsWithFragmentation(false, cms.Key, serialized, blockSize, false)
										for _, record := range records {
											blockManager.PutRecord(record)
										}
										memtable.PutRecord(false, cms.Key, string(serialized), blockManager, len(blockManager.GetSegments())-1)

										fmt.Println("Element uspešno dodat")
									}
									continue cmsLoop
								}

								fmt.Println("Nema takvog ključa")
							}

						} else if operacija == "novi" {
							if !RateLimit.CheckRateLimit(tokenBucketKey, memtable, blockManager, blockSize, summarySampleRate, oneFile, hasCompression, compressionDictionary) {
								continue
							}
							for {
								fmt.Println("Unesite ključ za cms ili nazad za povratak: ")
								var keyCMS string
								scanner.Scan()
								keyCMS = scanner.Text()
								if keyCMS == "nazad" {
									continue cmsLoop
								}

								if !strings.HasPrefix(keyCMS, "cms_") {
									fmt.Println("Ključ mora početi sa 'cms_'")
									continue
								}

								//provera da li je unikatan
								if _, found := memtable.GetRecord(keyCMS); found {
									fmt.Println("Ključ već postoji(MemTable)")
									continue
								}
								if _, found := sstable.FindInSstables(keyCMS, *blockManager, compressionDictionary); found {
									fmt.Println("Ključ već postoji (SSTable)")
									continue
								}
								var epsilon, delta float64
								fmt.Println("Unesite epsilon:")
								for {
									scanner.Scan()
									input := scanner.Text()
									var err error
									epsilon, err = strconv.ParseFloat(input, 64)
									if err != nil || epsilon <= 0 {
										fmt.Println("Epsilon mora biti pozitivan broj")
										continue
									}
									break
								}

								fmt.Println("Unesite delta: ")
								for {
									scanner.Scan()
									input := scanner.Text()
									var err error
									delta, err = strconv.ParseFloat(input, 64)
									if err != nil || delta <= 0 {
										fmt.Println("Delta mora biti pozitivan broj")
										continue
									}
									break
								}

								m := Types.CalculateM(epsilon)
								k := Types.CalculateK(delta)
								cms := Types.NewCountMinSketch(uint32(k), uint32(m), keyCMS)

								serialized, err := cms.Serialize()
								if err != nil {
									fmt.Println("Greska pri serijalizaciji CountMinSketcha", err)
									continue
								}
								records := BlockManager.CreateBinaryRecordsWithFragmentation(false, keyCMS, serialized, blockSize, false) //vrednost ce mi biti serializovana struktura
								for _, record := range records {
									blockManager.PutRecord(record)
								}
								fmt.Println("SS - One file: ", oneFile)
								flushedRecords, flushed := memtable.PutRecord(false, keyCMS, string(serialized), blockManager, len(blockManager.GetSegments())-1)
								fmt.Println("Flushed", flushed)
								if flushed {
									sstable.WriteSstable(flushedRecords, summarySampleRate, false, oneFile, blockManager, hasCompression, 0)
								}
								fmt.Println("Novi cms uspešno napravljen i stavljen u memtable")
								continue cmsLoop
							}

						} else if operacija == "izbrisi" {
							if !RateLimit.CheckRateLimit(tokenBucketKey, memtable, blockManager, blockSize, summarySampleRate, oneFile, hasCompression, compressionDictionary) {
								continue
							}
							for {
								fmt.Println("Unesite ključ za cms ili nazad za povratak: ")
								var keyCMS string
								scanner.Scan()
								keyCMS = scanner.Text()
								if keyCMS == "nazad" {
									continue cmsLoop
								}

								if !strings.HasPrefix(keyCMS, "cms_") {
									fmt.Println("Ključ mora početi sa 'cms_'")
									continue
								}
								if _, found := memtable.GetRecord(keyCMS); !found {
									if _, found := sstable.FindInSstables(keyCMS, *blockManager, compressionDictionary); !found {
										fmt.Println("Ne postoji CMS sa tim kljucem.")
										continue
									}
								}
								//pravimo tombstone
								records := BlockManager.CreateBinaryRecordsWithFragmentation(true, keyCMS, []byte(""), blockSize, false)
								for _, record := range records {
									blockManager.PutRecord(record)
								}
								flushedRecords, flushed := memtable.PutRecord(true, keyCMS, "", blockManager, len(blockManager.GetSegments())-1)
								if flushed {
									sstable.WriteSstable(flushedRecords, summarySampleRate, false, oneFile, blockManager, hasCompression, 0)
								}
								fmt.Println(("CMS uspesno obrisan."))
								continue cmsLoop
							}

						} else if operacija == "prebroj" {
							if !RateLimit.CheckRateLimit(tokenBucketKey, memtable, blockManager, blockSize, summarySampleRate, oneFile, hasCompression, compressionDictionary) {
								continue
							}
							for {
								fmt.Println("Unesite ključ za cms ili nazad za povratak: ")
								var keyCMS string
								scanner.Scan()
								keyCMS = scanner.Text()
								if keyCMS == "nazad" {
									continue cmsLoop
								}

								if !strings.HasPrefix(keyCMS, "cms_") {
									fmt.Println("Ključ mora početi sa 'cms_'")
									continue
								}

								var cms *Types.CountMinSketch
								var err error

								if record, found := memtable.GetRecord(keyCMS); found {
									cms, err = Types.Deserialize(record.Value)
									if err != nil {
										fmt.Println("Greska pri deserializaciji CountMinSketcha (Memtable)", err)
										continue
									}
								} else if SSrecord, found := sstable.FindInSstables(keyCMS, *blockManager, compressionDictionary); found {
									cms, err = Types.Deserialize(SSrecord.Value)
									if err != nil {
										fmt.Println("Greska pri deserializaciji CountMinSketcha (SSTable):", err)
										continue
									}
								} else {
									fmt.Println("Nema takvog kljuca.")
									continue
								}
								var element string
								for {
									fmt.Println("Unesite zeljeni element: ")
									scanner.Scan()
									element = scanner.Text()
									if element == "" {
										fmt.Println("Element ne moze biti prazan!")
										continue
									}
									break
								}
								count := cms.Count([]byte(element))
								fmt.Printf("Element '%s' se pojavljuje ~ %d puta (procena).\n", element, count)
								continue cmsLoop
							}

						} else if operacija == "nazad" {
							break cmsLoop
						} else {
							fmt.Println("Nepoznata operacija.")
						}
					}

				} else if operacijaTypes == "HLL" {
				hllLoop:
					for {
						fmt.Println("Unesite željenu operaciju za HLL: novi, dodaj, kardinalitet, izbrisi, nazad")
						scanner.Scan()
						operacijaHLL := scanner.Text()

						if operacijaHLL == "novi" {
							if !RateLimit.CheckRateLimit(tokenBucketKey, memtable, blockManager, blockSize, summarySampleRate, oneFile, hasCompression, compressionDictionary) {
								continue
							}
							for {
								fmt.Println("Unesite ključ za hll ili nazad za povratak: ")
								var keyHLL string
								scanner.Scan()
								keyHLL = scanner.Text()
								if keyHLL == "nazad" {
									continue hllLoop
								}

								if !strings.HasPrefix(keyHLL, "hll_") {
									fmt.Println("Ključ mora početi sa 'hll_'")
									continue
								}

								//provera da li je unikatan
								if _, found := memtable.GetRecord(keyHLL); found {
									fmt.Println("Ključ već postoji(MemTable)")
									continue
								}
								if _, found := sstable.FindInSstables(keyHLL, *blockManager, compressionDictionary); found {
									fmt.Println("Ključ već postoji (SSTable)")
									continue
								}
								var p uint8
								var val int
								fmt.Println("Unesite preciznost:")
								for {
									scanner.Scan()
									input := scanner.Text()
									var err error
									val, err = strconv.Atoi(input)
									if err != nil || val < int(Types.HLL_MIN_PRECISION) || val > int(Types.HLL_MAX_PRECISION) {
										fmt.Println("Preciznost mora biti izmedju 4  i 16")
										continue
									}
									break
								}
								p = uint8(val)
								hll := Types.NewHLL(p, keyHLL)

								serialized, err := hll.SerializeHLL()
								if err != nil {
									fmt.Println("Greska pri serijalizaciji HyperLogLoga", err)
									continue
								}
								records := BlockManager.CreateBinaryRecordsWithFragmentation(false, keyHLL, serialized, blockSize, false) //vrednost ce mi biti serializovana struktura
								for _, record := range records {
									blockManager.PutRecord(record)
								}
								fmt.Println("SS - One file: ", oneFile)
								flushedRecords, flushed := memtable.PutRecord(false, keyHLL, string(serialized), blockManager, len(blockManager.GetSegments())-1)
								fmt.Println("Flushed", flushed)
								if flushed {
									sstable.WriteSstable(flushedRecords, summarySampleRate, false, oneFile, blockManager, hasCompression, 0)
								}
								fmt.Println("Novi HLL uspešno napravljen i stavljen u memtable")
								continue hllLoop
							}

						} else if operacijaHLL == "dodaj" {
							if !RateLimit.CheckRateLimit(tokenBucketKey, memtable, blockManager, blockSize, summarySampleRate, oneFile, hasCompression, compressionDictionary) {
								continue
							}
							for {
								fmt.Println("Unesite ključ za HLL kojem će se dodati element ili ukucajte nazad da se vratite na odabir operacije: ")
								var kljucHLL string
								scanner.Scan()
								kljucHLL = scanner.Text()
								if kljucHLL == "nazad" {
									continue hllLoop
								}
								if !strings.HasPrefix(kljucHLL, "hll_") {

									fmt.Println("Ključ mora da počinje sa cms_!")
									continue
								}
								var value []byte
								found := false

								if memRec, ok := memtable.GetRecord(kljucHLL); ok {
									value = memRec.Value
									found = true
								} else if ssRec, ok := sstable.FindInSstables(kljucHLL, *blockManager, compressionDictionary); ok {
									value = ssRec.Value
									found = true
								}

								if found {
									hll, err := Types.DeserializeHLL(value)
									if err != nil {
										fmt.Println("Greška pri deserializaciji:", err)
									} else {
										var noviElement string
										for {
											fmt.Println("Unesite željeni element: ")
											scanner.Scan()
											noviElement = scanner.Text()
											if noviElement == "" {
												fmt.Println("Element ne može biti prazan!")
												continue
											}
											break
										}

										hll.Add([]byte(noviElement))

										serialized, err := hll.SerializeHLL()
										if err != nil {
											fmt.Println("Greška pri serializaciji:", err)
											return
										}

										records := BlockManager.CreateBinaryRecordsWithFragmentation(false, hll.Key, serialized, blockSize, false)
										for _, record := range records {
											blockManager.PutRecord(record)
										}
										memtable.PutRecord(false, hll.Key, string(serialized), blockManager, len(blockManager.GetSegments())-1)

										fmt.Println("Element uspešno dodat")
									}
									continue hllLoop
								}

								fmt.Println("Nema takvog ključa")
							}

						} else if operacijaHLL == "kardinalitet" {
							if !RateLimit.CheckRateLimit(tokenBucketKey, memtable, blockManager, blockSize, summarySampleRate, oneFile, hasCompression, compressionDictionary) {
								continue
							}
							for {
								fmt.Println("Unesite ključ za hll ili nazad za povratak: ")
								var keyHLL string
								scanner.Scan()
								keyHLL = scanner.Text()
								if keyHLL == "nazad" {
									continue hllLoop
								}

								if !strings.HasPrefix(keyHLL, "hll_") {
									fmt.Println("Ključ mora početi sa 'hll_'")
									continue
								}

								var hll *Types.HLL
								var err error

								if record, found := memtable.GetRecord(keyHLL); found {
									hll, err = Types.DeserializeHLL(record.Value)
									if err != nil {
										fmt.Println("Greska pri deserializaciji HLL-a (Memtable)", err)
										continue
									}
								} else if SSrecord, found := sstable.FindInSstables(keyHLL, *blockManager, compressionDictionary); found {
									hll, err = Types.DeserializeHLL(SSrecord.Value)
									if err != nil {
										fmt.Println("Greska pri deserializaciji HLL-a (SSTable):", err)
										continue
									}
								} else {
									fmt.Println("Nema takvog kljuca.")
									continue
								}
								cardinality := hll.Estimate()
								fmt.Printf("Procenjen broj razlicitih elemenata: %.0f\n ", cardinality)
								continue hllLoop
							}

						} else if operacijaHLL == "izbrisi" {
							if !RateLimit.CheckRateLimit(tokenBucketKey, memtable, blockManager, blockSize, summarySampleRate, oneFile, hasCompression, compressionDictionary) {
								continue
							}
							for {
								fmt.Println("Unesite ključ za hll ili nazad za povratak: ")
								var keyHLL string
								scanner.Scan()
								keyHLL = scanner.Text()
								if keyHLL == "nazad" {
									continue hllLoop
								}

								if !strings.HasPrefix(keyHLL, "hll_") {
									fmt.Println("Ključ mora početi sa 'hll_'")
									continue
								}
								if _, found := memtable.GetRecord(keyHLL); !found {
									if _, found := sstable.FindInSstables(keyHLL, *blockManager, compressionDictionary); !found {
										fmt.Println("Ne postoji HLL sa tim kljucem.")
										continue
									}
								}
								//pravimo tombstone
								records := BlockManager.CreateBinaryRecordsWithFragmentation(true, keyHLL, []byte(""), blockSize, false)
								for _, record := range records {
									blockManager.PutRecord(record)
								}
								flushedRecords, flushed := memtable.PutRecord(true, keyHLL, "", blockManager, len(blockManager.GetSegments())-1)
								if flushed {
									sstable.WriteSstable(flushedRecords, summarySampleRate, false, oneFile, blockManager, hasCompression, 0)
								}
								fmt.Println(("HLL uspesno obrisan."))
								continue hllLoop
							}

						} else if operacijaHLL == "nazad" {
							break hllLoop
						} else {
							fmt.Println("Nepostojeća operacija za HLL")
						}
					}
				} else if operacijaTypes == "SH" {
					if !RateLimit.CheckRateLimit(tokenBucketKey, memtable, blockManager, blockSize, summarySampleRate, oneFile, hasCompression, compressionDictionary) {
						continue
					}
					for {
						fmt.Println("Unesite dodaj za dodavanje novog fingerprinta, udaljenost za hemingovu udaljenst izmedju dva fingerprinta, ili nazad za povratak: ")
						scanner.Scan()
						input := scanner.Text()

						if input == "nazad" {
							break
						} else if input == "dodaj" {
							var keySH string
							for {
								fmt.Println("Unesite kljuc za fingerprint koji zelite dodati: ")
								scanner.Scan()
								keySH = scanner.Text()

								if !strings.HasPrefix(keySH, "sh_") {
									fmt.Println("Kljuc mora pocinjati sa sh_!")
									continue
								}
								if _, found := memtable.GetRecord(keySH); found {
									fmt.Println("Kljuc vec postoji!")
									continue
								}
								if _, found := sstable.FindInSstables(keySH, *blockManager, compressionDictionary); found {
									fmt.Println("Kljuc vec postoji (SStalbe)!")
									continue
								}
								break
							}
							fmt.Println("Unesite tekst: ")
							scanner.Scan()
							text := scanner.Text()
							fingerprint := Types.ComputeFingerprint(text)
							fmt.Println("Fingerprint: ", fingerprint)

							records := BlockManager.CreateBinaryRecordsWithFragmentation(false, keySH, []byte(fingerprint), blockSize, false)
							for _, record := range records {
								blockManager.PutRecord(record)
							}
							flushedRecords, flushed := memtable.PutRecord(false, keySH, fingerprint, blockManager, len(blockManager.GetSegments())-1)
							if flushed {
								sstable.WriteSstable(flushedRecords, summarySampleRate, false, oneFile, blockManager, hasCompression, 0)
							}
							fmt.Println("Fingerprint uspesno dodat.")
						} else if input == "udaljenost" {
							if !RateLimit.CheckRateLimit(tokenBucketKey, memtable, blockManager, blockSize, summarySampleRate, oneFile, hasCompression, compressionDictionary) {
								continue
							}
							var keySH1, keySH2 string
							var v1, v2 *Memtable.MemtableRecord
							for {
								fmt.Println("Unesite prvi kljuc: ")
								scanner.Scan()
								keySH1 = scanner.Text()
								if !strings.HasPrefix(keySH1, "sh_") {
									fmt.Println("Kljuc mora pocinjati sa sh_!")
									continue
								}
								var found bool
								v1, found = memtable.GetRecord(keySH1)
								if !found {
									if val, foundS := sstable.FindInSstables(keySH1, *blockManager, compressionDictionary); foundS {
										v1 = &Memtable.MemtableRecord{Value: val.Value}
									} else {
										fmt.Println("Kljuc ne postoji")
										continue
									}

								}
								break
							}

							for {
								fmt.Println("Unesite drugi kljuc: ")
								scanner.Scan()
								keySH2 = scanner.Text()
								if !strings.HasPrefix(keySH2, "sh_") {
									fmt.Println("Kljuc mora pocinjati sa sh_!")
									continue
								}
								var found bool
								v2, found = memtable.GetRecord(keySH1)
								if !found {
									if val, foundS := sstable.FindInSstables(keySH1, *blockManager, compressionDictionary); foundS {
										v2 = &Memtable.MemtableRecord{Value: val.Value}
									} else {
										fmt.Println("Kljuc ne postoji")
										continue
									}

								}
								break
							}

							dist := Types.HammingDistance(string(v1.Value), string(v2.Value))
							fmt.Println("Hammingova udaljenost: ", dist)

						} else {
							fmt.Println("nepostojeca operacija")
						}
					}
				} else if operacijaTypes == "BF" {
				bfLoop:
					for {
						fmt.Println("Unesite željenju operaciju: dodaj, izbrisi, postoji, novi, nazad")
						scanner.Scan()
						operacija := scanner.Text()

						if operacija == "dodaj" {
							if !RateLimit.CheckRateLimit(tokenBucketKey, memtable, blockManager, blockSize, summarySampleRate, oneFile, hasCompression, compressionDictionary) {
								continue
							}
							for {
								fmt.Println("Unesite ključ za Bloom FIlter kojem će se dodati element ili ukucajte nazad da se vratite na odabir operacije: ")
								var kljucBF string
								scanner.Scan()
								kljucBF = scanner.Text()
								if kljucBF == "nazad" {
									continue bfLoop
								}
								if !strings.HasPrefix(kljucBF, "bf_") {

									fmt.Println("Ključ mora da počinje sa bf_!")
									continue
								}
								var value []byte
								found := false

								if memRec, ok := memtable.GetRecord(kljucBF); ok {
									value = memRec.Value
									found = true
								} else if ssRec, ok := sstable.FindInSstables(kljucBF, *blockManager, compressionDictionary); ok {
									value = ssRec.Value
									found = true
								}

								if found {
									bf, err := Types.DeserializeBFWK(value)
									if err != nil {
										fmt.Println("Greška pri deserializaciji:", err)
									} else {
										var noviElement string
										for {
											fmt.Println("Unesite željeni element: ")
											scanner.Scan()
											noviElement = scanner.Text()
											if noviElement == "" {
												fmt.Println("Element ne može biti prazan!")
												continue
											}
											break
										}

										bf.BF.Add(noviElement)

										serialized, err := bf.SerializeBFWK()
										if err != nil {
											fmt.Println("Greška pri serializaciji:", err)
											return
										}

										records := BlockManager.CreateBinaryRecordsWithFragmentation(false, bf.Key, serialized, blockSize, false)
										for _, record := range records {
											blockManager.PutRecord(record)
										}
										memtable.PutRecord(false, bf.Key, string(serialized), blockManager, len(blockManager.GetSegments())-1)

										fmt.Println("Element uspešno dodat")
									}
									continue bfLoop
								}

								fmt.Println("Nema takvog ključa")
							}

						} else if operacija == "novi" {
							if !RateLimit.CheckRateLimit(tokenBucketKey, memtable, blockManager, blockSize, summarySampleRate, oneFile, hasCompression, compressionDictionary) {
								continue
							}
							for {
								fmt.Println("Unesite ključ za bf ili nazad za povratak: ")
								var keyBF string
								scanner.Scan()
								keyBF = scanner.Text()
								if keyBF == "nazad" {
									continue bfLoop
								}

								if !strings.HasPrefix(keyBF, "bf_") {
									fmt.Println("Ključ mora početi sa 'cms_'")
									continue
								}

								//provera da li je unikatan
								if _, found := memtable.GetRecord(keyBF); found {
									fmt.Println("Ključ već postoji(MemTable)")
									continue
								}
								if _, found := sstable.FindInSstables(keyBF, *blockManager, compressionDictionary); found {
									fmt.Println("Ključ već postoji (SSTable)")
									continue
								}
								var n int
								var p float64
								fmt.Println("Unesite ocekivani broj elemenata:")
								for {
									scanner.Scan()
									input := scanner.Text()
									var err error
									n, err = strconv.Atoi(input)
									if err != nil || n <= 0 {
										fmt.Println("Mora biti pozitivan ceo broj")
										continue
									}
									break
								}

								fmt.Println("Unesite dozvoljenu stopu false positive (izmedju 0 i 1): ")
								for {
									scanner.Scan()
									input := scanner.Text()
									var err error
									p, err = strconv.ParseFloat(input, 64)
									if err != nil || p <= 0 || p >= 1 {
										fmt.Println("Broj mora biti izmedju 0 i 1")
										continue
									}
									break
								}
								bf := sstable.NewBloomFilter(n, p)
								bloomWithKey := Types.BloomFilterWithKey{
									Key: keyBF,
									BF:  bf,
								}

								serialized, err := bloomWithKey.SerializeBFWK()
								if err != nil {
									fmt.Println("Greska pri serijalizaciji Bloom Filtera", err)
									continue
								}
								records := BlockManager.CreateBinaryRecordsWithFragmentation(false, keyBF, serialized, blockSize, false) //vrednost ce mi biti serializovana struktura
								for _, record := range records {
									blockManager.PutRecord(record)
								}
								fmt.Println("SS - One file: ", oneFile)
								flushedRecords, flushed := memtable.PutRecord(false, keyBF, string(serialized), blockManager, len(blockManager.GetSegments())-1)
								fmt.Println("Flushed", flushed)
								if flushed {
									sstable.WriteSstable(flushedRecords, summarySampleRate, false, oneFile, blockManager, hasCompression, 0)
								}
								fmt.Println("Novi bf uspešno napravljen i stavljen u memtable")
								continue bfLoop
							}

						} else if operacija == "izbrisi" {
							if !RateLimit.CheckRateLimit(tokenBucketKey, memtable, blockManager, blockSize, summarySampleRate, oneFile, hasCompression, compressionDictionary) {
								continue
							}
							for {
								fmt.Println("Unesite ključ za cms ili nazad za povratak: ")
								var keyBF string
								scanner.Scan()
								keyBF = scanner.Text()
								if keyBF == "nazad" {
									continue bfLoop
								}

								if !strings.HasPrefix(keyBF, "bf_") {
									fmt.Println("Ključ mora početi sa 'bf_'")
									continue
								}
								if _, found := memtable.GetRecord(keyBF); !found {
									if _, found := sstable.FindInSstables(keyBF, *blockManager, compressionDictionary); !found {
										fmt.Println("Ne postoji CMS sa tim kljucem.")
										continue
									}
								}
								//pravimo tombstone
								records := BlockManager.CreateBinaryRecordsWithFragmentation(true, keyBF, []byte(""), blockSize, false)
								for _, record := range records {
									blockManager.PutRecord(record)
								}
								flushedRecords, flushed := memtable.PutRecord(true, keyBF, "", blockManager, len(blockManager.GetSegments())-1)
								if flushed {
									sstable.WriteSstable(flushedRecords, summarySampleRate, false, oneFile, blockManager, hasCompression, 0)
								}
								fmt.Println(("BF uspesno obrisan."))
								continue bfLoop
							}

						} else if operacija == "postoji" {
							if !RateLimit.CheckRateLimit(tokenBucketKey, memtable, blockManager, blockSize, summarySampleRate, oneFile, hasCompression, compressionDictionary) {
								continue
							}
							for {
								fmt.Println("Unesite ključ za bf ili nazad za povratak: ")
								var keyBF string
								scanner.Scan()
								keyBF = scanner.Text()
								if keyBF == "nazad" {
									continue bfLoop
								}

								if !strings.HasPrefix(keyBF, "bf_") {
									fmt.Println("Ključ mora početi sa 'bf_'")
									continue
								}

								var bf *Types.BloomFilterWithKey
								var err error

								if record, found := memtable.GetRecord(keyBF); found {
									bf, err = Types.DeserializeBFWK(record.Value)
									if err != nil {
										fmt.Println("Greska pri deserializaciji Bloom FIltera (Memtable)", err)
										continue
									}
								} else if SSrecord, found := sstable.FindInSstables(keyBF, *blockManager, compressionDictionary); found {
									bf, err = Types.DeserializeBFWK(SSrecord.Value)
									if err != nil {
										fmt.Println("Greska pri deserializaciji Bloom FIltera (SSTable):", err)
										continue
									}
								} else {
									fmt.Println("Nema takvog kljuca.")
									continue
								}
								fmt.Println("Unesite zeljeni element: ")
								var element string
								for {
									scanner.Scan()
									element = scanner.Text()
									if element == "" {
										fmt.Println("Element ne moze biti prazan!")
										continue
									}
									break
								}
								exists := bf.BF.Check(element)
								if exists {
									fmt.Println("Element postoji.")
								} else {
									fmt.Println("Element ne postoji.")
								}

								continue bfLoop
							}

						} else if operacija == "nazad" {
							break bfLoop
						} else {
							fmt.Println("Nepoznata operacija.")
						}
					}

				} else if operacijaTypes == "nazad" {
					break // izlaz iz Types petlje
				} else {
					fmt.Println("Nepostojeca operacija")
				}
			}
		} else {
			fmt.Println("Nepostojeca operacija")
		}
	}
}
