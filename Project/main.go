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

func promptForCustomKey(scanner *bufio.Scanner) (string, bool) {
	disallowedPrefixes := []string{"bf_", "cms_", "__rateLimit__", "hll_", "sh_"}

	fmt.Println("Unesite kljuc:")
	scanner.Scan()
	key := scanner.Text()

	for _, prefix := range disallowedPrefixes {
		if strings.HasPrefix(key, prefix) {
			fmt.Println("Kljuc ne sme pocinjati sa rezervisanim prefiksom:", prefix)
			return "", false
		}
	}

	return key, true
}

func promptForKey(scanner *bufio.Scanner, prefix, msg string) (string, bool) {
	for {
		fmt.Println(msg)
		scanner.Scan()
		key := scanner.Text()
		if key == "nazad" {
			return "", false
		}
		if !strings.HasPrefix(key, prefix) {
			fmt.Printf("Kljuc mora poceti sa '%s'\n", prefix)
			continue
		}
		return key, true
	}
}

func loadFromStorage(key string, memtable *Memtable.Memtable, blockManager *BlockManager.BlockManager, compressionDict *sstable.Dictionary) ([]byte, bool) {
	if rec, ok := memtable.GetRecord(key); ok {
		return rec.Value, true
	} else if rec, ok := sstable.FindInSstables(key, *blockManager, compressionDict); ok {
		return rec.Value, true
	}
	return nil, false
}

func writeToStorage(key string, value []byte, isTombstone bool, blockSize int, blockManager *BlockManager.BlockManager,
	memtable *Memtable.Memtable, summarySampleRate int, oneFile, hasCompression bool) {

	records := BlockManager.CreateBinaryRecordsWithFragmentation(isTombstone, key, value, blockSize, false)
	for _, record := range records {
		blockManager.PutRecord(record)
	}
	flushedRecords, flushed := memtable.PutRecord(isTombstone, key, string(value), blockManager, len(blockManager.GetSegments())-1)
	if flushed {
		sstable.WriteSstable(flushedRecords, summarySampleRate, false, oneFile, blockManager, hasCompression, 0)
	}
}

func promptForNonEmptyString(scanner *bufio.Scanner, msg string) string {
	for {
		fmt.Println(msg)
		scanner.Scan()
		text := scanner.Text()
		if text != "" {
			return text
		}
		fmt.Println("Unos ne moze biti prazan!")
	}
}

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

	blockManager := BlockManager.NewBlockManager(blockSize, blocksPerSegment, blockCacheSize)
	var compressionDictionary = sstable.NewDictionary()
	compressionDictionary.Deserialize(blockManager)

	cache := cache.NewLRUCache(cacheSize)

	memtable := Memtable.NewMemtable(implementType, numOfTables, tableSize)
	flushedReconstructRecords, flushed := memtable.Reconstruct(blockManager)
	if flushed { // moze da dodje do flush-a ako je promenjena konfiguracija
		for _, recs := range flushedReconstructRecords {
			if hasCompression {
				compressionDictionary.StoreKeys(blockManager, recs)
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

	amountOfKeys := 200
	counter := 0
	min := 1
	max := amountOfKeys

	operacija := "PUT"
	key := ""
	value := ""

	testPhase := 0
	returnedValues := []string{}
	expectedValues := []string{}

	foundIn := []string{}

	deleteStep := 3
	for {
		//////////////////////
		////////TEST//////////
		//////////////////////
		if counter < max && testPhase == 0 { //unos kljuceva
			counter++
			key = "key" + strconv.Itoa(counter)
			value = "value" + strconv.Itoa(counter)
		} else if counter < max && testPhase == 1 { //provera unetih kljuceva
			counter++
			key = "key" + strconv.Itoa(counter)
		} else if counter < max-deleteStep && testPhase == 2 { //brisanje svakog n-tog ključa
			counter += deleteStep
			key = "key" + strconv.Itoa(counter)
		} else if counter < max && testPhase == 3 { //provera da li su obrisani ključevi
			counter++
			key = "key" + strconv.Itoa(counter)
		} else {
			switch testPhase {
			case 0:
				operacija = "GET"
				for i := min; i <= max; i++ {
					expectedValues = append(expectedValues, "value"+strconv.Itoa(i))
				}
				counter = min - 1
			case 1:
				operacija = "DELETE"
				for i := min; i <= max; i++ {
					if expectedValues[i-1] != returnedValues[i-1] {
						fmt.Printf("Greska u testu: ocekivano '%s', a vraceno '%s'\n", expectedValues[i-1], returnedValues[i-1])
						return
					}
				}
				returnedValues = []string{}
				expectedValues = []string{}
				foundIn = []string{}

				counter = min - deleteStep
			case 2:
				operacija = "GET"
				nextDeleted := min
				for i := min; i <= max; i++ {
					if i == nextDeleted {
						expectedValues = append(expectedValues, "X")
						nextDeleted += deleteStep
					} else {
						expectedValues = append(expectedValues, "value"+strconv.Itoa(i))
					}
				}

				counter = min - 1
			case 3:
				operacija = "PUT"
				for i := min; i <= max; i++ {
					if expectedValues[i-1] != returnedValues[i-1] {
						fmt.Printf("Greska u testu: ocekivano '%s', a vraceno '%s'\n", expectedValues[i-1], returnedValues[i-1])
						return
					}
				}
				returnedValues = []string{}
				expectedValues = []string{}
				foundIn = []string{}
			default:
				fmt.Println("Test završen uspešno!")

				// entries, err := os.ReadDir("data/sstable")
				// if err != nil {
				// 	fmt.Printf("Greška pri ciscenju fajlova: %v\n", err)
				// 	return
				// }
				// for _, entry := range entries {
				// 	if entry.IsDir() {
				// 		subfolderPath := filepath.Join("data/sstable", entry.Name())
				// 		err := os.RemoveAll(subfolderPath)
				// 		if err != nil {
				// 			fmt.Printf("Greška pri brisanju %s: %v\n", subfolderPath, err)
				// 			return
				// 		}
				// 	}
				// }

				// file, err := os.OpenFile("data/sstable/master-toc.txt", os.O_WRONLY|os.O_TRUNC|os.O_CREATE, 0644)
				// if err != nil {
				// 	fmt.Printf("Greška pri otvaranju master-toc fajla: %v\n", err)
				// 	return
				// }
				// defer file.Close()

				// entries, err = os.ReadDir("BlockManager/wal")
				// if err != nil {
				// 	fmt.Printf("Greška pri ciscenju fajlova: %v\n", err)
				// 	return
				// }
				// for _, entry := range entries {
				// 	walSegment := filepath.Join("BlockManager/wal", entry.Name())
				// 	err := os.RemoveAll(walSegment)
				// 	if err != nil {
				// 		fmt.Printf("Greška pri brisanju %s: %v\n", walSegment, err)
				// 		return
				// 	}
				// }

				return
			}
			testPhase++
			continue
		}
		//////////////////////
		if operacija == "GET" {

			// if !RateLimit.CheckRateLimit(tokenBucketKey, memtable, blockManager, blockSize, summarySampleRate, oneFile, hasCompression, compressionDictionary) {
			// 	continue
			// }
			// kljuc, ok := promptForCustomKey(scanner)
			// if !ok {
			// 	fmt.Println("Povratak u meni.")
			// 	continue
			// }

			record, found := memtable.GetRecord(key)
			if found {
				returnedValues = append(returnedValues, string(record.Value))
				foundIn = append(foundIn, "memtable")
				continue

			} else if record != nil && !found { // ako je pronađen zapis ali je izbrisan
				returnedValues = append(returnedValues, "X")
				foundIn = append(foundIn, "memtable")
				continue
			}

			if value, found := cache.GetFromCache(key); found {
				returnedValues = append(returnedValues, string(value))
				foundIn = append(foundIn, "cache")
				continue
			}
			// Ako nije u memtable-u i kešu, tražimo u SSTable-ima, radi i sa kompresovanim ključevima i pravim

			if testPhase == 3 {
				fmt.Println("Traženje u SSTable-ima...")
			}
			SSrecord, keyFound := sstable.FindInSstables(key, *blockManager, compressionDictionary)
			if keyFound {
				returnedValues = append(returnedValues, string(SSrecord.Value))
				foundIn = append(foundIn, "sstable")
				cache.PutInCache(key, SSrecord.Value)
				continue
			}

			returnedValues = append(returnedValues, "X")
			foundIn = append(foundIn, "none")
		} else if operacija == "PUT" {

			// if !RateLimit.CheckRateLimit(tokenBucketKey, memtable, blockManager, blockSize, summarySampleRate, oneFile, hasCompression, compressionDictionary) {
			// 	continue
			// }

			// kljuc, ok := promptForCustomKey(scanner)
			// if !ok {
			// 	fmt.Println("Povratak u meni.")
			// 	continue
			// }

			records := BlockManager.CreateBinaryRecordsWithFragmentation(false, key, []byte(value), blockSize, false)
			for _, record := range records {
				blockManager.PutRecord(record)
			}
			flushedRecords, flushed := memtable.PutRecord(false, key, value, blockManager, len(blockManager.GetSegments())-1)
			if flushed {
				if hasCompression {
					compressedRecords := compressionDictionary.StoreKeys(blockManager, flushedRecords)
					sstable.WriteSstable(compressedRecords, summarySampleRate, false, oneFile, blockManager, hasCompression, 0)
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
			// if !RateLimit.CheckRateLimit(tokenBucketKey, memtable, blockManager, blockSize, summarySampleRate, oneFile, hasCompression, compressionDictionary) {
			// 	continue
			// }
			// kljuc, ok := promptForCustomKey(scanner)
			// if !ok {
			// 	fmt.Println("Povratak u meni.")
			// 	continue
			// }

			//napraviti zapis sa tombstone = true
			records := BlockManager.CreateBinaryRecordsWithFragmentation(true, key, []byte(""), blockSize, false)
			for _, record := range records {
				blockManager.PutRecord(record)
			}

			flushedRecords, flushed := memtable.PutRecord(true, key, "", blockManager, len(blockManager.GetSegments())-1)
			if flushed {
				if hasCompression {
					compressedRecords := compressionDictionary.StoreKeys(blockManager, flushedRecords) // čuvanje ključeva u rečniku
					sstable.WriteSstable(compressedRecords, summarySampleRate, false, oneFile, blockManager, hasCompression, 0)
				} else {
					sstable.WriteSstable(flushedRecords, summarySampleRate, false, oneFile, blockManager, hasCompression, 0) // upisivanje u SSTable
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
		} else if operacija == "VERIFY" {
			fmt.Println("Unesite ime SSTable TOC fajla: ")
			scanner.Scan()
			userInput := scanner.Text()

			err := sstable.VerifySstableMerkle(userInput, blockManager)
			if err != nil {
				fmt.Println(err)
			}
		} else if operacija == "SCAN" {
			if !RateLimit.CheckRateLimit(tokenBucketKey, memtable, blockManager, blockSize, summarySampleRate, oneFile, hasCompression, compressionDictionary) {
				continue
			}
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
							keyCMS, ok := promptForKey(scanner, "cms_", "Unesite kljuc za CMS ili ukucajte nazad")
							if !ok {
								continue cmsLoop
							}

							value, found := loadFromStorage(keyCMS, memtable, blockManager, compressionDictionary)
							if !found {
								fmt.Println("Nema takvog kljuca.")
								continue
							}

							cms, err := Types.Deserialize(value)
							if err != nil {
								fmt.Println("Greška pri deserializaciji:", err)
								continue
							}
							element := promptForNonEmptyString(scanner, "Unesite željeni element: ")

							cms.Add([]byte(element))
							serialized, err := cms.Serialize()
							if err != nil {
								fmt.Println("Greška pri serializaciji:", err)
								continue
							}
							writeToStorage(cms.Key, serialized, false, blockSize, blockManager, memtable, summarySampleRate, oneFile, hasCompression)
							fmt.Println("Element uspesno dodat u CMS.")
							continue cmsLoop

						} else if operacija == "novi" {
							if !RateLimit.CheckRateLimit(tokenBucketKey, memtable, blockManager, blockSize, summarySampleRate, oneFile, hasCompression, compressionDictionary) {
								continue
							}

							keyCMS, ok := promptForKey(scanner, "cms_", "Unesite ključ za CMS ili nazad za povratak:")
							if !ok {
								continue cmsLoop
							}

							if _, found := loadFromStorage(keyCMS, memtable, blockManager, compressionDictionary); found {
								fmt.Println("Ključ već postoji")
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

							fmt.Println("Unesite delta:")
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
								fmt.Println("Greška pri serijalizaciji:", err)
								continue
							}

							writeToStorage(keyCMS, serialized, false, blockSize, blockManager, memtable, summarySampleRate, oneFile, hasCompression)
							fmt.Println("Novi CMS uspešno napravljen i stavljen u MemTable")
							continue cmsLoop

						} else if operacija == "izbrisi" {
							if !RateLimit.CheckRateLimit(tokenBucketKey, memtable, blockManager, blockSize, summarySampleRate, oneFile, hasCompression, compressionDictionary) {
								continue
							}

							keyCMS, ok := promptForKey(scanner, "cms_", "Unesite ključ za CMS koji se briše ili nazad:")
							if !ok {
								continue cmsLoop
							}

							if _, found := loadFromStorage(keyCMS, memtable, blockManager, compressionDictionary); !found {
								fmt.Println("Ne postoji CMS sa tim ključem.")
								continue
							}

							writeToStorage(keyCMS, []byte(""), true, blockSize, blockManager, memtable, summarySampleRate, oneFile, hasCompression)
							fmt.Println("CMS uspešno obrisan.")
							continue cmsLoop

						} else if operacija == "prebroj" {
							if !RateLimit.CheckRateLimit(tokenBucketKey, memtable, blockManager, blockSize, summarySampleRate, oneFile, hasCompression, compressionDictionary) {
								continue
							}

							keyCMS, ok := promptForKey(scanner, "cms_", "Unesite ključ za CMS:")
							if !ok {
								continue cmsLoop
							}

							value, found := loadFromStorage(keyCMS, memtable, blockManager, compressionDictionary)
							if !found {
								fmt.Println("Nema takvog ključa.")
								continue
							}

							cms, err := Types.Deserialize(value)
							if err != nil {
								fmt.Println("Greška pri deserializaciji:", err)
								continue
							}

							element := promptForNonEmptyString(scanner, "Unesite željeni element:")
							count := cms.Count([]byte(element))
							fmt.Printf("Element '%s' se pojavljuje ~ %d puta (procena).\n", element, count)
							continue cmsLoop

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
							key, ok := promptForKey(scanner, "hll_", "Unesite ključ za HLL ili nazad za povratak:")
							if !ok {
								continue hllLoop
							}

							if _, found := loadFromStorage(key, memtable, blockManager, compressionDictionary); found {
								fmt.Println("Ključ već postoji")
								continue
							}

							var p uint8
							fmt.Println("Unesite preciznost (4 - 16):")
							for {
								scanner.Scan()
								input := scanner.Text()
								val, err := strconv.Atoi(input)
								if err != nil || val < int(Types.HLL_MIN_PRECISION) || val > int(Types.HLL_MAX_PRECISION) {
									fmt.Println("Preciznost mora biti između 4 i 16")
									continue
								}
								p = uint8(val)
								break
							}

							hll := Types.NewHLL(p, key)
							serialized, err := hll.SerializeHLL()
							if err != nil {
								fmt.Println("Greška pri serijalizaciji HLL:", err)
								continue
							}

							writeToStorage(key, serialized, false, blockSize, blockManager, memtable, summarySampleRate, oneFile, hasCompression)
							fmt.Println("Novi HLL uspešno napravljen i stavljen u MemTable")

						} else if operacijaHLL == "dodaj" {
							if !RateLimit.CheckRateLimit(tokenBucketKey, memtable, blockManager, blockSize, summarySampleRate, oneFile, hasCompression, compressionDictionary) {
								continue
							}
							key, ok := promptForKey(scanner, "hll_", "Unesite ključ za HLL kojem će se dodati element ili ukucajte nazad da se vratite na odabir operacije:")
							if !ok {
								continue hllLoop
							}

							value, found := loadFromStorage(key, memtable, blockManager, compressionDictionary)
							if !found {
								fmt.Println("Nema takvog ključa")
								continue
							}

							hll, err := Types.DeserializeHLL(value)
							if err != nil {
								fmt.Println("Greška pri deserializaciji:", err)
								continue
							}

							element := promptForNonEmptyString(scanner, "Unesite željeni element:")
							hll.Add([]byte(element))

							serialized, err := hll.SerializeHLL()
							if err != nil {
								fmt.Println("Greška pri serializaciji:", err)
								continue
							}

							writeToStorage(hll.Key, serialized, false, blockSize, blockManager, memtable, summarySampleRate, oneFile, hasCompression)
							fmt.Println("Element uspešno dodat u HLL.")

						} else if operacijaHLL == "kardinalitet" {
							if !RateLimit.CheckRateLimit(tokenBucketKey, memtable, blockManager, blockSize, summarySampleRate, oneFile, hasCompression, compressionDictionary) {
								continue
							}
							key, ok := promptForKey(scanner, "hll_", "Unesite ključ za HLL:")
							if !ok {
								continue hllLoop
							}

							value, found := loadFromStorage(key, memtable, blockManager, compressionDictionary)
							if !found {
								fmt.Println("Nema takvog ključa.")
								continue
							}

							hll, err := Types.DeserializeHLL(value)
							if err != nil {
								fmt.Println("Greška pri deserializaciji:", err)
								continue
							}

							estimate := hll.Estimate()
							fmt.Printf("Procenjen broj različitih elemenata: %.0f\n", estimate)

						} else if operacijaHLL == "izbrisi" {
							if !RateLimit.CheckRateLimit(tokenBucketKey, memtable, blockManager, blockSize, summarySampleRate, oneFile, hasCompression, compressionDictionary) {
								continue
							}
							key, ok := promptForKey(scanner, "hll_", "Unesite ključ za HLL koji se briše ili nazad:")
							if !ok {
								continue hllLoop
							}

							if _, found := loadFromStorage(key, memtable, blockManager, compressionDictionary); !found {
								fmt.Println("Ne postoji HLL sa tim ključem.")
								continue
							}

							writeToStorage(key, []byte(""), true, blockSize, blockManager, memtable, summarySampleRate, oneFile, hasCompression)
							fmt.Println("HLL uspešno obrisan.")

						} else if operacijaHLL == "nazad" {
							break hllLoop
						} else {
							fmt.Println("Nepostojeća operacija za HLL")
						}
					}
				} else if operacijaTypes == "SH" {

					for {
						fmt.Println("Unesite dodaj za dodavanje novog fingerprinta, udaljenost za hemingovu udaljenst izmedju dva fingerprinta, ili nazad za povratak: ")
						scanner.Scan()
						input := scanner.Text()

						if input == "nazad" {
							break
						} else if input == "dodaj" {
							if !RateLimit.CheckRateLimit(tokenBucketKey, memtable, blockManager, blockSize, summarySampleRate, oneFile, hasCompression, compressionDictionary) {
								continue
							}
							keySH, ok := promptForKey(scanner, "sh_", "Unesite ključ za fingerprint ili 'nazad':")
							if !ok {
								continue
							}

							if _, found := loadFromStorage(keySH, memtable, blockManager, compressionDictionary); found {
								fmt.Println("Ključ već postoji!")
								continue
							}

							text := promptForNonEmptyString(scanner, "Unesite tekst:")
							fingerprint := Types.ComputeFingerprint(text)
							fmt.Println("Fingerprint:", fingerprint)

							writeToStorage(keySH, []byte(fingerprint), false, blockSize, blockManager, memtable, summarySampleRate, oneFile, hasCompression)
							fmt.Println("Fingerprint uspešno dodat.")
							continue
						} else if input == "udaljenost" {
							if !RateLimit.CheckRateLimit(tokenBucketKey, memtable, blockManager, blockSize, summarySampleRate, oneFile, hasCompression, compressionDictionary) {
								continue
							}
							// Učitaj prvi fingerprint
							key1, ok := promptForKey(scanner, "sh_", "Unesite prvi ključ za fingerprint ili 'nazad':")
							if !ok {
								continue
							}
							value1, found1 := loadFromStorage(key1, memtable, blockManager, compressionDictionary)
							if !found1 {
								fmt.Println("Prvi ključ ne postoji.")
								continue
							}

							// Učitaj drugi fingerprint
							key2, ok := promptForKey(scanner, "sh_", "Unesite drugi ključ za fingerprint ili 'nazad':")
							if !ok {
								continue
							}
							value2, found2 := loadFromStorage(key2, memtable, blockManager, compressionDictionary)
							if !found2 {
								fmt.Println("Drugi ključ ne postoji.")
								continue
							}

							dist := Types.HammingDistance(string(value1), string(value2))
							fmt.Println("Hemingova udaljenost:", dist)
							continue
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

							keyBF, ok := promptForKey(scanner, "bf_", "Unesite ključ za Bloom Filter ili ukucajte nazad")
							if !ok {
								continue bfLoop
							}

							value, found := loadFromStorage(keyBF, memtable, blockManager, compressionDictionary)
							if !found {
								fmt.Println("Nema takvog ključa.")
								continue
							}

							bf, err := Types.DeserializeBFWK(value)
							if err != nil {
								fmt.Println("Greška pri deserializaciji:", err)
								continue
							}

							element := promptForNonEmptyString(scanner, "Unesite željeni element:")
							bf.BF.Add(element)

							serialized, err := bf.SerializeBFWK()
							if err != nil {
								fmt.Println("Greška pri serializaciji:", err)
								continue
							}

							writeToStorage(bf.Key, serialized, false, blockSize, blockManager, memtable, summarySampleRate, oneFile, hasCompression)
							fmt.Println("Element uspešno dodat.")
							continue bfLoop

						} else if operacija == "novi" {
							if !RateLimit.CheckRateLimit(tokenBucketKey, memtable, blockManager, blockSize, summarySampleRate, oneFile, hasCompression, compressionDictionary) {
								continue
							}

							keyBF, ok := promptForKey(scanner, "bf_", "Unesite ključ za Bloom Filter ili 'nazad'")
							if !ok {
								continue bfLoop
							}

							if _, found := loadFromStorage(keyBF, memtable, blockManager, compressionDictionary); found {
								fmt.Println("Ključ već postoji.")
								continue
							}

							var n int
							var p float64

							fmt.Println("Unesite očekivani broj elemenata:")
							for {
								scanner.Scan()
								if v, err := strconv.Atoi(scanner.Text()); err == nil && v > 0 {
									n = v
									break
								}
								fmt.Println("Mora biti pozitivan ceo broj")
							}

							fmt.Println("Unesite dozvoljenu stopu false positive (0 < p < 1):")
							for {
								scanner.Scan()
								if v, err := strconv.ParseFloat(scanner.Text(), 64); err == nil && v > 0 && v < 1 {
									p = v
									break
								}
								fmt.Println("Broj mora biti između 0 i 1")
							}

							bf := sstable.NewBloomFilter(n, p)
							bfwk := Types.BloomFilterWithKey{Key: keyBF, BF: bf}

							serialized, err := bfwk.SerializeBFWK()
							if err != nil {
								fmt.Println("Greška pri serijalizaciji:", err)
								continue
							}

							writeToStorage(keyBF, serialized, false, blockSize, blockManager, memtable, summarySampleRate, oneFile, hasCompression)
							fmt.Println("Novi BF uspešno napravljen i stavljen u MemTable.")
							continue bfLoop

						} else if operacija == "izbrisi" {
							if !RateLimit.CheckRateLimit(tokenBucketKey, memtable, blockManager, blockSize, summarySampleRate, oneFile, hasCompression, compressionDictionary) {
								continue
							}

							keyBF, ok := promptForKey(scanner, "bf_", "Unesite ključ za BF koji se briše ili nazad:")
							if !ok {
								continue bfLoop
							}

							if _, found := loadFromStorage(keyBF, memtable, blockManager, compressionDictionary); !found {
								fmt.Println("Ne postoji BF sa tim ključem.")
								continue
							}

							writeToStorage(keyBF, []byte(""), true, blockSize, blockManager, memtable, summarySampleRate, oneFile, hasCompression)
							fmt.Println("BF uspešno obrisan.")
							continue bfLoop

						} else if operacija == "postoji" {
							if !RateLimit.CheckRateLimit(tokenBucketKey, memtable, blockManager, blockSize, summarySampleRate, oneFile, hasCompression, compressionDictionary) {
								continue
							}

							keyBF, ok := promptForKey(scanner, "bf_", "Unesite ključ za BF ili 'nazad'")
							if !ok {
								continue bfLoop
							}

							value, found := loadFromStorage(keyBF, memtable, blockManager, compressionDictionary)
							if !found {
								fmt.Println("Nema takvog ključa.")
								continue
							}

							bf, err := Types.DeserializeBFWK(value)
							if err != nil {
								fmt.Println("Greška pri deserializaciji:", err)
								continue
							}

							element := promptForNonEmptyString(scanner, "Unesite željeni element:")
							if bf.BF.Check(element) {
								fmt.Println("Element postoji.")
							} else {
								fmt.Println("Element ne postoji.")
							}
							continue bfLoop

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
