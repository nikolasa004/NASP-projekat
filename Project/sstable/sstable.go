package sstable

import (
	"bufio"
	"encoding/binary"
	"fmt"
	"io"
	"log"
	"os"
	"project/BlockManager"
	"project/Memtable"
	"strings"
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

func NewRecord(key string, value []byte, tombstone bool, timestamp uint64) DataRecord {
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

func GenerateSstableFilenames(baseDir string, tableID int) map[string]string {
	filenames := map[string]string{
		"Data":    fmt.Sprintf("%s/sstable-%04d/sstable-%04d-Data.bin", baseDir, tableID, tableID),
		"Filter":  fmt.Sprintf("%s/sstable-%04d/sstable-%04d-Filter.bin", baseDir, tableID, tableID),
		"Index":   fmt.Sprintf("%s/sstable-%04d/sstable-%04d-Index.bin", baseDir, tableID, tableID),
		"Summary": fmt.Sprintf("%s/sstable-%04d/sstable-%04d-Summary.bin", baseDir, tableID, tableID),
		"TOC":     fmt.Sprintf("%s/sstable-%04d/sstable-%04d-TOC.txt", baseDir, tableID, tableID),
	}

	return filenames
}

func NewSstable(data []*Memtable.MemtableRecord, sampleRate int, compaction bool, blockManager *BlockManager.BlockManager, dictionary *Dictionary) *Sstable {

	tableId, err := GetNextTableID()
	if err != nil {
		log.Fatal(err)
	}

	tableDir := fmt.Sprintf("data/sstable/sstable-%04d", tableId)
	err = os.MkdirAll(tableDir, os.ModePerm)
	if err != nil {
		log.Fatalf("Failed to create directory for SSTable: %v", err)
	}

	filenames := GenerateSstableFilenames("data/sstable", tableId)

	sstable := &Sstable{
		Filename:        filenames["TOC"],
		DataFilename:    filenames["Data"],
		IndexFilename:   filenames["Index"],
		SummaryFilename: filenames["Summary"],
		FilterFilename:  filenames["Filter"],
	}

	// Kreiranje fajlova
	createFiles(tableId, false)

	// TOC fajl
	toc := TOC{
		Files: []string{sstable.DataFilename, sstable.IndexFilename, sstable.SummaryFilename, sstable.FilterFilename},
	}

	err = WriteTOCFile(filenames["TOC"], false, toc.Files)

	if err != nil {
		log.Fatal(err)
	}

	for _, record := range data {
		val := dictionary.Put(record.Key) // Dodavanje ključeva u rečnik
		record.Key = fmt.Sprintf("%d", val) // Mapiranje ključeva na nove vrednosti
	}
	err = blockManager.WriteDictionary(dictionary.Serialize(), dictionary.filename)
	if err != nil {
		log.Fatalf("Failed to write dictionary: %v", err)
	}

	// Ovo su potreni podaci za kreiranje svih potrebnih struktura
	filter := NewBloomFilter(len(data), 0.01)

	for _, record := range data {
		filter.Add(record.Key)
	}
	// Serijalizacija filtera
	bytes, err := filter.Serialize()
	blockManager.WriteBloomFilter(bytes, sstable.FilterFilename)
	if err != nil {
		log.Fatalf("Failed to write Bloom filter: %v", err)
	}

	// Kreiranje blokova i upis preko block manager-a
	blockSize := blockManager.GetBlockSize() // za sada
	sstableRecords := make(map[string][][]byte)

	for _, record := range data {
		fragments := BlockManager.CreateBinaryRecordsWithFragmentation(record.Tombstone, record.Key, record.Value, blockSize, true)
		sstableRecords[record.Key] = fragments
	}
	keys, offsets, _ := blockManager.PutSstableRecords(sstableRecords, tableId, sstable.DataFilename, true)

	// Kreiranje indexa i upis index fajla
	index := NewIndex(sstable.IndexFilename, keys, offsets)
	index.Sort()
	summaryKeys, summaryOffsets, _ := index.WriteBM(true, 0, blockManager)

	// Kreiranje summary fajla i upis summary fajla
	summary := NewSummary(summaryKeys, summaryOffsets, sstable.SummaryFilename, sampleRate)
	summary.writeWithBM(true, 0, blockManager)

	UpdateMasterTOC(fmt.Sprintf("sstable-%04d", tableId))

	return sstable
}

func NewSstableOneFile(data []*Memtable.MemtableRecord, sampleRate int, compaction bool, blockManager *BlockManager.BlockManager, dictionary *Dictionary) *Sstable {

	tableId, err := GetNextTableID()
	if err != nil {
		log.Fatal(err)
	}

	tableDir := fmt.Sprintf("data/sstable/sstable-%04d", tableId)
	err = os.MkdirAll(tableDir, os.ModePerm)
	if err != nil {
		log.Fatalf("Failed to create directory for SSTable: %v", err)
	}

	filename := fmt.Sprintf("data/sstable/sstable-%04d/sstable-%04d", tableId, tableId)

	sstable := &Sstable{
		Filename:        filename + ".bin",
		DataFilename:    filename + ".bin",
		IndexFilename:   filename + ".bin",
		SummaryFilename: filename + ".bin",
		FilterFilename:  filename + ".bin",
	}

	// Kreiranje fajla
	createFiles(tableId, true)

	toc := TOC{
		SingleFile: true,
		Files:      []string{sstable.Filename},
	}

	err = WriteTOCFile(filename+"-TOC.txt", toc.SingleFile, toc.Files)
	if err != nil {
		log.Fatal(err)
	}

	var metadata Metadata
	var currentOffset uint64

	filter := NewBloomFilter(len(data), 0.01)
	for _, record := range data {
		filter.Add(record.Key)
	}

	metadata.FilterOffset = currentOffset
	// Serijalizacija filtera
	bytes, err := filter.Serialize()
	blockManager.WriteBloomFilter(bytes, sstable.FilterFilename)
	metadata.FilterLength = uint64(len(bytes))
	currentOffset += uint64(blockManager.GetBlockSize())

	if err != nil {
		log.Fatalf("Failed to write Bloom filter: %v", err)
	}

	// Pravljenje fragmentisanih zapisa i upis preko block manager-a
	blockSize := blockManager.GetBlockSize() // za sada
	sstableRecords := make(map[string][][]byte)

	for _, record := range data {
		fragments := BlockManager.CreateBinaryRecordsWithFragmentation(record.Tombstone, record.Key, record.Value, blockSize, true)
		sstableRecords[record.Key] = fragments
	}

	metadata.DataOffset = currentOffset
	keys, offsets, dataLength := blockManager.PutSstableRecords(sstableRecords, tableId, sstable.Filename, false)
	metadata.DataLength = dataLength
	currentOffset += dataLength

	fmt.Println("Keys: ", keys, "Offsets: ", offsets, "DataLength: ", dataLength)

	metadata.IndexOffset = currentOffset
	index := NewIndex(sstable.Filename, keys, offsets)
	index.Sort()
	summaryKeys, summaryOffsets, indexLength := index.WriteBM(false, currentOffset, blockManager)
	fmt.Println("Current offset --> ", currentOffset, "Index Offsets: ", summaryOffsets, "Index Length: ", indexLength)
	metadata.IndexLength = indexLength
	currentOffset += indexLength

	metadata.SummaryOffset = currentOffset
	summary := NewSummary(summaryKeys, summaryOffsets, sstable.Filename, sampleRate)
	summaryLength := summary.writeWithBM(false, currentOffset, blockManager)
	fmt.Println("Summary Offsets: ", summary.IndexOffsets, "Summary Length: ", summaryLength)
	metadata.SummaryLength = summaryLength
	currentOffset += summaryLength

	err = WriteMetadata(sstable.Filename, metadata)

	if err != nil {
		log.Fatal(err)
	}

	UpdateMasterTOC(fmt.Sprintf("sstable-%04d", tableId))

	return sstable
}

func WriteMetadata(filename string, metadata Metadata) error {
	file, err := os.OpenFile(filename, os.O_APPEND|os.O_WRONLY, 0644)
	if err != nil {
		return err
	}
	defer file.Close()

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

	return metadata, nil
}

func WriteSstable(data []*Memtable.MemtableRecord, sampleRate int, compaction bool, oneFile bool, blockManager *BlockManager.BlockManager, dictionary *Dictionary) *Sstable {
	if oneFile {
		return NewSstableOneFile(data, sampleRate, compaction, blockManager, dictionary)
	}
	return NewSstable(data, sampleRate, compaction, blockManager, dictionary)
}

func createFiles(tableID int, oneFile bool) (string, error) {
	// Kreiraj folder za novi SSTable
	tableDir := fmt.Sprintf("data/sstable/sstable-%04d", tableID)

	err := os.MkdirAll(tableDir, os.ModePerm) // Kreiraj folder ako ne postoji
	if err != nil {
		return "", fmt.Errorf("failed to create folder for SSTable: %v", err)
	}

	tableDir = "data/sstable/"

	// Generiši fajlove unutar foldera
	if !oneFile {
		fileNames := GenerateSstableFilenames("", tableID)

		fileNamesList := []string{fileNames["Data"], fileNames["Filter"], fileNames["Index"], fileNames["Summary"], fileNames["TOC"]}

		for _, name := range fileNamesList {
			filePath := fmt.Sprintf("%s%s", tableDir, name)
			file, err := os.Create(filePath)
			if err != nil {
				return "", fmt.Errorf("failed to create file %s: %v", filePath, err)
			}
			file.Close()
		}
	} else {
		// Kreiraj jedan fajl ako je single file mod
		filePath := fmt.Sprintf("%s/sstable-%04d/sstable-%04d.bin", tableDir, tableID, tableID)
		file, err := os.Create(filePath)
		if err != nil {
			return "", fmt.Errorf("failed to create single file: %v", err)
		}
		file.Close()
	}

	return tableDir, nil
}

func FindInSstable(key string, filename string, blockManager BlockManager.BlockManager) (*BlockManager.WalRecord, bool) {
	var toc TOC
	var err error
	toc, err = ReadTOCFile(filename)
	if err != nil {
		log.Fatal(err)
	}
	oneFile := toc.SingleFile
	files := toc.Files
	fmt.Println("Buffer pool: ", blockManager.GetBufferPool().Buffer)
	if oneFile {
		filename = files[0]
		filename = strings.TrimSpace(filename)
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
		filterBlock := blockManager.GetWalBlock(filename, int(metadata.FilterOffset), blockManager.GetBlockSize(), false)
		filter, err := Deserialize(filterBlock[:metadata.FilterLength])
		if err != nil {
			log.Fatal(err)
		}

		if !filter.Check(key) {
			return &BlockManager.WalRecord{}, false
		}

		// Proveri da li je ključ u summary-ju
		summary := &Summary{}
		summary = ReadSummaryFromFile(&blockManager, filename, metadata.SummaryOffset, metadata.SummaryLength)
		found, offset := summary.Find(key)

		if !found {
			return &BlockManager.WalRecord{}, false
		}

		// Proveri da li je ključ u indexu
		var index *Index
		keyOffsetPairs := ReadIndexFromFile(&blockManager, filename, offset, metadata.IndexLength)
		found, offset = index.SearchIndex(key, keyOffsetPairs)
		if !found {
			return &BlockManager.WalRecord{}, false
		}

		// Posto je single file mod, filter je pre data dela napisan:
		offset += uint64(blockManager.GetBlockSize())

		// Pročitaj podatke
		maxDataOffset := metadata.DataOffset + metadata.DataLength
		record, found, blockKeys, block := FindDataRecordBM(key, offset, filename, blockManager, maxDataOffset)
		if !found {
			return &BlockManager.WalRecord{}, false
		}

		// Ako je pronadjen record, blok(ovi) se upisuju u buffer pool
		for i, block := range block {
			blockManager.PutBlock(blockKeys[i], block)
		}
		fmt.Println(blockManager.GetBufferPool().Buffer)

		return record, true

	} else {
		datafile := files[0]
		indexfile := files[1]
		summaryfile := files[2]
		filterfile := files[3]

		// Proveri da li je ključ u filteru
		filterBlock := blockManager.GetWalBlock(filterfile, 0, blockManager.GetBlockSize(), false)
		filter, err := Deserialize(filterBlock)
		if err != nil {
			log.Fatal(err)
		}

		if !filter.Check(key) {
			return &BlockManager.WalRecord{}, false
		}

		// Proveri da li je ključ u summary-ju

		info, err := os.Stat(summaryfile)
		if err != nil {
			log.Fatalf("Greska pri dohvatanju informacija o fajlu %s: %v", summaryfile, err)
		}
		length := uint64(info.Size())

		summary := &Summary{}
		summary = ReadSummaryFromFile(&blockManager, summaryfile, 0, length)
		fmt.Println("Summary: ", summary)
		found, offsetS := summary.Find(key)

		if !found {
			return &BlockManager.WalRecord{}, false
		}

		// Proveri da li je ključ u indexu

		info, err = os.Stat(indexfile)
		if err != nil {
			log.Fatalf("Greska pri dohvatanju informacija o fajlu %s: %v", indexfile, err)
		}
		length = uint64(info.Size())

		var index *Index
		keyOffsetPairs := ReadIndexFromFile(&blockManager, indexfile, offsetS, length)
		found, offset := index.SearchIndex(key, keyOffsetPairs)
		if !found {
			return &BlockManager.WalRecord{}, false
		}

		info, err = os.Stat(datafile)
		if err != nil {
			log.Fatalf("Greska pri dohvatanju informacija o fajlu %s: %v", datafile, err)
		}
		maxDataOffset := uint64(info.Size())

		// Pročitaj podatke
		record, found, blockKeys, blocks := FindDataRecordBM(key, offset, datafile, blockManager, maxDataOffset)
		if !found {
			return &BlockManager.WalRecord{}, false
		}

		// Ako je pronadjen record, blok(ovi) se upisuju u buffer pool
		for i, block := range blocks {
			blockManager.PutBlock(blockKeys[i], block)
		}

		return record, true
	}
}

func FindInSstables(key string, blockManager BlockManager.BlockManager) (BlockManager.WalRecord, bool) {
	masterToc, err := os.OpenFile("data/sstable/master-toc.txt", os.O_RDONLY, 0644)
	if err != nil {
		log.Fatal(err)
	}
	defer masterToc.Close()

	var directories []string
	scanner := bufio.NewScanner(masterToc)
	for scanner.Scan() {
		directories = append(directories, strings.TrimSpace(scanner.Text()))
	}

	// Čitamo od najnovije ka starijim
	for i := len(directories) - 1; i >= 0; i-- {
		dir := directories[i]
		tocfilename := "data/sstable/" + dir + "/" + dir + "-TOC.txt"

		dataRecord, found := FindInSstable(key, tocfilename, blockManager)
		if found {
			if !dataRecord.Tombstone {
				// Ako je pronađen i nije tombstone, odmah vraćamo
				return *dataRecord, true
			} else {
				// Ako je tombstone, znamo da je obrisan — prekidamo
				return BlockManager.WalRecord{}, false
			}
		}
	}

	// Nije pronađen ni u jednom SSTable
	return BlockManager.WalRecord{}, false
}

// Napravio sam TOC file kako bi lakse mogao da se pristupi svim fajlovima
func WriteTOCFile(filename string, singleFile bool, files []string) error {
	file, err := os.OpenFile(filename, os.O_CREATE|os.O_WRONLY, 0644)
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