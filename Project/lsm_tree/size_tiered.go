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

type LSMTree struct {
	Levels           [][]*sstable.Sstable // za svaki nivo lista SSTabela (pojednostavljeno, može i sa mapom, ali ovde je lista)
	MaxLevels        int
	FilesPerLevel    int  // size-tiered parametar
	TieredCompaction bool // "size-tiered" ili "leveled"
}

func NewLSMTree(maxLevels int, filesPerLevel int, compactionType bool) *LSMTree {
	return &LSMTree{
		Levels:           make([][]*sstable.Sstable, maxLevels),
		MaxLevels:        maxLevels,
		FilesPerLevel:    filesPerLevel,
		TieredCompaction: compactionType, // ili "leveled", zavisno od konfiguracije
	}
}

func (lsm *LSMTree) AddSstable(level int, summarySampleRate int, oneFile bool, blockManager *BlockManager.BlockManager, compression bool) {
	maxLevels := lsm.MaxLevels

	for currLevel := level; currLevel < maxLevels; currLevel++ {
		// Napravi putanju do foldera za trenutni nivo
		levelDir := fmt.Sprintf("data/sstable/L%d", currLevel)

		// Pročitaj šta ima u tom folderu
		entries, err := os.ReadDir(levelDir)
		if err != nil {
			// Ako ne postoji folder, ništa za kompakciju
			fmt.Printf("Nema foldera za nivo %d: %v\n", currLevel, err)
			break
		}

		// Prebroj sve podfoldere koji počinju sa "sstable-"
		sstableCount := 0
		for _, entry := range entries {
			if entry.IsDir() && strings.HasPrefix(entry.Name(), "sstable-") {
				sstableCount++
			}
		}
		fmt.Printf("Nivo %d ima %d SSTabela\n", currLevel, sstableCount)

		// Ako broj SSTabla dostiže granicu, radi kompakciju i idi na sledeći nivo
		if sstableCount >= lsm.FilesPerLevel {
			// Ovde pozivaš funkciju za kompakciju na tom nivou
			fmt.Printf("Radi kompakciju na nivou %d\n", currLevel)
			lsm.CompactLevel(currLevel, summarySampleRate, oneFile, blockManager, compression)
			// posle kompakcije, nastavi petlju na sledeći nivo
		} else {
			// Ako nema dovoljno za kompakciju, izlazimo iz petlje
			break
		}
	}
}

// Pretpostavimo: lsm.Levels[level] je lista SSTabela sa LEVELA na kojem radis kompakciju
func (lsm *LSMTree) CompactLevel(level int, sampleRate int, oneFile bool, blockManager *BlockManager.BlockManager, compression bool) {
	fmt.Printf("Kompaktujem nivo %d\n", level)

	levelDir := fmt.Sprintf("data/sstable/L%d", level)
	entries, err := os.ReadDir(levelDir)
	if err != nil {
		fmt.Printf("Greška: %v\n", err)
		return
	}

	var allRecords [][]*Memtable.MemtableRecord
	var sstableFolders []string     // ovde ćemo folder putanje, tipa "data/sstable/L0/sstable-0001"
	var sstableNamesForTOC []string // ovde imena za master TOC, tipa "L0/sstable-0001"

	for _, entry := range entries {
		if entry.IsDir() && strings.HasPrefix(entry.Name(), "sstable-") {
			tocPath := fmt.Sprintf("%s/%s/%s-TOC.txt", levelDir, entry.Name(), entry.Name())
			sst := &sstable.Sstable{Filename: tocPath}
			records, err := sstable.ReadAllRecords(sst, blockManager)
			if err != nil {
				fmt.Printf("Greska ucitavanja %s: %v\n", tocPath, err)
				continue
			}
			allRecords = append(allRecords, records)
			sstableFolders = append(sstableFolders, fmt.Sprintf("%s/%s", levelDir, entry.Name()))
			sstableNamesForTOC = append(sstableNamesForTOC, fmt.Sprintf("L%d/%s", level, entry.Name()))
		}
	}

	if len(allRecords) == 0 {
		fmt.Println("Nema sstabela za kompakciju na ovom nivou.")
		return
	}

	// Prvo obriši stare SSTable foldere
	for _, folder := range sstableFolders {
		err := os.RemoveAll(folder)
		if err != nil {
			fmt.Printf("Greska pri brisanju foldera %s: %v\n", folder, err)
		} else {
			fmt.Printf("Obrisan folder %s\n", folder)
		}
	}

	// Sada ih ukloni iz master TOC-a
	if err := sstable.RemoveSstablesFromMasterTOC(sstableNamesForTOC); err != nil {
		fmt.Printf("Greska pri brisanju iz master TOC-a: %v\n", err)
	}

	// Merge-uj sve podatke
	mergedRecords := sstable.CompactSstables(allRecords)

	sort.Slice(mergedRecords, func(i, j int) bool {
		return mergedRecords[i].Key < mergedRecords[j].Key
	})

	newSst := sstable.WriteSstable(mergedRecords, sampleRate, true, oneFile, blockManager, compression, level+1) //dodaj u sstable level
	lsm.Levels[level+1] = append(lsm.Levels[level+1], newSst)

	// Novi SSTable će sam sebe dodati u master TOC (u WriteSstable funkciji)
}
