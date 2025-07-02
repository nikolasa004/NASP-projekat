package sstable

import (
	"encoding/binary"
	"fmt"
	"log"
	"os"
	"project/BlockManager"
)

type Summary struct {
	Keys         []string
	IndexOffsets []uint64
	Filename     string
	Min          string
	Max          string
	Type         uint8
}

func NewSummaryFromIndex(index *Index, filename string, sampleRate int) *Summary {
	summary := &Summary{
		Filename: filename,
		Min:      index.Entries[0].Key,
		Max:      index.Entries[len(index.Entries)-1].Key,
	}
	for i, entry := range index.Entries {
		if i == 0 || i%sampleRate == 0 {
			summary.Add(entry.Key, uint64(i*16))
		}
	}
	return summary
}

func NewSummary(keys []string, offsets []uint64, filename string, sampleRate int) *Summary {
	var maxi string
	if len(keys) == 1 {
		maxi = `~`
	} else {
		maxi = keys[len(keys)-1]
	}
	summary := &Summary{
		Filename: filename,
		Min:      keys[0],
		Max:      maxi,
	}
	for i := range keys {
		if i%sampleRate == 0 {
			summary.Add(keys[i], offsets[i])
		}
	}

	return summary
}

func CreateFragmentedSummaryRecord(key string, offset uint64, fragment int, isHeader bool) []byte {
	bytesRecord := []byte{}

	keyBytes := []byte(key)
	keySize := uint64(len(keyBytes))

	// 1. Key size (8B)
	keySizeBytes := make([]byte, 8)
	binary.LittleEndian.PutUint64(keySizeBytes, keySize)
	bytesRecord = append(bytesRecord, keySizeBytes...)

	// 2. Fragment type (1B)
	typeBytes := []byte{byte(fragment)}
	bytesRecord = append(bytesRecord, typeBytes...)

	// 3. Offset (8B)
	if !isHeader {
		offsetBytes := make([]byte, 8)
		binary.LittleEndian.PutUint64(offsetBytes, offset)
		bytesRecord = append(bytesRecord, offsetBytes...)
	}
	// 4. Key (keySize B)
	bytesRecord = append(bytesRecord, keyBytes...)

	return bytesRecord
}

func CreateSummaryRecordFragments(key string, offset uint64, blockSize int, isHeader bool) [][]byte {
	binaryRecords := [][]byte{}

	keySize := uint64(len(key))

	var constMaxSize uint64

	if !isHeader {
		constMaxSize = uint64(blockSize) - 17 // 8 (keySize) + 1 (type) + 8 (offset)
	} else {
		constMaxSize = uint64(blockSize) - 9 // 8 (keySize) + 1 (type)
	}

	if keySize <= constMaxSize {
		binaryRecord := CreateFragmentedSummaryRecord(key, offset, 0, isHeader)
		binaryRecords = append(binaryRecords, binaryRecord)
	} else {
		fragmentedKeySize := constMaxSize
		numberOfFragments := 1

		for {
			var newKey string
			if keySize > fragmentedKeySize {
				newKey = key[:fragmentedKeySize]
			} else {
				newKey = key
			}

			binaryRecord := CreateFragmentedSummaryRecord(newKey, offset, numberOfFragments, isHeader)
			binaryRecords = append(binaryRecords, binaryRecord)

			if len(newKey) == len(key) {
				break
			}

			key = key[len(newKey):]
			keySize -= uint64(len(newKey))
			numberOfFragments++
		}
	}

	return binaryRecords
}

func CreateMinMaxHeader(min, max string) []byte {
	buf := []byte{}

	// Min length + Min
	minLen := uint64(len(min))
	minLenBytes := make([]byte, 8)
	binary.LittleEndian.PutUint64(minLenBytes, minLen)
	buf = append(buf, minLenBytes...)
	buf = append(buf, []byte(min)...)

	// Max length + Max
	maxLen := uint64(len(max))
	maxLenBytes := make([]byte, 8)
	binary.LittleEndian.PutUint64(maxLenBytes, maxLen)
	buf = append(buf, maxLenBytes...)
	buf = append(buf, []byte(max)...)

	return buf
}

func (s *Summary) Add(key string, offset uint64) {
	s.Keys = append(s.Keys, key)
	s.IndexOffsets = append(s.IndexOffsets, offset)
}

func (s *Summary) Find(key string) (bool, uint64) {
	// Provera da li je ključ u opsegu Summary-ja
	if key < s.Min || key > s.Max {
		return false, uint64(0)
	}

	// Pretražujemo ključeve u Summary-ju
	for i, currentKey := range s.Keys {
		if currentKey == key {
			return true, s.IndexOffsets[i]
		} else if currentKey > key {
			if i > 0 {
				return true, s.IndexOffsets[i-1]
			}

			return true, uint64(0)
		}
	}

	return true, s.IndexOffsets[len(s.IndexOffsets)-1]
}

func (s *Summary) Print() {
	fmt.Println("Summary:")
	fmt.Println("Min: ", s.Min)
	fmt.Println("Max: ", s.Max)
	for i := range s.Keys {
		fmt.Println("Key: ", s.Keys[i])

	}
}

func (s *Summary) Write(multipleFiles bool, offset uint64) uint64 {
	currentOffset := uint64(0)
	file, err := os.OpenFile(s.Filename, os.O_APPEND|os.O_WRONLY, 0644)
	if err != nil {
		log.Fatal(err)
	}
	defer file.Close()

	_, err = file.Seek(int64(offset), 0)
	if err != nil {
		log.Fatal(err)
	}

	minLen := uint64(len(s.Min))
	err = binary.Write(file, binary.LittleEndian, minLen)
	if err != nil {
		log.Fatal(err)
	}
	currentOffset += 8

	_, err = file.Write([]byte(s.Min))
	if err != nil {
		log.Fatal(err)
	}
	currentOffset += uint64(len(s.Min))

	maxLen := uint64(len(s.Max))
	err = binary.Write(file, binary.LittleEndian, maxLen)
	if err != nil {
		log.Fatal(err)
	}
	currentOffset += 8

	_, err = file.Write([]byte(s.Max))
	if err != nil {
		log.Fatal(err)
	}
	currentOffset += uint64(len(s.Max))

	for i, key := range s.Keys {
		keyLen := uint64(len(key))
		err = binary.Write(file, binary.LittleEndian, keyLen)
		if err != nil {
			log.Fatal(err)
		}
		currentOffset += 8

		_, err = file.Write([]byte(key))
		if err != nil {
			log.Fatal(err)
		}
		currentOffset += uint64(len(key))

		err = binary.Write(file, binary.LittleEndian, s.IndexOffsets[i])
		if err != nil {
			log.Fatal(err)
		}
		currentOffset += 8

	}
	return currentOffset
}

func (s *Summary) writeWithBM(multipleFiles bool, offset uint64, bm *BlockManager.BlockManager) uint64 {

	headerMinFragments := CreateSummaryRecordFragments(s.Min, 0, bm.GetBlockSize(), true)

	headerMaxFragments := CreateSummaryRecordFragments(s.Max, 0, bm.GetBlockSize(), true)

	keyFragmentsMap := make(map[string][][]byte)

	keyFragmentsMap["MIN"] = headerMinFragments
	keyFragmentsMap["MAX"] = headerMaxFragments

	for i := range s.Keys {
		fragments := CreateSummaryRecordFragments(s.Keys[i], s.IndexOffsets[i], bm.GetBlockSize(), false)
		keyFragmentsMap[s.Keys[i]] = fragments
	}

	_, _, fileLen := bm.PutSstableRawBlock(keyFragmentsMap, s.Filename, multipleFiles, offset, false)

	return fileLen
}

func ReadSummaryFromFile(bm *BlockManager.BlockManager, filename string, offset uint64, length uint64) *Summary {
	blockSize := bm.GetBlockSize()
	var blocks [][]byte

	numBlocks := int(length / uint64(blockSize))
	if length%uint64(blockSize) != 0 {
		numBlocks++
	}

	for i := 0; i < numBlocks; i++ {
		block := bm.GetWalBlock(filename, int(offset/uint64(blockSize))+i, blockSize, false)
		if block != nil {
			blocks = append(blocks, block)
		}
	}

	return ReadSummaryFromBlocks(blocks)
}

func ReadSummaryFromBlocks(blocks [][]byte) *Summary {
	if len(blocks) == 0 {
		return nil
	}

	summary := &Summary{}

	// --- 1. Pročitaj MIN ---
	minKey, blk, cur, err := readHeaderKey(blocks, 0, 0)
	if err != nil {
		fmt.Println("Error reading MIN:", err)
		return nil
	}
	summary.Min = minKey
	fmt.Println("MIN:", summary.Min)

	// --- 2. Pročitaj MAX ---
	maxKey, blk, cur, err := readHeaderKey(blocks, blk, cur)
	if err != nil {
		fmt.Println("Error reading MAX:", err)
		return nil
	}
	summary.Max = maxKey
	fmt.Println("MAX:", summary.Max)

	// --- 3. Čitaj key+offset parove ---
	var currentFragments []KeyOffsetPair
	var lastType uint8

	for blockIndex := blk; blockIndex < len(blocks); blockIndex++ {
		block := blocks[blockIndex]
		cursor := cur
		if blockIndex != blk {
			cursor = 0
		}

		for cursor+17 <= len(block) {
			keySize := binary.LittleEndian.Uint64(block[cursor : cursor+8])
			fragType := block[cursor+8]
			offset := binary.LittleEndian.Uint64(block[cursor+9 : cursor+17])

			if keySize == 0 || cursor+17+int(keySize) > len(block) {
				break
			}

			keyBytes := block[cursor+17 : cursor+17+int(keySize)]
			cursor += 17 + int(keySize)

			pair := KeyOffsetPair{
				Key:    string(keyBytes),
				Offset: offset,
				KeyLen: keySize,
				Type:   fragType,
			}

			if fragType == 0 {
				if len(currentFragments) > 0 {
					full := stitchFragments(currentFragments)
					summary.Add(full.Key, full.Offset)
					currentFragments = nil
				}
				summary.Add(pair.Key, pair.Offset)
			} else {
				if len(currentFragments) == 0 || fragType > lastType {
					currentFragments = append(currentFragments, pair)
					lastType = fragType
				} else {
					full := stitchFragments(currentFragments)
					summary.Add(full.Key, full.Offset)
					currentFragments = []KeyOffsetPair{pair}
					lastType = fragType
				}
			}
		}
		cur = 0
	}

	if len(currentFragments) > 0 {
		full := stitchFragments(currentFragments)
		summary.Add(full.Key, full.Offset)
	}

	return summary
}

func readHeaderKey(blocks [][]byte, startBlockIndex int, startCursor int) (string, int, int, error) {
	var fragments [][]byte
	blockIndex := startBlockIndex
	cursor := startCursor

	blockSize := len(blocks[0])
	totalRead := 0

	for {
		if blockIndex >= len(blocks) {
			return "", blockIndex, cursor, fmt.Errorf("not enough blocks to read header key")
		}

		block := blocks[blockIndex]

		if cursor+9 > len(block) {
			blockIndex++
			cursor = 0
			continue
		}

		keySize := binary.LittleEndian.Uint64(block[cursor : cursor+8])
		fragType := block[cursor+8]
		cursor += 9
		totalRead += 9

		bytesRemaining := int(keySize)
		keyFragment := make([]byte, 0, keySize)

		for bytesRemaining > 0 {
			if blockIndex >= len(blocks) {
				return "", blockIndex, cursor, fmt.Errorf("not enough blocks for key fragment")
			}

			block = blocks[blockIndex]
			available := len(block) - cursor
			toRead := min(bytesRemaining, available)

			keyFragment = append(keyFragment, block[cursor:cursor+toRead]...)
			cursor += toRead
			totalRead += toRead
			bytesRemaining -= toRead

			if cursor == blockSize {
				blockIndex++
				cursor = 0
			}
		}

		fragments = append(fragments, keyFragment)

		if fragType == 0 {
			break
		}
	}

	if cursor > 0 && allZero(blocks[blockIndex][cursor:]) {
		blockIndex++
		cursor = 0
	}

	fullKey := []byte{}
	for _, f := range fragments {
		fullKey = append(fullKey, f...)
	}

	return string(fullKey), blockIndex, cursor, nil
}

func allZero(data []byte) bool {
	for _, b := range data {
		if b != 0 {
			return false
		}
	}
	return true
}

func (s *Summary) ReadSummary(offset uint64, filename string) error {
	file, err := os.Open(filename)
	if err != nil {
		return err
	}
	defer file.Close()

	_, err = file.Seek(int64(offset), 0)
	if err != nil {
		log.Fatal(err)
	}

	var minLen uint64
	err = binary.Read(file, binary.LittleEndian, &minLen)
	if err != nil {
		return err
	}

	minBytes := make([]byte, minLen)
	_, err = file.Read(minBytes)
	if err != nil {
		return err
	}
	s.Min = string(minBytes)

	var maxLen uint64
	err = binary.Read(file, binary.LittleEndian, &maxLen)
	if err != nil {
		return err
	}

	maxBytes := make([]byte, maxLen)
	_, err = file.Read(maxBytes)
	if err != nil {
		return err
	}

	s.Max = string(maxBytes)

	s.Keys = nil
	s.IndexOffsets = nil

	for {
		var keyLen uint64
		err = binary.Read(file, binary.LittleEndian, &keyLen)
		if err != nil {
			if err.Error() == "EOF" {
				break
			}
			return err
		}

		if keyLen == 0 {
			break
		}

		keyBytes := make([]byte, keyLen)
		_, err = file.Read(keyBytes)
		if err != nil {
			return err
		}
		key := string(keyBytes)

		var indexOffset uint64
		err = binary.Read(file, binary.LittleEndian, &indexOffset)
		if err != nil {
			return err
		}

		s.Keys = append(s.Keys, key)
		s.IndexOffsets = append(s.IndexOffsets, indexOffset)

	}
	return nil
}
