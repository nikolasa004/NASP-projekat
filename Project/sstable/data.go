package main

import (
	"encoding/binary"
	"fmt"
	"hash/crc32"
	"io"
	"log"
	"os"
	"sort"
)

type DataRecord struct {
	CRC       uint32
	Key       string
	Value     []byte
	ValueSize uint32
	Tombstone bool
	Timestamp uint64
}

type DataSegment struct {
	Offset  uint
	Records []DataRecord
}

func CRC32(data []byte) uint32 { //funkcija koja racuna CRC32 vrednost za zadati niz bajtova
	return crc32.ChecksumIEEE(data)
}

func NewDataRecord(key string, value []byte, tombstone bool, timestamp uint64) DataRecord {
	record := DataRecord{
		Key:       key,
		Value:     value,
		ValueSize: uint32(len(value)),
		Tombstone: tombstone,
		Timestamp: timestamp,
	}

	record.CRC = CRC32(value)
	return record
}

func (ds *DataSegment) Print() {
	for i, record := range ds.Records {
		fmt.Println("Record ", i+1, " ", record)
	}
}

func (ds *DataSegment) Add(record DataRecord) {
	ds.Records = append(ds.Records, record)
}

func SortRecords(records []DataRecord) {
	sort.Slice(records, func(i, j int) bool {
		return records[i].Key < records[j].Key
	})
}

func WriteSegmentsToFile(filename string, segments []DataSegment) ([]string, []uint64) {
	file, err := os.Create(filename)
	if err != nil {
		log.Fatal(err)
	}

	keys := make([]string, 0)
	offsets := make([]uint64, 0)
	currentOffset := uint64(0)

	for _, segment := range segments {
		for j, record := range segment.Records {
			if j == 0 {
				keys = append(keys, record.Key)
				offsets = append(offsets, currentOffset)
			}
			err = binary.Write(file, binary.LittleEndian, record.CRC)
			if err != nil {
				log.Fatal(err)
			}
			currentOffset += 4

			err = binary.Write(file, binary.LittleEndian, record.Timestamp)
			if err != nil {
				log.Fatal(err)
			}
			currentOffset += 8
			if record.Tombstone {
				err = binary.Write(file, binary.LittleEndian, byte(1))
			} else {
				err = binary.Write(file, binary.LittleEndian, byte(0))
			}
			if err != nil {
				log.Fatal(err)
			}
			currentOffset += 1

			keyLen := uint32(len(record.Key))
			err = binary.Write(file, binary.LittleEndian, keyLen)
			if err != nil {
				log.Fatal(err)
			}
			currentOffset += 4

			_, err = file.Write([]byte(record.Key))
			if err != nil {
				log.Fatal(err)
			}
			currentOffset += uint64(len(record.Key))

			if !record.Tombstone {
				err = binary.Write(file, binary.LittleEndian, record.ValueSize)
				if err != nil {
					log.Fatal(err)
				}
				currentOffset += 4

				_, err = file.Write(record.Value)
				if err != nil {
					log.Fatal(err)
				}
				currentOffset += uint64(len(record.Value))
			}
		}
	}
	return keys, offsets
}

func FindDataRecord(key string, segmentOffset uint64, filename string) (DataRecord, bool) {
	file, err := os.Open(filename)
	if err != nil {
		log.Fatal(err)
	}
	defer file.Close()

	_, err = file.Seek(int64(segmentOffset), io.SeekStart)
	if err != nil {
		log.Fatal(err)
	}

	for {
		var record DataRecord

		// Čitanje CRC
		err = binary.Read(file, binary.LittleEndian, &record.CRC)
		if err != nil {
			if err == io.EOF {
				break
			}
			log.Fatal(err)
		}
		segmentOffset += 4

		// Čitanje Timestamp
		err = binary.Read(file, binary.LittleEndian, &record.Timestamp)
		if err != nil {
			log.Fatal(err)
		}
		segmentOffset += 8

		// Čitanje Tombstone
		var tombstoneByte byte
		err = binary.Read(file, binary.LittleEndian, &tombstoneByte)
		if err != nil {
			log.Fatal(err)
		}
		record.Tombstone = tombstoneByte == 1
		segmentOffset += 1

		// Čitanje dužine ključa
		var keyLen uint32
		err = binary.Read(file, binary.LittleEndian, &keyLen)
		if err != nil {
			log.Fatal(err)
		}
		segmentOffset += 4

		// Čitanje ključa
		keyBytes := make([]byte, keyLen)
		_, err = file.Read(keyBytes)
		if err != nil {
			log.Fatal(err)
		}
		record.Key = string(keyBytes)
		segmentOffset += uint64(keyLen)

		// Čitanje vrednosti ako nije tombstone
		if !record.Tombstone {
			err = binary.Read(file, binary.LittleEndian, &record.ValueSize)
			if err != nil {
				log.Fatal(err)
			}
			segmentOffset += 4

			record.Value = make([]byte, record.ValueSize)
			_, err = file.Read(record.Value)
			if err != nil {
				log.Fatal(err)
			}
			segmentOffset += uint64(record.ValueSize)
		}

		// Provera ključa
		if record.Key == key {
			return record, true
		}

		if key < record.Key {
			return DataRecord{}, false
		}
	}
	return DataRecord{}, false
}
