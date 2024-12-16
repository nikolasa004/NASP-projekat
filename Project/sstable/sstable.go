package main

import (
	"encoding/binary"
	"encoding/gob"
	"log"
	"os"
)

type Sstable struct {
	Filename        string
	DataFilename    string
	IndexFilename   string
	SummaryFilename string
	FilterFilename  string
}

type Metadata struct {
	DataOffset    uint64
	DataLength    uint64
	IndexOffset   uint64
	IndexLength   uint64
	SummaryOffset uint64
	SummaryLength uint64
	FilterOffset  uint64
	FilterLength  uint64
}

func NewSstable(filename string, data []DataRecord, sampleRate int, compaction bool) *Sstable {

	sstable := &Sstable{
		Filename:        filename,
		DataFilename:    filename + "-data.bin",
		IndexFilename:   filename + "-index.bin",
		SummaryFilename: filename + "-summary.bin",
		FilterFilename:  filename + "-filter.bin",
	}

	// Kreiranje fajlova
	createFiles(filename, true)

	// Ovo su potreni podaci za kreiranje svih potrebnih struktura
	filter := NewBloomFilter(len(data), 0.01)

	for _, record := range data {
		filter.Add(record.Key)
	}
	// Serijalizacija filtera
	filter.Serialize(sstable.FilterFilename)

	// Kreiranje segmenata i upis data fajla
	segments := makeSegments(data, 5)
	keys, offsets, _ := WriteSegmentsToFile(sstable.DataFilename, segments, true, 0)

	// Kreiranje indexa i upis index fajla
	index := NewIndex(sstable.IndexFilename, keys, offsets)
	index.Sort()
	index.Print()
	summaryKeys, summaryOffsets, _ := index.Write(true, 0)

	// Kreiranje summary fajla i upis summary fajla
	summary := NewSummary(summaryKeys, summaryOffsets, sstable.SummaryFilename, sampleRate)
	summary.Print()
	summary.Write(true, 0)

	return sstable
}
func NewSstableOneFile(filename string, data []DataRecord, sampleRate int, compaction bool) *Sstable {
	sstable := &Sstable{
		Filename: filename + ".bin",
	}

	// Kreiranje fajla
	createFiles(filename, false)

	var metadata Metadata
	var currentOffset uint64

	filter := NewBloomFilter(len(data), 0.01)
	for _, record := range data {
		filter.Add(record.Key)
	}
	metadata.FilterOffset = currentOffset
	metadata.FilterLength, _ = filter.Serialize(sstable.Filename)
	currentOffset += metadata.FilterLength

	metadata.DataOffset = currentOffset
	segments := makeSegments(data, 5)
	keys, offsets, dataLength := WriteSegmentsToFile(sstable.Filename, segments, false, currentOffset)
	metadata.DataLength = dataLength
	currentOffset += dataLength

	metadata.IndexOffset = currentOffset
	index := NewIndex("", keys, offsets)
	index.Sort()
	summaryKeys, summaryOffsets, indexLength := index.Write(false, currentOffset)
	metadata.IndexLength = indexLength
	currentOffset += indexLength

	metadata.SummaryOffset = currentOffset
	summary := NewSummary(summaryKeys, summaryOffsets, "", sampleRate)
	summaryLength := summary.Write(false, currentOffset)
	metadata.SummaryLength = summaryLength
	currentOffset += summaryLength

	file, err := os.OpenFile(sstable.Filename, os.O_APPEND|os.O_WRONLY, 0644)
	if err != nil {
		log.Fatal(err)
	}
	defer file.Close()

	// Serijalizacija metapodataka
	encoder := gob.NewEncoder(file)
	err = encoder.Encode(metadata)
	if err != nil {
		log.Fatal(err)
	}

	// Zapis veličine metapodataka na kraj fajla
	err = binary.Write(file, binary.LittleEndian, uint64(binary.Size(metadata)))
	if err != nil {
		log.Fatal(err)
	}

	return sstable
}

func createFiles(filename string, multipleFiles bool) {
	if multipleFiles {
		dataFilename := filename + "-data.bin"
		indexFilename := filename + "-index.bin"
		summaryFilename := filename + "-summary.bin"
		filterFilename := filename + "-filter.bin"

		dataFile, err := os.Create(dataFilename)
		if err != nil {
			log.Fatal(err)
		}
		dataFile.Close()

		indexFile, err := os.Create(indexFilename)
		if err != nil {
			log.Fatal(err)
		}
		indexFile.Close()

		summaryFile, err := os.Create(summaryFilename)
		if err != nil {
			log.Fatal(err)
		}
		summaryFile.Close()

		filterFile, err := os.Create(filterFilename)
		if err != nil {
			log.Fatal(err)
		}
		filterFile.Close()
	} else {
		file, err := os.Create(filename + ".bin")
		if err != nil {
			log.Fatal(err)
		}
		file.Close()
	}

}
