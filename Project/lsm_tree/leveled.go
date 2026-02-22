package lsmtree

import (
	"fmt"
	"os"
	"project/BlockManager"
	"project/Memtable"
	"project/sstable"
	"sort"
	"strings"
)

func (lsm *LSMTree) AddSstableLeveled(level int, summarySampleRate int, oneFile bool, blockManager *BlockManager.BlockManager, compression bool, dictionary *sstable.Dictionary, multiplyer int) {
	maxLevels := lsm.MaxLevels

	// Početna granica (koliko fajlova na nivou 0 izaziva kompakciju)
	maxFiles := lsm.FilesPerLevel

	for currLevel := level; currLevel < maxLevels; currLevel++ {
		// Napravi putanju do foldera za trenutni nivo
		levelDir := fmt.Sprintf("data/sstable/L%d", currLevel)

		// Pročitaj šta ima u tom folderu
		entries, err := os.ReadDir(levelDir)
		if err != nil {
			// Ako ne postoji folder, ništa za kompakciju
			fmt.Printf("There is no directory for level %d: %v\n", currLevel, err)
			break
		}

		// Prebroj sve podfoldere koji počinju sa "sstable-"
		sstableCount := 0
		for _, entry := range entries {
			if entry.IsDir() && strings.HasPrefix(entry.Name(), "sstable-") {
				sstableCount++
			}
		}
		fmt.Printf("Level %d has %d SSTables (edge for this level: %d)\n", currLevel, sstableCount, maxFiles)

		// Ako broj SSTabela dostiže granicu, radi kompakciju i idi na sledeći nivo
		if sstableCount >= maxFiles {
			fmt.Printf("Compacting level %d\n", currLevel)
			lsm.CompactLevelLeveled(currLevel, summarySampleRate, oneFile, blockManager, compression, dictionary)
			// Granica za sledeći nivo je puta 10
			maxFiles *= multiplyer
		} else {
			// Ako nema dovoljno za kompakciju, izlazimo iz petlje
			break
		}
	}
}

func (lsm *LSMTree) CompactLevelLeveled(
	level int,
	sampleRate int,
	oneFile bool,
	blockManager *BlockManager.BlockManager,
	compression bool,
	dictionary *sstable.Dictionary,
) {
	fmt.Printf("Compacting level %d\n", level)

	levelDir := fmt.Sprintf("data/sstable/L%d", level)
	entries, err := os.ReadDir(levelDir)
	if err != nil {
		fmt.Printf("[Error] Reading level directory: %v\n", err)
		return
	}

	// 1. Nađi najstariju SSTabelu (prva po imenu)
	var oldestSstable string
	for _, entry := range entries {
		if entry.IsDir() && strings.HasPrefix(entry.Name(), "sstable-") {
			oldestSstable = entry.Name()
			break
		}
	}
	if oldestSstable == "" {
		fmt.Println("[Error] No SSTables to compact on level.")
		return
	}
	fmt.Printf("Compacting the oldest SSTable: %s\n", oldestSstable)

	// 2. Proveri da li je kompresovana
	tocPath := fmt.Sprintf("%s/%s/%s-TOC.txt", levelDir, oldestSstable, oldestSstable)
	isCompressedOldest := false
	tocFile, err := os.Open(tocPath)
	if err == nil {
		var firstLine string
		fmt.Fscanf(tocFile, "%s\n", &firstLine)
		if firstLine == "1" {
			isCompressedOldest = true
		}
		tocFile.Close()
	}

	// 3. Učitaj min i max ključ
	sst := &sstable.Sstable{Filename: tocPath}
	minKey, maxKey, err := sstable.GetMinMaxKey(sst, blockManager, compression, dictionary)
	if err != nil {
		fmt.Printf("[Error] Cannot read min/max key: %v\n", err)
		return
	}
	fmt.Printf("Min key: %s, Max key: %s\n", minKey, maxKey)

	// 4. Učitaj sve zapise iz najstarije SSTabele
	recordsOldest, err := sstable.ReadAllRecords(sst, blockManager)
	if err != nil {
		fmt.Printf("[Error] Reading records from %s: %v\n", tocPath, err)
		return
	}
	// --- Dekomprimuj ključeve ako je potrebno ---
	if isCompressedOldest && dictionary != nil {
		for _, rec := range recordsOldest {
			rec.Key = dictionary.FindOriginalKey(rec.Key)
		}
	}

	// 5. Na sledećem nivou nađi sve overlapp SSTabele
	nextLevelDir := fmt.Sprintf("data/sstable/L%d", level+1)
	entriesNext, err := os.ReadDir(nextLevelDir)
	var overlappingFolders []string
	var allRecords [][]*Memtable.MemtableRecord

	// Dodaj najstariju
	allRecords = append(allRecords, recordsOldest)
	if err == nil {
		for _, entry := range entriesNext {
			if entry.IsDir() && strings.HasPrefix(entry.Name(), "sstable-") {
				tocNext := fmt.Sprintf("%s/%s/%s-TOC.txt", nextLevelDir, entry.Name(), entry.Name())
				//fmt.Printf("Proveravam SSTabelu: %s\n", tocNext)
				// --- Proveri da li je kompresovana ---
				isCompressedNext := false
				tocFile, err := os.Open(tocNext)
				if err == nil {
					var firstLine string
					fmt.Fscanf(tocFile, "%s\n", &firstLine)
					if firstLine == "1" {
						isCompressedNext = true
					}
					tocFile.Close()
				}
				sstNext := &sstable.Sstable{Filename: tocNext}
				minNext, maxNext, err := sstable.GetMinMaxKey(sstNext, blockManager, isCompressedNext, dictionary)
				if err == nil && (minNext <= maxKey && maxNext >= minKey) {
					records, err := sstable.ReadAllRecords(sstNext, blockManager)
					if err == nil {
						// --- Dekomprimuj ključeve ako treba ---
						if isCompressedNext && dictionary != nil {
							for _, rec := range records {
								rec.Key = dictionary.FindOriginalKey(rec.Key)
							}
						}
						allRecords = append(allRecords, records)
						overlappingFolders = append(overlappingFolders, fmt.Sprintf("%s/%s", nextLevelDir, entry.Name()))
					}
				}
			}
		}
	}
	fmt.Printf("Found %d overlapping tables on level %d: %v\n", len(overlappingFolders), level+1, overlappingFolders)

	// 9. Obriši stare foldere
	os.RemoveAll(fmt.Sprintf("%s/%s", levelDir, oldestSstable))
	for _, folder := range overlappingFolders {
		os.RemoveAll(folder)
	}

	levels := strings.Split(levelDir, "/") // uzmi samo deo posle "data/sstable/"
	sstableToDelete := fmt.Sprintf("%s/%s", levels[2], oldestSstable)
	// isto za najstariju
	if err := sstable.RemoveSstablesFromMasterTOC([]string{sstableToDelete}); err != nil {
		fmt.Printf("[Error] Error removing oldest SSTable from master TOC: %v\n", err)
	} else {
		//fmt.Printf("Uspesno uklonjena najstarija SSTabela iz master TOC-a: %s\n", fmt.Sprintf("%s/%s", levelDir, oldestSstable))
	}
	fmt.Printf("Removed level: %s\n", levelDir)
	fmt.Printf("Removed sstable: %s\n", oldestSstable)

	for i, path := range overlappingFolders {
		overlappingFolders[i] = sstable.ExtractMasterTOCPath(path)
	}

	//sada ih ukloni iz master TOC-a
	if err := sstable.RemoveSstablesFromMasterTOC(overlappingFolders); err != nil {
		fmt.Printf("[Error] Error removing overlapping SSTables from master TOC: %v\n", err)
	}

	// 6. Merge i sort
	mergedRecords := sstable.CompactSstables(allRecords)
	sort.Slice(mergedRecords, func(i, j int) bool {
		return mergedRecords[i].Key < mergedRecords[j].Key
	})

	// 7. Kompresuj ključeve ako treba
	if compression && dictionary != nil {
		for _, rec := range mergedRecords {
			rec.Key = dictionary.Put(rec.Key)
		}
	}

	// 8. Upisi novu SSTabelu na sledeći nivo
	newSst := sstable.WriteSstable(mergedRecords, sampleRate, true, oneFile, blockManager, compression, level+1)
	lsm.Levels[level+1] = append(lsm.Levels[level+1], newSst)

}
