package main

import (
	"bufio"
	"encoding/binary"
	"fmt"
	"io"
	"log"
	"os"
)

type TOC struct {
	SingleFile bool
	Files      []string
}

type Sstable struct {
	Filename        string
	DataFilename    string
	IndexFilename   string
	SummaryFilename string
	FilterFilename  string
}

type Metadata struct {
	IsSingleFile  uint64
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

	// TOC fajl
	toc := TOC{
		Files: []string{sstable.DataFilename, sstable.IndexFilename, sstable.SummaryFilename, sstable.FilterFilename},
	}

	err := WriteTOCFile(filename, false, toc.Files)

	if err != nil {
		log.Fatal(err)
	}

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

	toc := TOC{
		SingleFile: true,
		Files:      []string{sstable.Filename},
	}

	err := WriteTOCFile(filename, toc.SingleFile, toc.Files)
	if err != nil {
		log.Fatal(err)
	}

	info, _ := os.Stat(sstable.Filename)
	print("File size: ", info.Size(), "\n")

	var metadata Metadata
	var currentOffset uint64

	filter := NewBloomFilter(len(data), 0.01)
	for _, record := range data {
		filter.Add(record.Key)
	}
	metadata.FilterOffset = currentOffset
	metadata.FilterLength, _ = filter.Serialize(sstable.Filename)
	info, _ = os.Stat(sstable.Filename)
	currentOffset += uint64(info.Size())

	metadata.DataOffset = currentOffset
	segments := makeSegments(data, 5)
	keys, offsets, dataLength := WriteSegmentsToFile(sstable.Filename, segments, false, currentOffset)
	metadata.DataLength = dataLength
	currentOffset += dataLength
	info, _ = os.Stat(sstable.Filename)
	print("File size: ", info.Size(), "\n")

	metadata.IndexOffset = currentOffset
	index := NewIndex(sstable.Filename, keys, offsets)
	index.Sort()
	summaryKeys, summaryOffsets, indexLength := index.Write(false, currentOffset)
	metadata.IndexLength = indexLength
	currentOffset += indexLength
	info, _ = os.Stat(sstable.Filename)
	print("File size: ", info.Size(), "\n")

	metadata.SummaryOffset = currentOffset
	summary := NewSummary(summaryKeys, summaryOffsets, sstable.Filename, sampleRate)
	summaryLength := summary.Write(false, currentOffset)
	metadata.SummaryLength = summaryLength
	currentOffset += summaryLength
	info, _ = os.Stat(sstable.Filename)
	print("File size: ", info.Size(), "\n")

	fmt.Println("Filter offset: ", metadata.FilterOffset, " Filter length: ", metadata.FilterLength)
	fmt.Println("Data offset: ", metadata.DataOffset, " Data length: ", metadata.DataLength)
	fmt.Println("Index offset: ", metadata.IndexOffset, " Index length: ", metadata.IndexLength)
	fmt.Println("Summary offset: ", metadata.SummaryOffset, " Summary length: ", metadata.SummaryLength)

	err = WriteMetadata(sstable.Filename, metadata)

	if err != nil {
		log.Fatal(err)
	}

	return sstable
}
func WriteMetadata(filename string, metadata Metadata) error {
	file, err := os.OpenFile(filename, os.O_APPEND|os.O_WRONLY, 0644)
	if err != nil {
		return err
	}
	defer file.Close()

	// Pozicioniraj se na kraj fajla
	_, err = file.Seek(0, io.SeekEnd)
	if err != nil {
		return err
	}

	err = binary.Write(file, binary.LittleEndian, metadata.IsSingleFile)
	if err != nil {
		return err
	}

	err = binary.Write(file, binary.LittleEndian, metadata.DataOffset)
	if err != nil {
		return err
	}
	err = binary.Write(file, binary.LittleEndian, metadata.DataLength)
	if err != nil {
		return err
	}

	err = binary.Write(file, binary.LittleEndian, metadata.IndexOffset)
	if err != nil {
		return err
	}
	err = binary.Write(file, binary.LittleEndian, metadata.IndexLength)
	if err != nil {
		return err
	}
	err = binary.Write(file, binary.LittleEndian, metadata.SummaryOffset)
	if err != nil {
		return err
	}
	err = binary.Write(file, binary.LittleEndian, metadata.SummaryLength)
	if err != nil {
		return err
	}

	err = binary.Write(file, binary.LittleEndian, metadata.FilterOffset)
	if err != nil {
		return err
	}
	err = binary.Write(file, binary.LittleEndian, metadata.FilterLength)
	if err != nil {
		return err
	}

	fmt.Printf("Written metadata: %+v\n", metadata)
	return nil
}

func ReadMetadata(filename string) (Metadata, error) {
	var metadata Metadata

	file, err := os.Open(filename)
	if err != nil {
		return metadata, err
	}
	defer file.Close()

	// Čitaj veličinu fajla
	stat, err := file.Stat()
	if err != nil {
		return metadata, err
	}

	fileSize := stat.Size()
	if fileSize < 80 { // Provera minimalne veličine
		return metadata, fmt.Errorf("file too small to contain metadata")
	}

	// Pozicioniraj pokazivač na početak metapodataka
	_, err = file.Seek(fileSize-72, io.SeekStart)
	if err != nil {
		return metadata, err
	}

	// Čitaj polja redom kako su zapisana
	err = binary.Read(file, binary.LittleEndian, &metadata.IsSingleFile)
	if err != nil {
		return metadata, err
	}

	err = binary.Read(file, binary.LittleEndian, &metadata.DataOffset)
	if err != nil {
		return metadata, err
	}

	err = binary.Read(file, binary.LittleEndian, &metadata.DataLength)
	if err != nil {
		return metadata, err
	}

	err = binary.Read(file, binary.LittleEndian, &metadata.IndexOffset)
	if err != nil {
		return metadata, err
	}

	err = binary.Read(file, binary.LittleEndian, &metadata.IndexLength)
	if err != nil {
		return metadata, err
	}

	err = binary.Read(file, binary.LittleEndian, &metadata.SummaryOffset)
	if err != nil {
		return metadata, err
	}

	err = binary.Read(file, binary.LittleEndian, &metadata.SummaryLength)
	if err != nil {
		return metadata, err
	}

	err = binary.Read(file, binary.LittleEndian, &metadata.FilterOffset)
	if err != nil {
		return metadata, err
	}

	err = binary.Read(file, binary.LittleEndian, &metadata.FilterLength)
	if err != nil {
		return metadata, err
	}

	fmt.Printf("Read metadata: %+v\n", metadata)
	return metadata, nil
}

func WriteSstable(filename string, data []DataRecord, sampleRate int, compaction bool, oneFile bool) *Sstable {
	if oneFile {
		return NewSstableOneFile(filename, data, sampleRate, compaction)
	}
	return NewSstable(filename, data, sampleRate, compaction)
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

func FindInSstable(key string, filename string) (DataRecord, bool) {
	var toc TOC
	var err error
	toc, err = ReadTOCFile(filename)
	if err != nil {
		log.Fatal(err)
	}
	oneFile := toc.SingleFile
	files := toc.Files
	if oneFile {
		filename = files[0]
		file, err := os.Open(filename)
		if err != nil {
			log.Fatal(err)
		}
		defer file.Close()

		metadata, err := ReadMetadata(filename)
		if err != nil {
			log.Fatal(err)
		}

		// Proveri da li je ključ u filteru
		filter, err := Deserialize(metadata.FilterOffset, metadata.FilterLength, filename)
		if err != nil {
			log.Fatal(err)
		}

		if !filter.Check(key) {
			return DataRecord{}, false
		}

		// Proveri da li je ključ u summary-ju
		summary := &Summary{}
		summary.ReadSummary(metadata.SummaryOffset, filename)
		found, offset := summary.Find(key)

		if !found {
			return DataRecord{}, false
		}

		// Proveri da li je ključ u indexu
		var index *Index
		found, offset = index.FindIndex(key, metadata.IndexOffset+offset, filename)
		if !found {
			return DataRecord{}, false
		}

		// Pročitaj podatke
		record, found := FindDataRecord(key, metadata.DataOffset+offset, filename)
		if !found {
			return DataRecord{}, false
		}

		return record, true

	} else {
		datafile := files[0]
		indexfile := files[1]
		summaryfile := files[2]
		filterfile := files[3]

		// Proveri da li je ključ u filteru
		filter, err := Deserialize(0, 0, filterfile)
		if err != nil {
			log.Fatal(err)
		}

		if !filter.Check(key) {
			return DataRecord{}, false
		}

		// Proveri da li je ključ u summary-ju
		summary := &Summary{}
		summary.ReadSummary(0, summaryfile)
		found, offsetS := summary.Find(key)

		if !found {
			return DataRecord{}, false
		}

		// Proveri da li je ključ u indexu
		var index *Index
		found, offset := index.FindIndex(key, offsetS, indexfile)
		if !found {
			return DataRecord{}, false
		}

		// Pročitaj podatke
		record, found := FindDataRecord(key, offset, datafile)
		if !found {
			return DataRecord{}, false
		}

		return record, true
	}
}

// Napravio sam TOC file kako bi lakse mogao da se pristupi svim fajlovima
func WriteTOCFile(filename string, singleFile bool, files []string) error {
	file, err := os.Create(filename + "-TOC.txt")
	if err != nil {
		return err
	}
	defer file.Close()

	writer := bufio.NewWriter(file)

	mode := "0"
	if singleFile {
		mode = "1"
	}

	_, err = writer.WriteString(mode + "\n")
	if err != nil {
		return err
	}

	for _, f := range files {
		_, err = writer.WriteString(f + "\n")
		if err != nil {
			return err
		}
	}

	return writer.Flush()
}

func ReadTOCFile(filename string) (TOC, error) {
	var toc TOC

	file, err := os.Open(filename)
	if err != nil {
		return toc, err
	}
	defer file.Close()

	reader := bufio.NewReader(file)

	mode, err := reader.ReadString('\n')
	if err != nil {
		return toc, err
	}

	toc.SingleFile = mode[0] == '1'

	for {
		line, err := reader.ReadString('\n')
		if err != nil {
			if err == io.EOF {
				break
			}
			return toc, err
		}

		toc.Files = append(toc.Files, line[:len(line)-1])
	}

	return toc, nil
}
