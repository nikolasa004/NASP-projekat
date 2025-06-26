package sstable

import (
	"encoding/binary"
	"fmt"
	"log"
	"os"
	"project/BlockManager"
	"sort"
)

type KeyOffsetPair struct {
	Key    string
	Offset uint64
	KeyLen uint64
	Type   uint8
}

type Index struct {
	Entries  []KeyOffsetPair
	Filename string
}

func NewIndex(filename string, keys []string, offsets []uint64) *Index {
	index := Index{
		Filename: filename,
		Entries:  make([]KeyOffsetPair, 0),
	}

	for i, key := range keys {
		index.Add(key, offsets[i])
	}

	return &index
}

func CreateIndexRecordFragments(key string, offset uint64, blockSize int) [][]byte {
	binaryRecords := [][]byte{}

	keySize := uint64(len(key))

	constMaxSize := uint64(blockSize) - 17

	if keySize <= constMaxSize {
		binaryRecord := CreateFragmentedIndexRecord(key, offset, 0)
		binaryRecords = append(binaryRecords, binaryRecord)
	} else {
		fragmentedKeySize := constMaxSize
		firstFragmentation := true
		numberOfFragments := 1

		for {
			var newKey string
			if firstFragmentation && keySize > constMaxSize {
				newKey = key[:fragmentedKeySize]
			} else if !firstFragmentation && keySize > constMaxSize {
				newKey = key[:fragmentedKeySize]
			} else {
				newKey = key
			}
			binaryRecord := CreateFragmentedIndexRecord(newKey, offset, numberOfFragments)
			binaryRecords = append(binaryRecords, binaryRecord)

			if len(newKey) == len(key) {
				break
			}

			key = key[len(newKey):]
			keySize -= uint64(len(newKey))
			firstFragmentation = false
			numberOfFragments++
		}
	}
	return binaryRecords
}

func CreateFragmentedIndexRecord(key string, offset uint64, fragment int) []byte {
	bytesRecord := []byte{}

	keyBytes := []byte(key)
	keySize := uint64(len(keyBytes))

	// 1. Key size
	keySizeBytes := make([]byte, 8)
	binary.LittleEndian.PutUint64(keySizeBytes, keySize)
	bytesRecord = append(bytesRecord, keySizeBytes...)

	// 2. Fragment type
	typeBytes := []byte{byte(fragment)}
	bytesRecord = append(bytesRecord, typeBytes...)

	// 3. Offset
	offsetBytes := make([]byte, 8)
	binary.LittleEndian.PutUint64(offsetBytes, offset)
	bytesRecord = append(bytesRecord, offsetBytes...)

	// 4. Key
	bytesRecord = append(bytesRecord, keyBytes...)

	return bytesRecord
}

func (i *Index) Add(key string, offset uint64) {
	i.Entries = append(i.Entries, KeyOffsetPair{Key: key, Offset: offset})
}

func (i *Index) Sort() {
	sort.Slice(i.Entries, func(j, k int) bool {
		return i.Entries[j].Key < i.Entries[k].Key
	})
}

func (i *Index) Print() {
	for _, entry := range i.Entries {
		fmt.Printf("Key: %s, Offset: %d\n", entry.Key, entry.Offset)
	}
}

func (i *Index) Write(multipleFiles bool, offset uint64, blockManager *BlockManager.BlockManager) (keys []string, offsets []uint64, fileLen uint64) {
	currentOffset := uint64(0)

	offsets = make([]uint64, 0)

	file, err := os.OpenFile(i.Filename, os.O_APPEND|os.O_WRONLY, 0644)
	if err != nil {
		log.Fatal(err)
	}
	defer file.Close()

	if !multipleFiles {
		_, err = file.Seek(int64(offset), 0)
		if err != nil {
			log.Fatal(err)
		}
	}

	for _, entry := range i.Entries {
		offsets = append(offsets, currentOffset)
		keyLen := uint32(len(entry.Key))
		err := binary.Write(file, binary.LittleEndian, keyLen)

		if err != nil {
			log.Fatal(err)
		}
		currentOffset += uint64(4)

		_, err = file.Write([]byte(entry.Key))
		if err != nil {
			log.Fatal(err)
		}
		currentOffset += uint64(len(entry.Key))

		err = binary.Write(file, binary.LittleEndian, entry.Offset)
		if err != nil {
			log.Fatal(err)
		}
		currentOffset += uint64(8)

		keys = append(keys, entry.Key)
	}

	return keys, offsets, currentOffset
}

func (i *Index) WriteBM(multipleFiles bool, offset uint64, blockManager *BlockManager.BlockManager) (keys []string, offsets []uint64, fileLen uint64) {
	blockSize := blockManager.GetBlockSize()

	keyFragmentsMap := make(map[string][][]byte)

	for _, entry := range i.Entries {
		entryFragments := CreateIndexRecordFragments(entry.Key, entry.Offset, blockSize)

		keyFragmentsMap[entry.Key] = entryFragments
	}

	keys, offsets, fileLen = blockManager.PutSstableRawBlock(keyFragmentsMap, i.Filename, multipleFiles, offset, true)

	return keys, offsets, fileLen
}

func ReadIndexFromFile(bm *BlockManager.BlockManager, path string, offset uint64, length uint64) []KeyOffsetPair {
	blockSize := bm.GetBlockSize()
	var blocks [][]byte

	numBlocks := int(length / uint64(blockSize))
	if length%uint64(blockSize) != 0 {
		numBlocks++
	}

	for i := 0; i < numBlocks; i++ {
		block := bm.GetWalBlock(path, int(offset/uint64(blockSize))+i, blockSize, false)
		if block != nil {
			blocks = append(blocks, block)
		}
	}

	return ReadKeyOffsetPairsFromBlocks(blocks)
}

func ReadKeyOffsetPairsFromBlocks(blocks [][]byte) []KeyOffsetPair {
	var results []KeyOffsetPair
	var fragmentGroup []KeyOffsetPair
	var lastFragmentType uint8 = 0

	for _, block := range blocks {
		pairs := ReadKeyOffsetPairsFromBlock(block)
		for _, pair := range pairs {
			if pair.Type == 0 {
				// Kompletan zapis, bez fragmentacije
				results = append(results, pair)
			} else {
				// Fragmentirani zapis
				if len(fragmentGroup) == 0 || pair.Type > lastFragmentType {
					fragmentGroup = append(fragmentGroup, pair)
					lastFragmentType = pair.Type
				} else {
					// Novi fragment zapisa — prethodni je završen
					combined := stitchFragments(fragmentGroup)
					results = append(results, combined)
					fragmentGroup = []KeyOffsetPair{pair}
					lastFragmentType = pair.Type
				}
			}
		}
	}

	// Ako ostane neka grupa na kraju
	if len(fragmentGroup) > 0 {
		combined := stitchFragments(fragmentGroup)
		results = append(results, combined)
	}

	return results
}

func ReadKeyOffsetPairsFromBlock(block []byte) []KeyOffsetPair {
	var results []KeyOffsetPair
	cursor := 0

	for cursor+17 <= len(block) {
		// Parsiraj metapodatke
		keyLen := binary.LittleEndian.Uint64(block[cursor : cursor+8])
		fragType := uint8(block[cursor+8])
		offset := binary.LittleEndian.Uint64(block[cursor+9 : cursor+17])

		if cursor+17+int(keyLen) > len(block) {
			break // Oštećen blok ili padding
		}

		keyBytes := block[cursor+17 : cursor+17+int(keyLen)]

		if keyLen == 0 {
			break
		}

		results = append(results, KeyOffsetPair{
			Key:    string(keyBytes),
			Offset: offset,
			KeyLen: keyLen,
			Type:   fragType,
		})

		cursor += 17 + int(keyLen)
	}

	return results
}

func stitchFragments(fragments []KeyOffsetPair) KeyOffsetPair {
	fullKey := ""
	for _, f := range fragments {
		fullKey += f.Key
	}
	return KeyOffsetPair{
		Key:    fullKey,
		Offset: fragments[0].Offset,
		KeyLen: uint64(len(fullKey)),
		Type:   fragments[0].Type, // može i 0 ako želiš da kažeš "sastavljeno"
	}
}

func (i *Index) SearchIndex(key string, keyOffsetPairs []KeyOffsetPair) (bool, uint64) {
	found := false
	offset := uint64(0)

	for _, pair := range keyOffsetPairs {
		if pair.Key == key {
			found = true
			offset = pair.Offset
			break
		} else if pair.Key > key {
			break
		}
	}
	if !found {
		if len(keyOffsetPairs) > 0 {
			offset = keyOffsetPairs[len(keyOffsetPairs)-1].Offset
			found = true
		}
	}
	return found, offset
}

func (i *Index) ReadIndex(offset uint64, filename string) error {
	file, err := os.Open(filename)
	if err != nil {
		log.Fatal(err)
	}
	defer file.Close()

	_, err = file.Seek(int64(offset), 0)
	if err != nil {
		log.Fatal(err)
	}

	for {
		var keyLen uint32
		err := binary.Read(file, binary.LittleEndian, &keyLen)
		if err != nil {
			if err.Error() == "EOF" {
				break
			}
			log.Fatal(err)
		}

		keyB := make([]byte, keyLen)
		_, err = file.Read(keyB)
		if err != nil {
			log.Fatal(err)
		}

		var offset uint64
		err = binary.Read(file, binary.LittleEndian, &offset)
		if err != nil {
			log.Fatal(err)
		}

		i.Add(string(keyB), offset)
	}

	return nil
}

func (i *Index) FindIndex(key string, startOffset uint64, filename string) (found bool, offset uint64) {
	found = false
	offset = 0

	file, err := os.Open(filename)
	if err != nil {
		log.Fatal(err)
	}
	defer file.Close()
	_, err = file.Seek(int64(startOffset), 0)
	if err != nil {
		log.Fatal(err)
	}

	var closestOffset uint64
	var closestKey string

	for {
		var keyLen uint32
		err := binary.Read(file, binary.LittleEndian, &keyLen)
		if err != nil {
			if err.Error() == "EOF" {
				fmt.Println("EOF")
				break
			}
			log.Fatal(err)
		}

		keyB := make([]byte, keyLen)
		_, err = file.Read(keyB)
		if err != nil {
			log.Fatal(err)
		}

		currentKey := string(keyB)

		var offsetC uint64
		err = binary.Read(file, binary.LittleEndian, &offsetC)
		if err != nil {
			log.Fatal(err)
		}

		if currentKey == key {
			found = true
			offset = offsetC
			return found, offset
		}
		if currentKey < key {
			closestKey = currentKey
			closestOffset = offsetC
			break
		} else {
			break
		}
	}

	if closestKey != "" {
		found = true
		offset = closestOffset
	}
	return found, offset
}
