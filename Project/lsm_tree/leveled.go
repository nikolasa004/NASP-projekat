package lsmtree

import (
	"fmt"
	"os"
	"project/BlockManager"
	"strings"
)

func (lsm *LSMTree) AddSstableLeveled(level int, summarySampleRate int, oneFile bool, blockManager *BlockManager.BlockManager, compression bool) {
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
		fmt.Printf("Nivo %d ima %d SSTabela (granica za ovaj nivo: %d)\n", currLevel, sstableCount, maxFiles)

		// Ako broj SSTabela dostiže granicu, radi kompakciju i idi na sledeći nivo
		if sstableCount >= maxFiles {
			fmt.Printf("Radi kompakciju na nivou %d\n", currLevel)
			lsm.CompactLevel(currLevel, summarySampleRate, oneFile, blockManager, compression)
			// Granica za sledeći nivo je puta 10
			maxFiles *= 10
		} else {
			// Ako nema dovoljno za kompakciju, izlazimo iz petlje
			break
		}
	}
}
