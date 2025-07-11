package Iterator

import (
	"fmt"
	"os"
	"path/filepath"
	"project/BlockManager"
	"project/sstable"
	"strings"
)

type SstableIterator struct {
	blockManager   *BlockManager.BlockManager
	filename       string
	keyOffsetPairs []sstable.KeyOffsetPair
	currentIndex   int
	isScanned      bool
	dataOffset     uint64
	dataLength     uint64

	isPrefixScan bool
	prefix       string
	isRangeScan  bool
	lowerBound   string
	upperBound   string
}

func NewSstableIterator(tocPath string, bm *BlockManager.BlockManager, prefix string, lower string, upper string) *SstableIterator {
	toc, err := sstable.ReadTOCFile(tocPath)
	if err != nil {
		fmt.Println("Greška pri čitanju TOC fajla:", err)
		return nil
	}
	if len(toc.Files) == 0 {
		fmt.Println("TOC fajl je prazan:", tocPath)
		return nil
	}

	// Očisti fajlove da ne sadrže višak
	for i, file := range toc.Files {
		cleaned := strings.TrimSpace(file)
		cleaned = strings.Trim(cleaned, "[]\r\n\t ")
		toc.Files[i] = cleaned
	}

	var dataPath, indexPath, summaryPath string
	var summaryLength, indexLength, dataLength int64
	isSingleFile := len(toc.Files) == 1

	// Single file režim
	if isSingleFile {
		dataPath = toc.Files[0]
		indexPath = toc.Files[0]
		summaryPath = toc.Files[0]
	} else if len(toc.Files) >= 3 {
		dataPath = toc.Files[0]
		indexPath = toc.Files[1]
		summaryPath = toc.Files[2]

		infoSummary, _ := os.Stat(summaryPath)
		summaryLength = infoSummary.Size()
		infoIndex, _ := os.Stat(indexPath)
		indexLength = infoIndex.Size()
		infoData, _ := os.Stat(dataPath)
		dataLength = infoData.Size()
	} else {
		fmt.Println("Nevalidan TOC format:", toc.Files)
		return nil
	}

	meta, err := sstable.ReadMetadata(dataPath)
	if err != nil {
		fmt.Println("Greška pri čitanju metapodataka:", err)
		return nil
	}

	summary := sstable.Summary{}
	if isSingleFile {
		summary = *sstable.ReadSummaryFromFile(bm, summaryPath, meta.SummaryOffset, meta.SummaryLength)
	} else {
		summary = *sstable.ReadSummaryFromFile(bm, summaryPath, 0, uint64(summaryLength))
	}

	var indexStart uint64
	var keyOffsetPairs []sstable.KeyOffsetPair
	if prefix != "" {
		_, indexStart = summary.FindPrefix(prefix)
		fmt.Println("Index start za prefix:", prefix, "->", indexStart)
	} else if lower != "" {
		_, indexStart = summary.FindRange(lower, upper)
		fmt.Println("Index start za lower bound:", lower, "->", indexStart)
	} else {
		indexStart = 0 // default
	}

	fmt.Println("Index start:", indexStart, indexLength)
	if isSingleFile {
		keyOffsetPairs = sstable.ReadIndexFromFile(bm, indexPath, indexStart, meta.IndexLength)
	} else {
		keyOffsetPairs = sstable.ReadIndexFromFile(bm, indexPath, indexStart, uint64(indexLength))
	}
	fmt.Println(meta.IndexLength)

	filtered := make([]sstable.KeyOffsetPair, 0)
	for _, pair := range keyOffsetPairs {
		if prefix != "" && strings.HasPrefix(pair.Key, prefix) {
			fmt.Println("ima prefix:", pair.Key, "->", prefix)
			filtered = append(filtered, pair)
		} else if lower != "" && upper != "" && pair.Key >= lower && pair.Key <= upper {
			filtered = append(filtered, pair)
		}
	}

	if isSingleFile {
		return &SstableIterator{
			blockManager:   bm,
			filename:       dataPath,
			keyOffsetPairs: filtered,
			currentIndex:   0,
			isScanned:      false,
			dataOffset:     meta.DataOffset,
			dataLength:     meta.DataLength,
			isPrefixScan:   prefix != "",
			prefix:         prefix,
			isRangeScan:    lower != "",
			lowerBound:     lower,
			upperBound:     upper,
		}
	} else {
		return &SstableIterator{
			blockManager:   bm,
			filename:       dataPath,
			keyOffsetPairs: filtered,
			currentIndex:   0,
			isScanned:      false,
			dataOffset:     0,
			dataLength:     uint64(dataLength),
			isPrefixScan:   prefix != "",
			prefix:         prefix,
			isRangeScan:    lower != "",
			lowerBound:     lower,
			upperBound:     upper,
		}
	}
}

func GenerateIteratorsPrefixSstable(bm *BlockManager.BlockManager, prefix string) []InterfaceIterator {
	var iterators []InterfaceIterator

	levels, _ := sstable.ReadMasterTOCIterators()
	for level, sstables := range levels {
		for i := len(sstables) - 1; i >= 0; i-- {
			toc := filepath.Join("data", "sstable", level, sstables[i], fmt.Sprintf("%s-TOC.txt", sstables[i]))
			iter := NewSstableIterator(toc, bm, prefix, "", "")
			if iter == nil {
				fmt.Println("Preskačem nevalidan SSTable:", toc)
				continue
			}
			if len(iter.keyOffsetPairs) > 0 {
				iterators = append(iterators, iter)
			}
		}
	}
	return iterators
}

func GenerateIteratorsRangeSstable(bm *BlockManager.BlockManager, lower, upper string) []InterfaceIterator {
	var iterators []InterfaceIterator

	levels, _ := sstable.ReadMasterTOCIterators()
	for level, sstables := range levels {
		for i := len(sstables) - 1; i >= 0; i-- {
			toc := filepath.Join("data", "sstable", level, sstables[i], fmt.Sprintf("%s-TOC.txt", sstables[i]))
			iter := NewSstableIterator(toc, bm, "", lower, upper)
			if iter == nil {
				fmt.Println("Preskačem nevalidan SSTable:", toc)
				continue
			}
			if len(iter.keyOffsetPairs) > 0 {
				iterators = append(iterators, iter)
			}
		}
	}
	return iterators
}

func (it *SstableIterator) Next() {
	it.currentIndex++
	if it.currentIndex >= len(it.keyOffsetPairs) {
		it.isScanned = true
	}
}

func (it *SstableIterator) GetValue() *ScanRecord {
	for !it.isScanned && it.currentIndex < len(it.keyOffsetPairs) {
		fmt.Println("Proveravam indeks:", it.currentIndex, "od", it.keyOffsetPairs)
		pair := it.keyOffsetPairs[it.currentIndex]
		record, found, _, _ := sstable.FindDataRecordBM(
			pair.Key,
			pair.Offset+it.dataOffset,
			it.filename,
			*it.blockManager,
			it.dataOffset+it.dataLength,
		)
		if found {
			if it.isPrefixScan && !strings.HasPrefix(record.Key, it.prefix) {
				it.Next()
				continue
			}
			if it.isRangeScan && (record.Key < it.lowerBound || record.Key > it.upperBound) {
				it.Next()
				continue
			}
			return NewScanRecord(record.Key, record.Value, record.Tombstone)
		}
		// Ako nije pronađeno (npr. tombstone/greška) — preskoči
		it.Next()
	}
	return nil
}

func (it *SstableIterator) ChangePage(targetPage int) {

}

func (it *SstableIterator) FinishPage() {

}
