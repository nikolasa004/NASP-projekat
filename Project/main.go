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

	fmt.Println(">>> Enter Key: ")
	scanner.Scan()
	key := scanner.Text()

	for _, prefix := range disallowedPrefixes {
		if strings.HasPrefix(key, prefix) {
			fmt.Printf("[Error] Key cannot start with reserved system prefix: '%s'\n", prefix)
			return "", false
		}
	}

	return key, true
}

func promptForKey(scanner *bufio.Scanner, prefix, msg string) (string, bool) {
	for {
		fmt.Printf(">>> %s (or type 'back'): ", msg)
		scanner.Scan()
		key := scanner.Text()
		if key == "back" {
			return "", false
		}
		if !strings.HasPrefix(key, prefix) {
			fmt.Printf("[Invalid Format] Key must start with the prefix '%s'\n", prefix)
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

	records := BlockManager.CreateBinaryRecordsWithFragmentation(isTombstone, key, value, blockSize, false, 0)
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
		fmt.Println("[Input Error] Field cannot be empty. Please try again.")
	}
}

func main() {
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
		StartTime: time.Now().UnixMicro(),
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
			fmt.Println("[RateLimit] Error deserializing existing bucket. Resetting...")
			needToWrite = true
		} else {
			if existingState.Capacity != config.RateLimiter.Capacity || existingState.Interval != config.RateLimiter.Interval {
				fmt.Printf("[RateLimit] Config changed! Old: (cap=%d, int=%d) | New: (cap=%d, int=%d)\n",
					existingState.Capacity, existingState.Interval,
					config.RateLimiter.Capacity, config.RateLimiter.Interval)
				needToWrite = true //treba true
			}
		}
	} else {
		fmt.Println("[RateLimit] Token bucket not found. Initializing new state...")
		needToWrite = true //treba true
	}

	if needToWrite {
		serialized, err := RateLimit.SerializeRateLimit(rateLimitState)
		if err != nil {
			fmt.Println("[Fatal] Error serializing token bucket:", err)
			os.Exit(1)
		}
		sysRecords := BlockManager.CreateBinaryRecordsWithFragmentation(false, tokenBucketKey, serialized, blockSize, false, 0)
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

	fmt.Println("\n========================================")
    fmt.Println("   Welcome to KV-Engine CLI v1.0")
    fmt.Println("========================================")

	for {
		fmt.Println("\nMain Menu: [GET] [PUT] [DELETE] [SCAN] [TYPES] [VERIFY] [/ (Exit)]")
		fmt.Print("Action selection > ")
		var operacija string
		scanner.Scan()
		operacija = scanner.Text()
		if operacija == "GET" {

			if !RateLimit.CheckRateLimit(tokenBucketKey, memtable, blockManager, blockSize, summarySampleRate, oneFile, hasCompression, compressionDictionary) {
				fmt.Println("[RateLimit] Request rejected: Too many requests.")
				continue
			}
			kljuc, ok := promptForCustomKey(scanner)
			if !ok {
				continue
			}
			record, found := memtable.GetRecord(kljuc)
			if found {
				fmt.Println("[Found in Memtable] Value:", string(record.Value))
				continue

			} else if record != nil && !found { // ako je pronađen zapis ali je izbrisan
				fmt.Println("[Status] Record found but it is marked as DELETED (Tombstone).")
				continue
			}

			if value, found := cache.GetFromCache(kljuc); found {
				fmt.Println("[Found in Cache] Value:", string(value))
				continue
			}
			// Ako nije u memtable-u i kešu, tražimo u SSTable-ima, radi i sa kompresovanim ključevima i pravim

			SSrecord, keyFound := sstable.FindInSstables(kljuc, *blockManager, compressionDictionary)
			if keyFound {
				fmt.Println("[Found in SSTable] Value:", string(SSrecord.Value))
				cache.PutInCache(kljuc, SSrecord.Value)
				continue
			}

			fmt.Println("[Not Found] Key does not exist in the system.")
		} else if operacija == "PUT" {

			if !RateLimit.CheckRateLimit(tokenBucketKey, memtable, blockManager, blockSize, summarySampleRate, oneFile, hasCompression, compressionDictionary) {
				fmt.Println("[RateLimit] Request rejected.")
				continue
			}

			kljuc, ok := promptForCustomKey(scanner)
			if !ok {
				continue
			}
			fmt.Print("Enter Value: ")
			scanner.Scan()
			vrednost := scanner.Text()

			records := BlockManager.CreateBinaryRecordsWithFragmentation(false, kljuc, []byte(vrednost), blockSize, false, 0)
			for _, record := range records {
				blockManager.PutRecord(record)
			}
			flushedRecords, flushed := memtable.PutRecord(false, kljuc, vrednost, blockManager, len(blockManager.GetSegments())-1)
			if flushed {
				fmt.Println("[System] Memtable full. Flushing to SSTable...")
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
			fmt.Println("[Success] Key successfully stored.")
		} else if operacija == "DELETE" {
			if !RateLimit.CheckRateLimit(tokenBucketKey, memtable, blockManager, blockSize, summarySampleRate, oneFile, hasCompression, compressionDictionary) {
				continue
			}
			kljuc, ok := promptForCustomKey(scanner)
			if !ok {
				continue
			}

			//napraviti zapis sa tombstone = true
			records := BlockManager.CreateBinaryRecordsWithFragmentation(true, kljuc, []byte(""), blockSize, false, 0)
			for _, record := range records {
				blockManager.PutRecord(record)
			}
			flushedRecords, flushed := memtable.PutRecord(true, kljuc, "", blockManager, len(blockManager.GetSegments())-1)
			if flushed {
				if hasCompression {
					compressedRecords := compressionDictionary.StoreKeys(blockManager, flushedRecords) // čuvanje ključeva u rečniku
					sstable.WriteSstable(compressedRecords, summarySampleRate, false, oneFile, blockManager, hasCompression, 0)
				} else {
					sstable.WriteSstable(flushedRecords, summarySampleRate, false, oneFile, blockManager, hasCompression, 0) // upisivanje u SSTable
				}
			}
			fmt.Println("[Success] Key marked for deletion.")
		} else if operacija == "VERIFY" {
			fmt.Print("Enter SSTable TOC filename to verify Merkle Tree: ")
			scanner.Scan()
			userInput := scanner.Text()

			err := sstable.VerifySstableMerkle(userInput, blockManager)
			if err != nil {
				fmt.Println("[Verification Failed]", err)
			} else {
                fmt.Println("[Success] SSTable integrity verified via Merkle Tree.")
            }
		} else if operacija == "SCAN" {
			if !RateLimit.CheckRateLimit(tokenBucketKey, memtable, blockManager, blockSize, summarySampleRate, oneFile, hasCompression, compressionDictionary) {
				continue
			}
			for {
				reader := bufio.NewReader(os.Stdin)
				fmt.Println("\n--- Scan Configuration ---")
                fmt.Println("1. prefix  - Linear prefix scan")
                fmt.Println("2. range   - Linear range scan")
                fmt.Println("3. iprefix - Iterative prefix scan")
                fmt.Println("4. irange  - Iterative range scan")
                fmt.Println("5. back    - Return to main menu")
                fmt.Print("Select scan type > ")
				choiceInput, _ := reader.ReadString('\n')
				choice := strings.TrimSpace(choiceInput)
				if choice == "back" {
					break
				}
				var prefix, lowerBound, upperBound string
				useIterator := false
				pageSize := 0
				switch choice {
				case "prefix":
					for {
						fmt.Print("Enter prefix: ")
						input, _ := reader.ReadString('\n')
						prefix = strings.TrimSpace(input)
						if prefix != "" {
							break
						}
						fmt.Println("[Error] Prefix cannot be empty.")
					}
				case "range":
					for {
						fmt.Print("Enter lower bound: ")
						input, _ := reader.ReadString('\n')
						lowerBound = strings.TrimSpace(input)
						if lowerBound != "" {
							break
						}
						fmt.Println("[Error] Lower bound cannot be empty.")
					}
					for {
						fmt.Print("Enter upper bound: ")
						input, _ := reader.ReadString('\n')
						upperBound = strings.TrimSpace(input)
						if upperBound != "" {
							break
						}
						fmt.Println("[Error] Upper bound cannot be empty.")
					}
				case "iprefix":
					for {
						fmt.Print("Enter prefix: ")
						input, _ := reader.ReadString('\n')
						prefix = strings.TrimSpace(input)
						if prefix != "" {
							break
						}
						fmt.Println("[Error] Prefix cannot be empty.")
					}
					useIterator = true
					pageSize = 1

				case "irange":
					for {
						fmt.Print("Enter lower bound: ")
						input, _ := reader.ReadString('\n')
						lowerBound = strings.TrimSpace(input)
						if lowerBound != "" {
							break
						}
						fmt.Println("[Error] Lower bound cannot be empty.")
					}
					for {
						fmt.Print("Enter upper bound: ")
						input, _ := reader.ReadString('\n')
						upperBound = strings.TrimSpace(input)
						if upperBound != "" {
							break
						}
						fmt.Println("[Error] Upper bound cannot be empty.")
					}
					useIterator = true
					pageSize = 1
				default:
					fmt.Println("[Error] Invalid selection.")
                    continue
				}

				if !useIterator {
					for {
						fmt.Print("Enter Page Size (positive integer): ")
						input, _ := reader.ReadString('\n')
						input = strings.TrimSpace(input)
						num, err := strconv.Atoi(input)
						if err == nil && num > 0 {
							pageSize = num
							break
						}
						fmt.Println("[Error] Please enter a valid positive number.")
					}
				}
				Scanners.Scan(prefix, lowerBound, upperBound, memtable, blockManager, hasCompression, compressionDictionary, pageSize, scannerCacheSize)
				break
			}
		} else if operacija == "/" {
			fmt.Println("[System] Flushing unsaved buffers... Goodbye!")
			blockManager.FlushUnsaved() //ako ima nesto u baferu a nema u wal-u, upisuje se u wal
			break
		} else if operacija == "TYPES" {
			for {
				fmt.Println("\n--- Advanced Data Structures ---")
                fmt.Println("[CMS] Count-Min Sketch | [HLL] HyperLogLog | [SH] SimHash | [BF] Bloom Filter | [BACK]")
                fmt.Print("Select type > ")
				var operacijaTypes string
				scanner.Scan()
				operacijaTypes = scanner.Text()

				if operacijaTypes == "CMS" {
				cmsLoop:
					for {
						fmt.Println("\n[CMS Menu] Options: add, delete, count, new, back")
                        fmt.Print("CMS Action > ")
						scanner.Scan()
						operacija := scanner.Text()

						if operacija == "add" {
							if !RateLimit.CheckRateLimit(tokenBucketKey, memtable, blockManager, blockSize, summarySampleRate, oneFile, hasCompression, compressionDictionary) {
								continue
							}
							keyCMS, ok := promptForKey(scanner, "cms_", "Enter CMS key")
							if !ok {
								continue cmsLoop
							}

							value, found := loadFromStorage(keyCMS, memtable, blockManager, compressionDictionary)
							if !found {
								fmt.Println("[Error] CMS not found.")
								continue
							}

							cms, err := Types.Deserialize(value)
							if err != nil {
								fmt.Println("[Error] Deserialization error:", err)
								continue
							}
							element := promptForNonEmptyString(scanner, "Enter element to add: ")

							cms.Add([]byte(element))
							serialized, err := cms.Serialize()
							if err != nil {
								fmt.Println("[Error] Serialization error:", err)
								continue
							}
							writeToStorage(cms.Key, serialized, false, blockSize, blockManager, memtable, summarySampleRate, oneFile, hasCompression)
							fmt.Println("Element successfully added to CMS.")
							continue cmsLoop

						} else if operacija == "new" {
							if !RateLimit.CheckRateLimit(tokenBucketKey, memtable, blockManager, blockSize, summarySampleRate, oneFile, hasCompression, compressionDictionary) {
								continue
							}

							keyCMS, ok := promptForKey(scanner, "cms_", "Enter CMS key or 'back' to return:")
							if !ok {
								continue cmsLoop
							}

							if _, found := loadFromStorage(keyCMS, memtable, blockManager, compressionDictionary); found {
								fmt.Println("[Error] Key already exists.")
								continue
							}

							var epsilon, delta float64
							fmt.Println("Enter epsilon:")
							for {
								scanner.Scan()
								input := scanner.Text()
								var err error
								epsilon, err = strconv.ParseFloat(input, 64)
								if err != nil || epsilon <= 0 {
									fmt.Println("[Error] Epsilon must be a positive number.")
									continue
								}
								break
							}

							fmt.Println("Enter delta:")
							for {
								scanner.Scan()
								input := scanner.Text()
								var err error
								delta, err = strconv.ParseFloat(input, 64)
								if err != nil || delta <= 0 {
									fmt.Println("[Error] Delta must be a positive number.")
									continue
								}
								break
							}

							m := Types.CalculateM(epsilon)
							k := Types.CalculateK(delta)
							cms := Types.NewCountMinSketch(uint32(k), uint32(m), keyCMS)

							serialized, err := cms.Serialize()
							if err != nil {
								fmt.Println("[Error] Serialization error:", err)
								continue
							}

							writeToStorage(keyCMS, serialized, false, blockSize, blockManager, memtable, summarySampleRate, oneFile, hasCompression)
							fmt.Println("New CMS successfully created and added to MemTable")
							continue cmsLoop

						} else if operacija == "delete" {
							if !RateLimit.CheckRateLimit(tokenBucketKey, memtable, blockManager, blockSize, summarySampleRate, oneFile, hasCompression, compressionDictionary) {
								continue
							}

							keyCMS, ok := promptForKey(scanner, "cms_", "Enter CMS key to delete or 'back' to return:")
							if !ok {
								continue cmsLoop
							}

							if _, found := loadFromStorage(keyCMS, memtable, blockManager, compressionDictionary); !found {
								fmt.Println("[Error] CMS with that key does not exist.")
								continue
							}

							writeToStorage(keyCMS, []byte(""), true, blockSize, blockManager, memtable, summarySampleRate, oneFile, hasCompression)
							fmt.Println("CMS successfully deleted.")
							continue cmsLoop

						} else if operacija == "prebroj" {
							if !RateLimit.CheckRateLimit(tokenBucketKey, memtable, blockManager, blockSize, summarySampleRate, oneFile, hasCompression, compressionDictionary) {
								continue
							}

							keyCMS, ok := promptForKey(scanner, "cms_", "Enter CMS key:")
							if !ok {
								continue cmsLoop
							}

							value, found := loadFromStorage(keyCMS, memtable, blockManager, compressionDictionary)
							if !found {
								fmt.Println("[Error] No such CMS key.")
								continue
							}

							cms, err := Types.Deserialize(value)
							if err != nil {
								fmt.Println("[Error] Deserialization error:", err)
								continue
							}

							element := promptForNonEmptyString(scanner, "Enter element to count:")
							count := cms.Count([]byte(element))
							fmt.Printf("Element '%s' se pojavljuje ~ %d puta (procena).\n", element, count)
							continue cmsLoop

						} else if operacija == "back" {
							break cmsLoop
						} else {
							fmt.Println("[Error] Unknown operation.")
						}
					}

				} else if operacijaTypes == "HLL" {
				hllLoop:
					for {
						fmt.Println("\n[HLL Menu] Options: new, add, estimate, delete, back")
                        fmt.Print("HLL Action > ")
						scanner.Scan()
						operacijaHLL := scanner.Text()

						if operacijaHLL == "new" {
							if !RateLimit.CheckRateLimit(tokenBucketKey, memtable, blockManager, blockSize, summarySampleRate, oneFile, hasCompression, compressionDictionary) {
								continue
							}
							key, ok := promptForKey(scanner, "hll_", "Enter HLL key or 'back' to return:")
							if !ok {
								continue hllLoop
							}

							if _, found := loadFromStorage(key, memtable, blockManager, compressionDictionary); found {
								fmt.Println("[Error] Key already exists")
								continue
							}

							var p uint8
							fmt.Print("Enter Precision (4 - 16): ")
							for {
								scanner.Scan()
								input := scanner.Text()
								val, err := strconv.Atoi(input)
								if err != nil || val < int(Types.HLL_MIN_PRECISION) || val > int(Types.HLL_MAX_PRECISION) {
									fmt.Println("[Error] Precision must be between 4 and 16")
									continue
								}
								p = uint8(val)
								break
							}

							hll := Types.NewHLL(p, key)
							serialized, err := hll.SerializeHLL()
							if err != nil {
								fmt.Println("[Error] Serialization error:", err)
								continue
							}

							writeToStorage(key, serialized, false, blockSize, blockManager, memtable, summarySampleRate, oneFile, hasCompression)
							fmt.Println("New HLL successfully created and added to MemTable")

						} else if operacijaHLL == "dodaj" {
							if !RateLimit.CheckRateLimit(tokenBucketKey, memtable, blockManager, blockSize, summarySampleRate, oneFile, hasCompression, compressionDictionary) {
								continue
							}
							key, ok := promptForKey(scanner, "hll_", "Enter HLL key to add element to or 'back' to return:")
							if !ok {
								continue hllLoop
							}

							value, found := loadFromStorage(key, memtable, blockManager, compressionDictionary)
							if !found {
								fmt.Println("[Error] No such HLL key.")
								continue
							}

							hll, err := Types.DeserializeHLL(value)
							if err != nil {
								fmt.Println("[Error] Deserialization error:", err)
								continue
							}

							element := promptForNonEmptyString(scanner, "Enter element to add:")
							hll.Add([]byte(element))

							serialized, err := hll.SerializeHLL()
							if err != nil {
								fmt.Println("[Error] Serialization error:", err)
								continue
							}

							writeToStorage(hll.Key, serialized, false, blockSize, blockManager, memtable, summarySampleRate, oneFile, hasCompression)
							fmt.Println("Element successfully added to HLL.")

						} else if operacijaHLL == "estimate" {
							if !RateLimit.CheckRateLimit(tokenBucketKey, memtable, blockManager, blockSize, summarySampleRate, oneFile, hasCompression, compressionDictionary) {
								continue
							}
							key, ok := promptForKey(scanner, "hll_", "Enter HLL key to estimate cardinality:")
							if !ok {
								continue hllLoop
							}

							value, found := loadFromStorage(key, memtable, blockManager, compressionDictionary)
							if !found {
								fmt.Println("[Error] No such HLL key.")
								continue
							}

							hll, err := Types.DeserializeHLL(value)
							if err != nil {
								fmt.Println("[Error] Deserialization error:", err)
								continue
							}

							estimate := hll.Estimate()
							fmt.Printf("Estimated number of distinct elements: %.0f\n", estimate)

						} else if operacijaHLL == "delete" {
							if !RateLimit.CheckRateLimit(tokenBucketKey, memtable, blockManager, blockSize, summarySampleRate, oneFile, hasCompression, compressionDictionary) {
								continue
							}
							key, ok := promptForKey(scanner, "hll_", "Enter HLL key to delete or 'back' to return:")
							if !ok {
								continue hllLoop
							}

							if _, found := loadFromStorage(key, memtable, blockManager, compressionDictionary); !found {
								fmt.Println("[Error] No such HLL key.")
								continue
							}

							writeToStorage(key, []byte(""), true, blockSize, blockManager, memtable, summarySampleRate, oneFile, hasCompression)
							fmt.Println("HLL successfully deleted.")

						} else if operacijaHLL == "nazad" {
							break hllLoop
						} else {
							fmt.Println("Non-existent operation for HLL")
						}
					}
				} else if operacijaTypes == "SH" {

					for {
						fmt.Println("\n[SimHash Menu] Options: add, distance, back")
                        fmt.Print("SimHash Action > ")
						scanner.Scan()
						input := scanner.Text()

						if input == "back" {
							break
						} else if input == "add" {
							if !RateLimit.CheckRateLimit(tokenBucketKey, memtable, blockManager, blockSize, summarySampleRate, oneFile, hasCompression, compressionDictionary) {
								continue
							}
							keySH, ok := promptForKey(scanner, "sh_", "Enter fingerprint key")
							if !ok {
								continue
							}

							if _, found := loadFromStorage(keySH, memtable, blockManager, compressionDictionary); found {
								fmt.Println("[Error] Key already exists!")
								continue
							}

							text := promptForNonEmptyString(scanner, "Enter text:")
							fingerprint := Types.ComputeFingerprint(text)
							fmt.Println("Fingerprint:", fingerprint)

							writeToStorage(keySH, []byte(fingerprint), false, blockSize, blockManager, memtable, summarySampleRate, oneFile, hasCompression)
							fmt.Println("Fingerprint successfully added.")
							continue
						} else if input == "distance" {
							if !RateLimit.CheckRateLimit(tokenBucketKey, memtable, blockManager, blockSize, summarySampleRate, oneFile, hasCompression, compressionDictionary) {
								continue
							}
							// Učitaj prvi fingerprint
							key1, ok := promptForKey(scanner, "sh_", "Enter first fingerprint key or 'back':")
							if !ok {
								continue
							}
							value1, found1 := loadFromStorage(key1, memtable, blockManager, compressionDictionary)
							if !found1 {
								fmt.Println("[Error] First key does not exist.")
								continue
							}

							// Učitaj drugi fingerprint
							key2, ok := promptForKey(scanner, "sh_", "Enter second fingerprint key or 'back':")
							if !ok {
								continue
							}
							value2, found2 := loadFromStorage(key2, memtable, blockManager, compressionDictionary)
							if !found2 {
								fmt.Println("[Error] Second key does not exist.")
								continue
							}

							dist := Types.HammingDistance(string(value1), string(value2))
							fmt.Println("Hemingova udaljenost:", dist)
							continue
						} else {
							fmt.Println("Non-existent operation")
						}
					}
				} else if operacijaTypes == "BF" {
				bfLoop:
					for {
						fmt.Println("\n[Bloom Filter Menu] Options: add, check, delete, new, back")
                        fmt.Print("BF Action > ")
						scanner.Scan()
						operacija := scanner.Text()

						if operacija == "add" {
							if !RateLimit.CheckRateLimit(tokenBucketKey, memtable, blockManager, blockSize, summarySampleRate, oneFile, hasCompression, compressionDictionary) {
								continue
							}

							keyBF, ok := promptForKey(scanner, "bf_", "Enter Bloom Filter key")
							if !ok {
								continue bfLoop
							}

							value, found := loadFromStorage(keyBF, memtable, blockManager, compressionDictionary)
							if !found {
								fmt.Println("[Error] Key does not exist.")
								continue
							}

							bf, err := Types.DeserializeBFWK(value)
							if err != nil {
								fmt.Println("[Error] Deserialization error:", err)
								continue
							}

							element := promptForNonEmptyString(scanner, "Enter element to add:")
							bf.BF.Add(element)

							serialized, err := bf.SerializeBFWK()
							if err != nil {
								fmt.Println("[Error] Serialization error:", err)
								continue
							}

							writeToStorage(bf.Key, serialized, false, blockSize, blockManager, memtable, summarySampleRate, oneFile, hasCompression)
							fmt.Println("Element successfully added.")
							continue bfLoop

						} else if operacija == "new" {
							if !RateLimit.CheckRateLimit(tokenBucketKey, memtable, blockManager, blockSize, summarySampleRate, oneFile, hasCompression, compressionDictionary) {
								continue
							}

							keyBF, ok := promptForKey(scanner, "bf_", "Enter Bloom Filter key")
							if !ok {
								continue bfLoop
							}

							if _, found := loadFromStorage(keyBF, memtable, blockManager, compressionDictionary); found {
								fmt.Println("[Error] Key already exists.")
								continue
							}

							var n int
							var p float64

							fmt.Println("Enter expected number of elements (n):")
							for {
								scanner.Scan()
								if v, err := strconv.Atoi(scanner.Text()); err == nil && v > 0 {
									n = v
									break
								}
								fmt.Println("Number must be a positive integer.")
							}

							fmt.Println("Enter allowed false positive rate (0 < p < 1):")
							for {
								scanner.Scan()
								if v, err := strconv.ParseFloat(scanner.Text(), 64); err == nil && v > 0 && v < 1 {
									p = v
									break
								}
								fmt.Println("Number must be between 0 and 1")
							}

							bf := sstable.NewBloomFilter(n, p)
							bfwk := Types.BloomFilterWithKey{Key: keyBF, BF: bf}

							serialized, err := bfwk.SerializeBFWK()
							if err != nil {
								fmt.Println("[Error] Serialization error:", err)
								continue
							}

							writeToStorage(keyBF, serialized, false, blockSize, blockManager, memtable, summarySampleRate, oneFile, hasCompression)
							fmt.Println("New BF successfully created and added to MemTable.")
							continue bfLoop

						} else if operacija == "delete" {
							if !RateLimit.CheckRateLimit(tokenBucketKey, memtable, blockManager, blockSize, summarySampleRate, oneFile, hasCompression, compressionDictionary) {
								continue
							}

							keyBF, ok := promptForKey(scanner, "bf_", "Enter Bloom Filter key to delete")
							if !ok {
								continue bfLoop
							}

							if _, found := loadFromStorage(keyBF, memtable, blockManager, compressionDictionary); !found {
								fmt.Println("[Error] Key does not exist.")
								continue
							}

							writeToStorage(keyBF, []byte(""), true, blockSize, blockManager, memtable, summarySampleRate, oneFile, hasCompression)
							fmt.Println("BF successfully deleted.")
							continue bfLoop

						} else if operacija == "exists" {
							if !RateLimit.CheckRateLimit(tokenBucketKey, memtable, blockManager, blockSize, summarySampleRate, oneFile, hasCompression, compressionDictionary) {
								continue
							}

							keyBF, ok := promptForKey(scanner, "bf_", "Enter Bloom Filter key")
							if !ok {
								continue bfLoop
							}

							value, found := loadFromStorage(keyBF, memtable, blockManager, compressionDictionary)
							if !found {
								fmt.Println("[Error] Key does not exist.")
								continue
							}

							bf, err := Types.DeserializeBFWK(value)
							if err != nil {
								fmt.Println("[Error] Deserialization error:", err)
								continue
							}

							element := promptForNonEmptyString(scanner, "Enter element to check:")
							if bf.BF.Check(element) {
								fmt.Println("Element possibly exists (could be a false positive).")
							} else {
								fmt.Println("Element does not exist.")
							}
							continue bfLoop

						} else if operacija == "back" {
							break bfLoop
						} else {
							fmt.Println("Non-existent operation")
						}
					}

				} else if operacijaTypes == "back" {
					break // izlaz iz Types petlje
				} else {
					fmt.Println("Non-existent operation")
				}
			}
		} else {
			fmt.Println("Non-existent operation")
		}
	}
}
