package main

import (
	"fmt"
)

func main() {
	// Define sample data records
	records := []DataRecord{
		{CRC: 12345, Key: "apple", Value: []byte("value1"), ValueSize: 6, Tombstone: false, Timestamp: 1},
		{CRC: 67890, Key: "banana", Value: []byte("value2"), ValueSize: 6, Tombstone: false, Timestamp: 2},
		{CRC: 11223, Key: "cherry", Value: []byte("value3"), ValueSize: 6, Tombstone: false, Timestamp: 3},
		{CRC: 44556, Key: "date", Value: []byte("value4"), ValueSize: 6, Tombstone: false, Timestamp: 4},
		{CRC: 77889, Key: "elderberry", Value: []byte("value5"), ValueSize: 6, Tombstone: true, Timestamp: 5},
	}

	// Create segments
	segments := []DataSegment{
		{Records: records[:2]}, // First segment
		{Records: records[2:]}, // Second segment
	}

	// Write segments to a file
	dataFilename := "data.db"
	keys, offsets := WriteSegmentsToFile(dataFilename, segments)

	fmt.Println("Segments written to file.")
	fmt.Println("Keys:", keys)
	fmt.Println("Offsets:", offsets)

	// Create an index file
	indexFilename := "index.db"
	index := &Index{Filename: indexFilename}
	for i, key := range keys {
		index.Add(key, uint64(offsets[i]))
	}
	index.Sort()
	summaryKeys, summaryOffsets := index.Write()

	// Create a summary file
	summaryFilename := "summary.db"
	summary := NewSummary(summaryKeys, summaryOffsets, summaryFilename, 2)
	fmt.Println("Summary file created.")

	// Test FindDataRecord using summary and index
	testKeys := []string{"apple", "cherry", "elderberry", "grape", "date"}
	for _, key := range testKeys {
		// Use summary to find the starting point in the index
		summaryFound, summaryOffset := summary.Find(key)
		if !summaryFound {
			fmt.Printf("Key '%s' not found in summary.\n", key)
		}

		// Use index to find the offset in the data file
		indexFound, dataOffset := index.FindIndex(key, summaryOffset)
		if !indexFound {
			fmt.Printf("Key '%s' not found in index.\n", key)
		}

		// Use the offset to find the record in the data file
		recordFound, record := FindDataRecord(key, dataOffset, dataFilename)
		if recordFound {
			fmt.Printf("Record found for key '%s': %+v\n", key, record)
		} else {
			fmt.Printf("Record not found for key '%s'.\n", key)
		}
	}
}
