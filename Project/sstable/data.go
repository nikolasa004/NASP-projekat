package main

import (
	"bufio"
	"encoding/binary"
	"hash/crc32"
	"log"
	"os"
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

func (ds *DataSegment) Add(record DataRecord) {
	ds.Records = append(ds.Records, record)
}

func WriteSegmentsToFile(filename string, segments []DataSegment) (segmentKeys []string, segmentOffsets []uint64) {
	file, err := os.Create(filename)

	if err != nil {
		log.Fatal(err)
	}
	defer file.Close()

	writer := bufio.NewWriter(file)
	currentOffset := uint64(0)
	// Za svaki segment belezim prvi kljuc i offset zbog kasnijeg formiranja indexa
	segmentKeys = make([]string, 0)
	segmentOffsets = make([]uint64, 0)

	for _, segment := range segments {

		if len(segment.Records) > 0 {
			segmentKeys = append(segmentKeys, segment.Records[0].Key)
			segmentOffsets = append(segmentOffsets, currentOffset)
		}

		for _, record := range segment.Records {

			err := binary.Write(writer, binary.LittleEndian, record.CRC)
			if err != nil {
				log.Fatal(err)
			}
			currentOffset += uint64(4)

			err = binary.Write(writer, binary.LittleEndian, record.Timestamp)
			if err != nil {
				log.Fatal(err)
			}
			currentOffset += uint64(8)

			tombstoneByte := byte(0)
			if record.Tombstone {
				tombstoneByte = 1
			}
			err = binary.Write(writer, binary.LittleEndian, tombstoneByte)
			if err != nil {
				log.Fatal(err)
			}
			currentOffset += uint64(1)

			keyLen := uint32(len(record.Key))
			err = binary.Write(writer, binary.LittleEndian, keyLen)
			if err != nil {
				log.Fatal(err)
			}
			currentOffset += uint64(4)

			_, err = writer.Write([]byte(record.Key))
			if err != nil {
				log.Fatal(err)
			}
			currentOffset += uint64(len(record.Key))

			// Piši Value dužinu i Value (ako Tombstone nije aktivan)
			if !record.Tombstone {
				err = binary.Write(writer, binary.LittleEndian, record.ValueSize)
				if err != nil {
					log.Fatal(err)
				}
				currentOffset += uint64(4)

				_, err = writer.Write(record.Value)
				if err != nil {
					log.Fatal(err)
				}
				currentOffset += uint64(record.ValueSize)

			}
		}
	}
	err = writer.Flush()
	if err != nil {
		log.Fatal(err)
	}

	return segmentKeys, segmentOffsets
}

func FindDataRecord(key string, offset uint64, filename string) (bool, DataRecord) {
	found := false
	var record DataRecord

	file, err := os.Open(filename)
	if err != nil {
		log.Fatal(err)
	}
	defer file.Close()

	// Seek to the starting offset of the segment
	_, err = file.Seek(int64(offset), 0)
	if err != nil {
		log.Fatal(err)
	}

	reader := bufio.NewReader(file)

	// Loop through all records in the segment
	for {
		// Read CRC
		err = binary.Read(reader, binary.LittleEndian, &record.CRC)
		if err != nil {
			// EOF or end of segment
			if err.Error() == "EOF" {
				break
			}
			log.Fatal(err)
		}

		// Read Timestamp
		err = binary.Read(reader, binary.LittleEndian, &record.Timestamp)
		if err != nil {
			log.Fatal(err)
		}

		var tombstoneByte byte
		err = binary.Read(reader, binary.LittleEndian, &tombstoneByte)
		if err != nil {
			log.Fatal(err)
		}
		record.Tombstone = tombstoneByte == 1

		var keyLen uint32
		err = binary.Read(reader, binary.LittleEndian, &keyLen)
		if err != nil {
			log.Fatal(err)
		}

		keyBytes := make([]byte, keyLen)
		_, err = reader.Read(keyBytes)
		if err != nil {
			log.Fatal(err)
		}
		record.Key = string(keyBytes)

		if record.Key == key {
			if !record.Tombstone {
				err = binary.Read(reader, binary.LittleEndian, &record.ValueSize)
				if err != nil {
					log.Fatal(err)
				}

				record.Value = make([]byte, record.ValueSize)
				_, err = reader.Read(record.Value)
				if err != nil {
					log.Fatal(err)
				}
			}
			found = true
			break
		}

		if !record.Tombstone {
			var valueSize uint32
			err = binary.Read(reader, binary.LittleEndian, &valueSize)
			if err != nil {
				log.Fatal(err)
			}
			_, err = reader.Discard(int(valueSize))
			if err != nil {
				log.Fatal(err)
			}
		}
	}

	return found, record
}
