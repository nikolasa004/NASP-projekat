package sstable

import (
	"encoding/binary"
	"fmt"
	"log"
	"os"
)

type Summary struct {
	Keys         []string
	IndexOffsets []uint64
	Filename     string
	Min          string
	Max          string
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
	summary := &Summary{
		Filename: filename,
		Min:      keys[0],
		Max:      keys[len(keys)-1],
	}
	for i := range offsets {
		if i == 0 || i%sampleRate == 0 {
			summary.Add(keys[i], offsets[i])
		}
	}

	return summary
}

func (s *Summary) Add(key string, offset uint64) {
	s.Keys = append(s.Keys, key)
	s.IndexOffsets = append(s.IndexOffsets, offset)
	fmt.Print("Inserted" + "\n")
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
	fmt.Println("Min: ", string(minBytes))
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
