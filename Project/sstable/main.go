package sstable

import "fmt"

func main() {
	records := []DataRecord{
		NewDataRecord("apple", []byte("value1"), false, 1),
		NewDataRecord("banana", []byte("value2"), false, 2),
		NewDataRecord("cherry", []byte("value3"), false, 3),
		NewDataRecord("date", []byte("value4"), false, 4),
		NewDataRecord("elderberry", []byte("value5"), true, 5),
		NewDataRecord("fig", []byte("value6"), false, 6),
		NewDataRecord("grape", []byte("value7"), false, 7),
		NewDataRecord("honeydew", []byte("value8"), false, 8),
		NewDataRecord("kiwi", []byte("value9"), false, 9),
		NewDataRecord("lemon", []byte("value10"), false, 10),
		NewDataRecord("mango", []byte("value11"), true, 11),
		NewDataRecord("nectarine", []byte("value12"), false, 12),
		NewDataRecord("orange", []byte("value13"), false, 13),
		NewDataRecord("papaya", []byte("value14"), true, 14),
		NewDataRecord("quince", []byte("value15"), false, 15),
		NewDataRecord("raspberry", []byte("value16"), false, 16),
		NewDataRecord("strawberry", []byte("value17"), false, 17),
		NewDataRecord("tangerine", []byte("value18"), true, 18),
		NewDataRecord("ugli", []byte("value19"), false, 19),
		NewDataRecord("vanilla", []byte("value20"), false, 20),
		NewDataRecord("watermelon", []byte("value21"), false, 21),
		NewDataRecord("xigua", []byte("value22"), true, 22),
		NewDataRecord("yellowfruit", []byte("value23"), false, 23),
		NewDataRecord("zucchini", []byte("value24"), false, 24),
		NewDataRecord("avocado", []byte("value25"), false, 25),
		NewDataRecord("blackberry", []byte("value26"), false, 26),
		NewDataRecord("cranberry", []byte("value27"), true, 27),
		NewDataRecord("dragonfruit", []byte("value28"), false, 28),
	}

	// Write records to a file
	WriteSstable( records, 5, false, true)

	WriteSstable(records, 5, false, false)

	// print(sstable, sstable1)

	record, found := FindInSstable("vanilla", "sstable1-TOC.txt")

	record1, found1 := FindInSstable("vanilla", "sstable2-TOC.txt")

	fmt.Println(record, found)

	fmt.Println(record1, found1)

	// Sort records by key

	SortRecords(records)

	// Create segmentssegments := make([]DataSegment, 0)
	// segments := make([]DataSegment, 0)
	// var currentSegment DataSegment

	// 	for i, record := range records {
	// 		currentSegment.Add(record)

	// 		// When the current segment has 5 records, finalize and start a new Println()
	// 		if (i+1)%5 == 0 || i == len(records)-1 {
	// 			segments = append(segments, currentSegment)
	// 			currentSegment.Print()
	// 			currentSegment = DataSegment{}
	// 		}
	// 	}

	// 	// Write segments to a file
	// 	dataFilename := "data.db"
	// 	keys, offsets := WriteSegmentsToFile(dataFilename, segments)

	// 	fmt.Println("Segments written to file.")
	// 	fmt.Println("Keys:", keys)
	// 	fmt.Println("Offsets:", offsets)

	// 	// Create an index file
	// 	indexFilename := "index.db"
	// 	index := NewIndex(indexFilename, keys, offsets)

	// 	index.Sort()
	// 	summaryKeys, summaryOffsets := index.Write()

	// 	fmt.Println("Keys:", summaryKeys)
	// 	fmt.Println("Offsets:", summaryOffsets)

	// 	// Create a summary file
	// 	summaryFilename := "summary.db"
	// 	summary := NewSummary(summaryKeys, summaryOffsets, summaryFilename, 6)
	// 	summary.Print()
	// 	fmt.Println("Summary file created.")

	// 	// Test FindDataRecord using summary and index
	// 	testKeys := []string{"apple", "avocado", "banana", "cherry", "elderberry", "grape", "date", "watermelon", "banana", "kiwi", "fig", "xigua", "d"}
	// 	for _, key := range testKeys {
	// 		_, summaryOffset := summary.Find(key)

	// 		found, dataOffset := index.FindIndex(key, summaryOffset)

	// 		fmt.Println(dataOffset, "dataOffset", found, "found")

	// 		// Use the offset to find the record in the data file
	// 		fmt.Println(dataOffset, "dataOffset")
	// 		record, recordFound := FindDataRecord(key, dataOffset, dataFilename)
	// 		if recordFound {
	// 			fmt.Printf("Record found for key '%s': %+v\n", key, record)
	// 		} else {
	// 			fmt.Printf("Record not found for key '%s'.\n", key)
	// 		}
	// 	}
}
