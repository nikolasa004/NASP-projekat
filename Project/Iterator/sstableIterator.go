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

	globalCompressionDictionary *sstable.Dictionary
	hasCompression              bool
	isPrefixScan                bool
	prefix                      string
	isRangeScan                 bool
	lowerBound                  string
	upperBound                  string
	pagesMap                    map[int]int
}

func NewSstableIterator(tocPath string, bm *BlockManager.BlockManager, prefix string, lower string, upper string, startingIndex int, hasCompression bool, compressionDictionary *sstable.Dictionary) *SstableIterator {
	disallowedPrefixes := []string{"bf_", "cms_", "__rateLimit__", "hll_", "sh_"}
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
	} else if lower != "" {
		_, indexStart = summary.FindRange(lower, upper)
	} else {
		indexStart = 0 // default
	}

	if isSingleFile {
		keyOffsetPairs = sstable.ReadIndexFromFile(bm, indexPath, indexStart, meta.IndexLength)
	} else {
		keyOffsetPairs = sstable.ReadIndexFromFile(bm, indexPath, indexStart, uint64(indexLength))
	}

	filtered := make([]sstable.KeyOffsetPair, 0)
	for _, pair := range keyOffsetPairs {
		var realKey string
		if hasCompression {
			realKey = compressionDictionary.FindOriginalKey(pair.Key)
		} else {
			realKey = pair.Key
		}

		skip := false
		for _, prefix := range disallowedPrefixes {
			if strings.HasPrefix(realKey, prefix) {
				skip = true
				break
			}
		}
		if skip {
			continue // preskoci kljuceve vezane za types
		}

		if prefix != "" && strings.HasPrefix(realKey, prefix) {
			filtered = append(filtered, pair)
		} else if lower != "" && upper != "" && realKey >= lower && realKey <= upper {
			filtered = append(filtered, pair)
		}
	}

	if startingIndex < 0 || startingIndex >= len(filtered) {
		startingIndex = 0 // resetuj na početak ako je indeks van granica
	}

	// Kreiranje mape stranica
	pages := make(map[int]int)
	pages[0] = startingIndex // početak prve stranice

	if isSingleFile {
		return &SstableIterator{
			blockManager:                bm,
			filename:                    dataPath,
			keyOffsetPairs:              filtered,
			currentIndex:                startingIndex,
			isScanned:                   false,
			dataOffset:                  meta.DataOffset,
			dataLength:                  meta.DataLength,
			globalCompressionDictionary: compressionDictionary,
			hasCompression:              hasCompression,
			isPrefixScan:                prefix != "",
			prefix:                      prefix,
			isRangeScan:                 lower != "",
			lowerBound:                  lower,
			upperBound:                  upper,
			pagesMap:                    pages,
		}
	} else {
		return &SstableIterator{
			blockManager:                bm,
			filename:                    dataPath,
			keyOffsetPairs:              filtered,
			currentIndex:                startingIndex,
			isScanned:                   false,
			dataOffset:                  0,
			dataLength:                  uint64(dataLength),
			globalCompressionDictionary: compressionDictionary,
			hasCompression:              hasCompression,
			isPrefixScan:                prefix != "",
			prefix:                      prefix,
			isRangeScan:                 lower != "",
			lowerBound:                  lower,
			upperBound:                  upper,
			pagesMap:                    pages,
		}
	}
}

func GenerateIteratorsPrefixSstable(bm *BlockManager.BlockManager, prefix string, startingIndex int, hasCompression bool, compressionDictionary *sstable.Dictionary) []InterfaceIterator {
	var iterators []InterfaceIterator

	levels, _ := sstable.ReadMasterTOCIterators()
	for level, sstables := range levels {
		for i := len(sstables) - 1; i >= 0; i-- {
			toc := filepath.Join("data", "sstable", level, sstables[i], fmt.Sprintf("%s-TOC.txt", sstables[i]))
			iter := NewSstableIterator(toc, bm, prefix, "", "", startingIndex, hasCompression, compressionDictionary)
			print(sstables[i], " - ", startingIndex)
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

func GenerateIteratorsRangeSstable(bm *BlockManager.BlockManager, lower, upper string, startingIndex int, hasCompression bool, compressionDictionary *sstable.Dictionary) []InterfaceIterator {
	var iterators []InterfaceIterator

	levels, _ := sstable.ReadMasterTOCIterators()
	for level, sstables := range levels {
		for i := len(sstables) - 1; i >= 0; i-- {
			toc := filepath.Join("data", "sstable", level, sstables[i], fmt.Sprintf("%s-TOC.txt", sstables[i]))
			iter := NewSstableIterator(toc, bm, "", lower, upper, startingIndex, hasCompression, compressionDictionary)
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
		pair := it.keyOffsetPairs[it.currentIndex]

		record, found, _, _ := sstable.FindDataRecordBM(
			pair.Key,
			pair.Offset+it.dataOffset,
			it.filename,
			*it.blockManager,
			it.dataOffset+it.dataLength,
		)

		if found {
			originalKey := record.Key
			if it.hasCompression {
				originalKey = it.globalCompressionDictionary.FindOriginalKey(record.Key)
			}

			if it.isPrefixScan && !strings.HasPrefix(originalKey, it.prefix) {
				it.Next()
				continue
			}
			if it.isRangeScan && (originalKey < it.lowerBound || originalKey > it.upperBound) {
				it.Next()
				continue
			}
			return NewScanRecord(originalKey, record.Value, record.Tombstone)
		}
		it.Next()
	}
	return nil
}

func (it *SstableIterator) ChangePage(targetPage int) {
	index, exists := it.pagesMap[targetPage]
	if exists {
		it.currentIndex = index
		it.isScanned = false
	}
}

func (it *SstableIterator) FinishPage() {
	pageNumber := len(it.pagesMap)
	it.pagesMap[pageNumber] = it.currentIndex
}

func (s *SstableIterator) AlreadyRead(targetPage int) bool {
	_, exists := s.pagesMap[targetPage]
	return exists
}
