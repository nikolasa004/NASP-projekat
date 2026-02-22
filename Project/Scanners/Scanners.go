package Scanners

import (
	"bufio"
	"fmt"
	"os"
	"project/BlockManager"
	"project/Iterator"
	"project/Memtable"
	"project/sstable"
	"strconv"
	"strings"
)

func Scan(
	prefix string,
	lowerBound string,
	upperBound string,
	memtable *Memtable.Memtable,
	blockManager *BlockManager.BlockManager,
	hasCompression bool,
	compressionDict *sstable.Dictionary,
	pageSize int,
	cacheSize int,
) {
	var memIts []Iterator.InterfaceIterator
	var ssIts []Iterator.InterfaceIterator

	if prefix == "" {
		memIts, _ = Iterator.GenerateIteratorsRange(memtable, lowerBound, upperBound)
		ssIts = Iterator.GenerateIteratorsRangeSstable(blockManager, lowerBound, upperBound, 0, hasCompression, compressionDict)
	} else {
		memIts, _ = Iterator.GenerateIteratorsPrefix(memtable, prefix)
		ssIts = Iterator.GenerateIteratorsPrefixSstable(blockManager, prefix, 0, hasCompression, compressionDict)
	}

	allIts := append(memIts, ssIts...)

	currentPage := 0
	prevPage := 0
	scanner := bufio.NewScanner(os.Stdin)

	cache := NewLRUCache(cacheSize)
	skipToInput := false
	firstIteratiron := true
mainLoop:
	for {
		var pageOfIterators []Iterator.ScanRecord //ovde se cuvaju strane, puni se iz iteratora
		{
			if !skipToInput {

				//provera kesa, ako ima u kesu, ispise i preskoci sve do novog upisa
				cachedPage, found := cache.Get(currentPage)
				if found {

					fmt.Printf("Page %d:\n", currentPage+1)
					for _, rec := range cachedPage {
						fmt.Printf("Key: %s, Value: %s\n", rec.Key, rec.Value)
					}
				} else {

					//menja stanje svih iteratora na odabranu stranu
					for _, it := range allIts {
						if changer, ok := it.(interface{ ChangePage(int) }); ok {
							changer.ChangePage(currentPage)
						}
					}

					// uzimamo prvi iterator iz svakog iteratora
					heads := make([]*Iterator.ScanRecord, len(allIts))
					for i, it := range allIts {
						heads[i] = it.GetValue()
					}

					count := 0

					for count < pageSize {
						minKey := ""
						minKeyIndices := []int{}
						allNil := true

						// Pronadji sve iteratore koji imaju trenutno najmanji kljuc
						for i, rec := range heads {
							if rec == nil {
								continue
							}
							allNil = false
							if len(minKeyIndices) == 0 || rec.Key < minKey {
								minKey = rec.Key
								minKeyIndices = []int{i}
							} else if rec.Key == minKey {
								minKeyIndices = append(minKeyIndices, i)
							}
						}

						if allNil {
							break // svi iteratori su iscrpljeni
						}

						// Upis zapisa sa najmanjim kljucem
						rep := heads[minKeyIndices[0]]
						if !rep.Tombstone {
							pageOfIterators = append(pageOfIterators, *rep)
							count++
						}

						// Pomeri sve iteratore koji su imali taj minKey
						for _, idx := range minKeyIndices {
							allIts[idx].Next()
							heads[idx] = allIts[idx].GetValue()
						}
					}

					if len(pageOfIterators) == 0 {
						fmt.Println("No records on page", currentPage+1)
						if firstIteratiron {
							fmt.Println("No records found for the given scan criteria.")
							return
						}
						if currentPage > 0 {
							currentPage = prevPage // vrati se na prethodnu validnu ako nisi vec na prvoj
						}
						continue
					}
					firstIteratiron = false

					// Zapamti indekse za sledecu stranicu

					for _, it := range allIts {
						if f, ok := it.(interface{ FinishPage() }); ok {
							f.FinishPage()
						}
					}

					// Kesiranje
					cache.Put(currentPage, pageOfIterators)

					// Ispis

					fmt.Printf("Page %d:\n", currentPage+1)
					for _, rec := range pageOfIterators {
						fmt.Printf("Key: %s, Value: %s\n", rec.Key, rec.Value)
					}
				}

				prevPage = currentPage
			}
			skipToInput = false
		}
		// Upit za sledecu komandu
		fmt.Println("\nEnter 'n' for next page, 'p' for previous page, a page number to jump to that page, or 'q' to quit:")
		if !scanner.Scan() {
			break
		}
		input := strings.TrimSpace(scanner.Text())

		switch input {
		case "q":
			break mainLoop
		case "n":
			currentPage++
		case "p":
			if currentPage == 0 {
				fmt.Println("Already at the first page.")
				continue
			}
			currentPage--
		default:
			pageNum, err := strconv.Atoi(input)
			if err != nil || pageNum <= 0 {
				fmt.Println("Invalid input. Please enter a valid page number.")
				continue
			}
			desiredPage := pageNum - 1
			if desiredPage < 0 {
				fmt.Println("Page number must be positive.")
				continue
			}

			cachedPage, found := cache.Get(desiredPage)
			if found {
				fmt.Printf("Page %d:\n", desiredPage+1)
				for _, rec := range cachedPage {
					fmt.Printf("Key: %s, Value: %s\n", rec.Key, rec.Value)
				}

				prevPage = currentPage
				currentPage = desiredPage
				skipToInput = true
				continue mainLoop
			}

			//ako je stranica manja od trenutne, vec je ucitana i mozemo odmah da odradimo change page u main loop
			if desiredPage < currentPage {
				prevPage = currentPage
				currentPage = desiredPage
				continue mainLoop
			}

			//pravimo stranice od trenutne do trazene
			for page := currentPage + 1; page <= desiredPage; page++ {
				for _, it := range allIts {
					if changer, ok := it.(interface{ ChangePage(int) }); ok {
						changer.ChangePage(page)
					}
				}
				heads := make([]*Iterator.ScanRecord, len(allIts))
				for i, it := range allIts {
					heads[i] = it.GetValue()
				}

				count := 0
				merged := make([]Iterator.ScanRecord, 0, pageSize)

				for count < pageSize {
					minKey := ""
					minKeyIndices := []int{}
					allNil := true

					for i, rec := range heads {
						if rec == nil {
							continue
						}
						allNil = false
						if len(minKeyIndices) == 0 || rec.Key < minKey {
							minKey = rec.Key
							minKeyIndices = []int{i}
						} else if rec.Key == minKey {
							minKeyIndices = append(minKeyIndices, i)
						}
					}

					if allNil {
						break
					}

					rep := heads[minKeyIndices[0]]
					if !rep.Tombstone {
						merged = append(merged, *rep)
						count++
					}

					for _, idx := range minKeyIndices {
						allIts[idx].Next()
						heads[idx] = allIts[idx].GetValue()
					}
				}

				for _, it := range allIts {
					if f, ok := it.(interface{ FinishPage() }); ok {
						f.FinishPage()
					}
				}

				// Ako se pojavi prazna stranica ispisi
				if len(merged) == 0 {
					fmt.Println("No records on page", desiredPage+1)
					continue mainLoop
				}
			}
			prevPage = currentPage
			currentPage = desiredPage
		}
	}
}
