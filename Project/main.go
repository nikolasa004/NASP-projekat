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
	"strconv"
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
	//OuterLoop:
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
				fmt.Println("Sa čime želite da radite? Ukucajte cms za Count Min Sketch, hll za HyperLogLog, sh za SimHash, bl za BloomFilter ili nazad da izadjete iz biranja rada sa dostupnim strukturama: ")
				var operacijaTypes string
				scanner.Scan()
				operacijaTypes = scanner.Text()

				if operacijaTypes == "cms" {
				cmsLoop:
					for {
						fmt.Println("Unesite željenju operaciju: dodaj, izbrisi, prebroj, novi, nazad")
						scanner.Scan()
						operacija := scanner.Text()

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
									//fmt.Println("CMS.K =", cms.K, "CMS.M =", cms.M)
									//fmt.Println("Prvi red:", cms.Matrix[0])
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
										serialized, err := cms.Serialize()
										if err != nil {
											fmt.Println("Greška pri serializaciji: ", err)
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

								SSrecord, found := sstable.FindInSstables(kljucCMS, *blockManager)
								if found {
									value := SSrecord.Value
									cms, err := Types.Deserialize(value)
									if err != nil {
										fmt.Println("Greška pri deserializaciji: ", err)
									} else {
										var newElement string
										for {
											fmt.Println("Unesite željeni element: ")
											scanner.Scan()
											newElement = scanner.Text()
											if newElement == "" {
												fmt.Println("Element ne može biti prazan string!")
												continue
											}
											break
										}
										cms.Add([]byte(newElement))
										serialized, err := cms.Serialize()
										if err != nil {
											fmt.Println("Greška pri serializaciji: ", err)
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
								fmt.Println("Nema takvog kljuca")
							}

						} else if operacija == "novi" {
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
								if _, found := sstable.FindInSstables(keyCMS, *blockManager); found {
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
									sstable.WriteSstable(flushedRecords, summarySampleRate, false, oneFile, blockManager)
								}
								fmt.Println("Novi cms uspešno napravljen i stavljen u memtable")
								continue cmsLoop
							}

						} else if operacija == "izbrisi" {
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
									if _, found := sstable.FindInSstables(keyCMS, *blockManager); !found {
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
									sstable.WriteSstable(flushedRecords, summarySampleRate, false, oneFile, blockManager)
								}
								fmt.Println(("CMS uspesno obrisan."))
								continue cmsLoop
							}

						} else if operacija == "prebroj" {
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
								} else if SSrecord, found := sstable.FindInSstables(keyCMS, *blockManager); found {
									cms, err = Types.Deserialize(SSrecord.Value)
									if err != nil {
										fmt.Println("Greska pri deserializaciji CountMinSketcha (SSTable):", err)
										continue
									}
								} else {
									fmt.Println("Nema takvog kljuca.")
									continue
								}
								fmt.Println("Unesite zeljeni element: ")
								scanner.Scan()
								element := scanner.Text()
								if element == "" {
									fmt.Println("Element ne moze biti prazan!")
									continue
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

				} else if operacijaTypes == "hll" {
				hllLoop:
					for {
						fmt.Println("Unesite željenu operaciju za HLL: novi, dodaj, kardinalitet, izbrisi, nazad")
						scanner.Scan()
						operacijaHLL := scanner.Text()

						if operacijaHLL == "novi" {
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
								if _, found := sstable.FindInSstables(keyHLL, *blockManager); found {
									fmt.Println("Ključ već postoji (SSTable)")
									continue
								}
								var p uint8
								fmt.Println("Unesite preciznost:")
								//for {
								scanner.Scan()
								input := scanner.Text()
								val, err := strconv.Atoi(input)
								if err != nil || val < int(Types.HLL_MIN_PRECISION) || val > int(Types.HLL_MAX_PRECISION) {
									fmt.Println("Preciznost mora biti izmedju 4  i 16")
									continue
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
									sstable.WriteSstable(flushedRecords, summarySampleRate, false, oneFile, blockManager)
								}
								fmt.Println("Novi HLL uspešno napravljen i stavljen u memtable")
								continue hllLoop
							}

						} else if operacijaHLL == "dodaj" {
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
								record, found := memtable.GetRecord(kljucHLL)
								if found {
									value := record.Value
									hll, err := Types.DeserializeHLL(value)
									//fmt.Println("CMS.K =", cms.K, "CMS.M =", cms.M)
									//fmt.Println("Prvi red:", cms.Matrix[0])
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
										hll.Add([]byte(noviElement))
										serialized, err := hll.SerializeHLL()
										if err != nil {
											fmt.Println("Greška pri serializaciji: ", err)
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

								SSrecord, found := sstable.FindInSstables(kljucHLL, *blockManager)
								if found {
									value := SSrecord.Value
									hll, err := Types.DeserializeHLL(value)
									if err != nil {
										fmt.Println("Greška pri deserializaciji: ", err)
									} else {
										var newElement string
										for {
											fmt.Println("Unesite željeni element: ")
											scanner.Scan()
											newElement = scanner.Text()
											if newElement == "" {
												fmt.Println("Element ne može biti prazan string!")
												continue
											}
											break
										}
										hll.Add([]byte(newElement))
										serialized, err := hll.SerializeHLL()
										if err != nil {
											fmt.Println("Greška pri serializaciji: ", err)
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
								fmt.Println("Nema takvog kljuca")
							}

						} else if operacijaHLL == "kardinalitet" {
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
								} else if SSrecord, found := sstable.FindInSstables(keyHLL, *blockManager); found {
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
									if _, found := sstable.FindInSstables(keyHLL, *blockManager); !found {
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
									sstable.WriteSstable(flushedRecords, summarySampleRate, false, oneFile, blockManager)
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
				} else if operacijaTypes == "sh" {
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
								if _, found := sstable.FindInSstables(keySH, *blockManager); found {
									fmt.Println("Kljuc vec postoji (SStalbe)!")
									continue
								}
								break
							}
							fmt.Println("Unesite tekst: ")
							scanner.Scan()
							text := scanner.Text()
							fmt.Println("TEXT KOJI SE SALJE U ComputeFingerprint:", text)
							fingerprint := Types.ComputeFingerprint(text)
							fmt.Println("Fingerprint: ", fingerprint)

							records := BlockManager.CreateBinaryRecordsWithFragmentation(false, keySH, []byte(fingerprint), blockSize, false)
							for _, record := range records {
								blockManager.PutRecord(record)
							}
							flushedRecords, flushed := memtable.PutRecord(false, keySH, fingerprint, blockManager, len(blockManager.GetSegments())-1)
							if flushed {
								sstable.WriteSstable(flushedRecords, summarySampleRate, false, oneFile, blockManager)
							}
							fmt.Println("Fingerprint uspesno dodat.")
						} else if input == "udaljenost" {
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
									if val, foundS := sstable.FindInSstables(keySH1, *blockManager); foundS {
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
									if val, foundS := sstable.FindInSstables(keySH1, *blockManager); foundS {
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
				} else if operacijaTypes == "bl" {
				bfLoop:
					for {
						fmt.Println("Unesite željenju operaciju: dodaj, izbrisi, postoji, novi, nazad")
						scanner.Scan()
						operacija := scanner.Text()

						if operacija == "dodaj" {
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
								record, found := memtable.GetRecord(kljucBF)
								if found {
									value := record.Value
									bf, err := Types.DeserializeBFWK(value)
									//fmt.Println("CMS.K =", cms.K, "CMS.M =", cms.M)
									//fmt.Println("Prvi red:", cms.Matrix[0])
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
										bf.BF.Add(noviElement)
										serialized, err := bf.SerializeBFWK()
										if err != nil {
											fmt.Println("Greška pri serializaciji: ", err)
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

								SSrecord, found := sstable.FindInSstables(kljucBF, *blockManager)
								if found {
									value := SSrecord.Value
									bf, err := Types.DeserializeBFWK(value)
									if err != nil {
										fmt.Println("Greška pri deserializaciji: ", err)
									} else {
										var newElement string
										for {
											fmt.Println("Unesite željeni element: ")
											scanner.Scan()
											newElement = scanner.Text()
											if newElement == "" {
												fmt.Println("Element ne može biti prazan string!")
												continue
											}
											break
										}
										bf.BF.Add(newElement)
										serialized, err := bf.SerializeBFWK()
										if err != nil {
											fmt.Println("Greška pri serializaciji: ", err)
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
								fmt.Println("Nema takvog kljuca")
							}

						} else if operacija == "novi" {
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
								if _, found := sstable.FindInSstables(keyBF, *blockManager); found {
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
									sstable.WriteSstable(flushedRecords, summarySampleRate, false, oneFile, blockManager)
								}
								fmt.Println("Novi bf uspešno napravljen i stavljen u memtable")
								continue bfLoop
							}

						} else if operacija == "izbrisi" {
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
									if _, found := sstable.FindInSstables(keyBF, *blockManager); !found {
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
									sstable.WriteSstable(flushedRecords, summarySampleRate, false, oneFile, blockManager)
								}
								fmt.Println(("BF uspesno obrisan."))
								continue bfLoop
							}

						} else if operacija == "postoji" {
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
								} else if SSrecord, found := sstable.FindInSstables(keyBF, *blockManager); found {
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
								scanner.Scan()
								element := scanner.Text()
								if element == "" {
									fmt.Println("Element ne moze biti prazan!")
									continue
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
