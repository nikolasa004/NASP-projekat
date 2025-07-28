package Scanners

import (
	"fmt"
	"project/BlockManager"
	"project/Iterator"
	"project/Memtable"
	"project/sstable"
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
) []string {
	var results []string
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

					fmt.Printf("Stranica %d:\n", currentPage+1)
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
						fmt.Println("Nema zapisa na ovoj stranici.")
						if firstIteratiron {
							fmt.Println("Nema podataka!")
							return nil
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

					fmt.Printf("Stranica %d:\n", currentPage+1)
					for _, rec := range pageOfIterators {
						results = append(results, string(rec.Value))
					}
				}
			}
		}
		// Upit za sledecu komandu

		break mainLoop
	}

	return results
}
