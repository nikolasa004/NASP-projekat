package main

import (
	"encoding/binary"
	"fmt"
	"log"
	"os"
	"sort"
)

type KeyOffsetPair struct {
	Key    string
	Offset uint64
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

func (i *Index) Write(multipleFiles bool, offset uint64) (keys []string, offsets []uint64, fileLen uint64) {
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

		fmt.Println(currentOffset)

		keys = append(keys, entry.Key)
	}

	fmt.Println(offsets)

	return keys, offsets, currentOffset
}

func (i *Index) FindIndex(key string, startOffset uint64) (found bool, offset uint64) {
	found = false
	offset = 0

	file, err := os.Open(i.Filename)
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
		} else {
			// If currentKey > key, stop searching as the index keys are sorted
			break
		}
	}

	// If no exact match, return the closest preceding key and its offset
	if closestKey != "" {
		found = false
		offset = closestOffset
	}
	return found, offset
}
