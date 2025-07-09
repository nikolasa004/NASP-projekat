package sstable

import (
	"bufio"
	"encoding/binary"
	"encoding/json"
	"fmt"
	"io"
	"log"
	"os"
	"project/BlockManager"
	"project/Memtable"
	"sort"
	"strings"
)

type TOC struct {
	SingleFile     bool
	HasCompression bool
	Files          []string
}

type Sstable struct {
	Filename           string
	DataFilename       string
	IndexFilename      string
	SummaryFilename    string
	FilterFilename     string
	MerkleTreeFilename string
}

type Metadata struct {
	IsSingleFile     uint64
	HasCompression   uint64
	DataOffset       uint64
	DataLength       uint64
	IndexOffset      uint64
	IndexLength      uint64
	SummaryOffset    uint64
	SummaryLength    uint64
	FilterOffset     uint64
	FilterLength     uint64
	MerkleTreeOffset uint64
	MerkleTreeLength uint64
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

func GenerateSstableFilenames(baseDir string, tableID int, level int) map[string]string {
	folder := fmt.Sprintf("%s/L%d/sstable-%04d", baseDir, level, tableID)
	filenames := map[string]string{
		"Data":    fmt.Sprintf("%s/sstable-%04d-Data.bin", folder, tableID),
		"Filter":  fmt.Sprintf("%s/sstable-%04d-Filter.bin", folder, tableID),
		"Index":   fmt.Sprintf("%s/sstable-%04d-Index.bin", folder, tableID),
		"Summary": fmt.Sprintf("%s/sstable-%04d-Summary.bin", folder, tableID),
		"TOC":     fmt.Sprintf("%s/sstable-%04d-TOC.txt", folder, tableID),
		"Merkle":  fmt.Sprintf("%s/sstable-%04d-Merkle.bin", folder, tableID),
	}

	return filenames
}

func NewSstable(data []*Memtable.MemtableRecord, sampleRate int, compaction bool, blockManager *BlockManager.BlockManager, compression bool, level int) *Sstable {

	tableId, err := GetNextTableID()
	if err != nil {
		log.Fatal(err)
	}

	tableDir := fmt.Sprintf("data/sstable/L%d/sstable-%04d", level, tableId)
	err = os.MkdirAll(tableDir, os.ModePerm)
	if err != nil {
		log.Fatalf("Failed to create directory for SSTable: %v", err)
	}

	filenames := GenerateSstableFilenames("data/sstable", tableId, level)

	sstable := &Sstable{
		Filename:           filenames["TOC"],
		DataFilename:       filenames["Data"],
		IndexFilename:      filenames["Index"],
		SummaryFilename:    filenames["Summary"],
		FilterFilename:     filenames["Filter"],
		MerkleTreeFilename: filenames["Merkle"],
	}

	// Kreiranje fajlova
	createFiles(tableId, false, level)

	// TOC fajl
	toc := TOC{
		Files: []string{sstable.DataFilename, sstable.IndexFilename, sstable.SummaryFilename, sstable.FilterFilename, sstable.MerkleTreeFilename},
	}

	err = WriteTOCFile(filenames["TOC"], false, toc.Files, compression)

	if err != nil {
		log.Fatal(err)
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

	var allData [][]byte
	for _, record := range data {
		combined := []byte(record.Key + string(record.Value))
		allData = append(allData, combined)
	}
	root := BuildTree(allData) // Pretpostavljam da imaš ovu funkciju iz merkle_tree.go
	err = SaveMerkleTree(root, sstable.MerkleTreeFilename)
	if err != nil {
		log.Fatalf("Neuspešno čuvanje Merkle stabla: %v", err)
	}
	log.Printf("Merkle stablo uspešno sačuvano u %s\n", sstable.MerkleTreeFilename)

	UpdateMasterTOC(fmt.Sprintf("L%d/sstable-%04d", level, tableId))

	return sstable
}

func NewSstableOneFile(data []*Memtable.MemtableRecord, sampleRate int, compaction bool, blockManager *BlockManager.BlockManager, compression bool, level int) *Sstable {

	tableId, err := GetNextTableID()
	if err != nil {
		log.Fatal(err)
	}

	tableDir := fmt.Sprintf("data/sstable/L%d/sstable-%04d", level, tableId)
	err = os.MkdirAll(tableDir, os.ModePerm)
	if err != nil {
		log.Fatalf("Failed to create directory for SSTable: %v", err)
	}

	filename := fmt.Sprintf("data/sstable/L%d/sstable-%04d/sstable-%04d", level, tableId, tableId)

	sstable := &Sstable{
		Filename:           filename + ".bin",
		DataFilename:       filename + ".bin",
		IndexFilename:      filename + ".bin",
		SummaryFilename:    filename + ".bin",
		FilterFilename:     filename + ".bin",
		MerkleTreeFilename: filename + ".bin",
	}

	// Kreiranje fajla
	createFiles(tableId, true, level)

	toc := TOC{
		SingleFile:     true,
		Files:          []string{sstable.Filename},
		HasCompression: compression,
	}

	err = WriteTOCFile(filename+"-TOC.txt", toc.SingleFile, toc.Files, toc.HasCompression)
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

	var allData [][]byte
	for _, record := range data {
		combined := []byte(record.Key + string(record.Value))
		allData = append(allData, combined)
	}

	root := BuildTree(allData) // iz merkle_tree.go
	merkleJSON, err := json.MarshalIndent(root, "", "  ")
	if err != nil {
		log.Fatalf("Neuspešno serijalizovanje Merkle stabla: %v", err)
	}
	log.Printf("Merkle stablo uspešno serijalizovano, dužina: %d bajtova\n", len(merkleJSON))

	metadata.MerkleTreeOffset = currentOffset           // novo
	metadata.MerkleTreeLength = uint64(len(merkleJSON)) // novo

	// Upis Merkle stabla direktno u .bin fajl – novo
	file, err := os.OpenFile(sstable.Filename, os.O_WRONLY|os.O_APPEND, 0644)
	if err != nil {
		log.Fatalf("Neuspešno otvaranje fajla za Merkle: %v", err)
	}
	defer file.Close()

	_, err = file.Write(merkleJSON)
	if err != nil {
		log.Fatalf("Neuspešno pisanje Merkle stabla u fajl: %v", err)
	}

	err = WriteMetadata(sstable.Filename, metadata)

	if err != nil {
		log.Fatal(err)
	}

	UpdateMasterTOC(fmt.Sprintf("L%d/sstable-%04d", level, tableId))

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

	err = binary.Write(file, binary.LittleEndian, metadata.HasCompression)
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
	err = binary.Write(file, binary.LittleEndian, metadata.MerkleTreeOffset)
	if err != nil {
		return err
	}
	err = binary.Write(file, binary.LittleEndian, metadata.MerkleTreeLength)
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
	_, err = file.Seek(fileSize-96, io.SeekStart)
	if err != nil {
		return metadata, err
	}

	// Čitaj polja redom kako su zapisana
	err = binary.Read(file, binary.LittleEndian, &metadata.HasCompression)
	if err != nil {
		return metadata, err
	}

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
	err = binary.Read(file, binary.LittleEndian, &metadata.MerkleTreeOffset)
	if err != nil {
		return metadata, err
	}
	err = binary.Read(file, binary.LittleEndian, &metadata.MerkleTreeLength)
	if err != nil {
		return metadata, err
	}

	return metadata, nil
}

func WriteSstable(data []*Memtable.MemtableRecord, sampleRate int, compaction bool, oneFile bool, blockManager *BlockManager.BlockManager, compression bool, level int) *Sstable {
	if oneFile {
		return NewSstableOneFile(data, sampleRate, compaction, blockManager, compression, level)
	}
	return NewSstable(data, sampleRate, compaction, blockManager, compression, level)
}

func createFiles(tableID int, oneFile bool, level int) (string, error) {
	// Kreiraj folder za novi SSTable
	tableDir := fmt.Sprintf("data/sstable/L%d/sstable-%04d", level, tableID)

	err := os.MkdirAll(tableDir, os.ModePerm) // Kreiraj folder ako ne postoji
	if err != nil {
		return "", fmt.Errorf("failed to create folder for SSTable: %v", err)
	}

	fileNames := GenerateSstableFilenames("data/sstable", tableID, level)

	// Generiši fajlove unutar foldera
	if !oneFile {

		fileNamesList := []string{fileNames["Data"], fileNames["Filter"], fileNames["Index"], fileNames["Summary"], fileNames["Merkle"], fileNames["TOC"]}

		for _, name := range fileNamesList {
			file, err := os.Create(name)
			if err != nil {
				return "", fmt.Errorf("failed to create file %s: %v", name, err)
			}
			file.Close()
		}

	} else {
		// Kreiraj jedan fajl ako je single file mod
		filePath := fmt.Sprintf("data/sstable/L%d/sstable-%04d/sstable-%04d.bin", level, tableID, tableID)
		file, err := os.Create(filePath)
		if err != nil {
			return "", fmt.Errorf("failed to create single file: %v", err)
		}
		file.Close()
	}

	return tableDir, nil
}

func FindInSstable(key string, filename string, blockManager BlockManager.BlockManager, dictionary *Dictionary) (*BlockManager.WalRecord, bool) {
	var toc TOC
	var err error
	toc, err = ReadTOCFile(filename)
	if err != nil {
		log.Fatal(err)
	}
	oneFile := toc.SingleFile
	files := toc.Files
	isCompressed := toc.HasCompression
	if isCompressed {
		compressedKey, found := dictionary.Get(key)
		if !found {
			log.Fatalf("Failed to get key from dictionary: %v", err)
		}
		key = compressedKey
	}
	// fmt.Println("Buffer pool: ", blockManager.GetBufferPool().Buffer)
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
		// fmt.Println(blockManager.GetBufferPool().Buffer)

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
		// fmt.Println("Summary: ", summary)
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

func FindInSstables(key string, blockManager BlockManager.BlockManager, dictionary *Dictionary) (BlockManager.WalRecord, bool) {
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
		sstableName := strings.Split(dir, "/")[1]
		tocfilename := "data/sstable/" + dir + "/" + sstableName + "-TOC.txt"

		dataRecord, found := FindInSstable(key, tocfilename, blockManager, dictionary)
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
func WriteTOCFile(filename string, singleFile bool, files []string, hasCompression bool) error {
	file, err := os.OpenFile(filename, os.O_CREATE|os.O_WRONLY, 0644)
	if err != nil {
		return err
	}
	defer file.Close()

	writer := bufio.NewWriter(file)

	compression := "0"
	if hasCompression {
		compression = "1"
	}
	if _, err := writer.WriteString(compression + "\n"); err != nil {
		return err
	}

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
	compression, err := reader.ReadString('\n')
	if err != nil {
		return toc, err
	}

	compression = strings.TrimSpace(compression)
	toc.HasCompression = compression[0] == '1'

	mode, err := reader.ReadString('\n')
	if err != nil {
		return toc, err
	}

	toc.SingleFile = mode[0] == '1' //da li je bane ovo dobro dodao,koja se vrednost upisuje u prvi red

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
func ReadSSTable(filename string, bm *BlockManager.BlockManager) ([][]byte, error) {
	metadata, err := ReadMetadata(filename)
	if err != nil {
		return nil, err
	}

	records, _, _, err := bm.ReadSstableRecordsFromBlocks(filename, metadata.DataOffset, bm.GetBlockSize(), metadata.DataOffset+metadata.DataLength)
	if err != nil {
		return nil, err
	}

	sort.Slice(records, func(i, j int) bool {
		return records[i].Key < records[j].Key
	})

	var allData [][]byte
	for _, record := range records {
		if !record.Tombstone {
			combined := append([]byte(record.Key), record.Value...)
			allData = append(allData, combined)
		}
	}
	return allData, nil
}
func ReadSSTableMultiMode(tocFile string, bm *BlockManager.BlockManager) ([][]byte, error) {
	fmt.Println("Čitam SSTable multi file:", tocFile)
	toc, err := ReadTOCFile(tocFile)
	if err != nil {
		return nil, err
	}

	dataFile := toc.Files[0]
	info, err := os.Stat(dataFile)
	if err != nil {
		return nil, err
	}
	fmt.Println("Veličina data fajla:", info.Size())

	records, _, _, err := bm.ReadSstableRecordsFromBlocks(dataFile, 0, bm.GetBlockSize(), uint64(info.Size()))
	fmt.Printf("ReadSstableRecordsFromBlocks: file=%s, offset=0, blocksize=%d, end=%d\n", dataFile, bm.GetBlockSize(), uint64(info.Size()))

	if err != nil {
		return nil, err
	}

	sort.Slice(records, func(i, j int) bool {
		return records[i].Key < records[j].Key
	})

	var allData [][]byte
	for _, record := range records {
		if !record.Tombstone {
			combined := append([]byte(record.Key), record.Value...)
			allData = append(allData, combined)
		}
	}
	return allData, nil
}

func FindTOCPath(sstableName string) (string, error) {
	masterToc, err := os.Open("data/sstable/master-toc.txt")
	if err != nil {
		return "", err
	}
	defer masterToc.Close()

	scanner := bufio.NewScanner(masterToc)
	for scanner.Scan() {
		dir := strings.TrimSpace(scanner.Text())
		if strings.Contains(dir, sstableName) { // Možeš i preciznije == ako želiš baš tačan match
			return fmt.Sprintf("data/sstable/%s/%s-TOC.txt", dir, sstableName), nil
		}
	}
	return "", fmt.Errorf("nema SSTable sa tim imenom: %s", sstableName)
}

func ReadAllRecords(sst *Sstable, blockManager *BlockManager.BlockManager) ([]*Memtable.MemtableRecord, error) {
	// 1. Otvori TOC
	toc, err := ReadTOCFile(sst.Filename)
	if err != nil {
		return nil, err
	}

	var records []*BlockManager.WalRecord
	if toc.SingleFile {
		// Single file mode
		metadata, err := ReadMetadata(toc.Files[0])
		if err != nil {
			return nil, err
		}
		records, _, _, err = blockManager.ReadSstableRecordsFromBlocks(
			toc.Files[0],
			metadata.DataOffset,
			blockManager.GetBlockSize(),
			metadata.DataOffset+metadata.DataLength,
		)
		if err != nil {
			return nil, err
		}
	} else {
		// Multi file mode
		dataFile := toc.Files[0]
		info, err := os.Stat(dataFile)
		if err != nil {
			return nil, err
		}
		records, _, _, err = blockManager.ReadSstableRecordsFromBlocks(
			dataFile,
			0,
			blockManager.GetBlockSize(),
			uint64(info.Size()),
		)
		if err != nil {
			return nil, err
		}
	}
	// Konvertuj u []*Memtable.MemtableRecord
	var memRecs []*Memtable.MemtableRecord
	for _, rec := range records {
		memRecs = append(memRecs, &Memtable.MemtableRecord{
			Key:       rec.Key,
			Value:     rec.Value,
			Tombstone: rec.Tombstone,
			Timestamp: rec.Timestamp,
		})
	}
	return memRecs, nil
}

// CompactSstables pravi merge više SSTabela i vraća jedan slice rezultata (bez duplikata, najnoviji zapis pobeđuje)
func CompactSstables(allSstables [][]*Memtable.MemtableRecord) []*Memtable.MemtableRecord {
	N := len(allSstables)
	positions := make([]int, N)
	result := []*Memtable.MemtableRecord{}

	for {
		// Nađi najmanji ključ među aktivnim pokazivačima
		minKey := ""
		first := true
		for i := range positions {
			if positions[i] >= len(allSstables[i]) {
				continue
			}
			key := allSstables[i][positions[i]].Key
			if first || key < minKey {
				minKey = key
				first = false
			}
		}
		if first {
			break // Gotovi svi pokazivači
		}

		// Pronađi sve zapise sa tim ključem i upamti njihove timestampe
		var newest *Memtable.MemtableRecord
		var newestIdx int
		var timestamps []uint64
		for i := range positions {
			if positions[i] >= len(allSstables[i]) {
				continue
			}
			rec := allSstables[i][positions[i]]
			if rec.Key == minKey {
				timestamps = append(timestamps, rec.Timestamp)
				if newest == nil || rec.Timestamp > newest.Timestamp {
					newest = rec
					newestIdx = i
				}
			}
		}

		// Prikazi sve timestampove i koji se upisuje
		if len(timestamps) > 1 {
			fmt.Printf("Ključ '%s' ima timestampe: %v, upisuje se iz SSTable #%d sa timestampom %d\n", minKey, timestamps, newestIdx, newest.Timestamp)
		} else if len(timestamps) == 1 {

		}

		// Upisi i tombstone i netombstone zapise da bi se očuvala semantika brisanja
		if newest != nil {
			result = append(result, newest)
		}

		// Pomeraj pokazivače napred za sve koje su bile na tom ključu
		for i := range positions {
			if positions[i] < len(allSstables[i]) && allSstables[i][positions[i]].Key == minKey {
				positions[i]++
			}
		}
	}

	return result
}

func CountSstablesInLevel(level int) (int, []string, error) {
	path := fmt.Sprintf("data/sstable/L%d", level)
	entries, err := os.ReadDir(path)
	if err != nil {
		return 0, nil, err
	}
	count := 0
	var sstDirs []string
	for _, entry := range entries {
		if entry.IsDir() && strings.HasPrefix(entry.Name(), "sstable-") {
			count++
			sstDirs = append(sstDirs, entry.Name())
		}
	}
	return count, sstDirs, nil
}

// treba mi funkcija koja ce da ukloni sstable-xxxx iz master-toc.txt
func RemoveSstablesFromMasterTOC(sstableNames []string) error {
	masterTocPath := "data/sstable/master-toc.txt"
	tmpPath := masterTocPath + ".tmp"

	// Napravi mapu radi bržeg pretraživanja
	toDelete := make(map[string]struct{})
	for _, name := range sstableNames {
		toDelete[name] = struct{}{}
	}

	in, err := os.Open(masterTocPath)
	if err != nil {
		return fmt.Errorf("failed to open master TOC file: %v", err)
	}
	defer in.Close()

	out, err := os.Create(tmpPath)
	if err != nil {
		return fmt.Errorf("failed to create temp TOC file: %v", err)
	}
	defer out.Close()

	scanner := bufio.NewScanner(in)
	for scanner.Scan() {
		line := strings.TrimSpace(scanner.Text())
		skip := false
		for sstableName := range toDelete {
			if strings.Contains(line, sstableName) {
				skip = true
				break
			}
		}
		if !skip && line != "" {
			out.WriteString(line + "\n")
		}
	}
	if err := scanner.Err(); err != nil {
		return fmt.Errorf("error reading master TOC file: %v", err)
	}

	out.Sync()
	out.Close()
	in.Close()

	if err := os.Rename(tmpPath, masterTocPath); err != nil {
		return fmt.Errorf("failed to replace TOC file: %v", err)
	}

	return nil
}
