package BlockManager

import (
	"encoding/binary"
	"fmt"
	"log"
	"os"
	"sort"
	"strconv"
	"strings"
)

//importovanje potrebnih paketa

type BlockManager struct {
	blockSize               int
	blocksPerSegment        int //blokova po segmentu u wal-u
	bufferPool              *BufferPool
	currentBlock            int              // trenutni segment i blok u wal-u
	walPath                 string           //putanja do wal foldera
	segments                []string         //lista segmenata
	orderOfSegments         map[int]int      //redosled segmenata, kljuc je indeks u sortiranoj listi, vrednost je pravi broj segmenta jer ne mora da prvi bude 0 posle brisanja
	differentConfigSegments map[string][]int //segmenti koji imaju drugaciju konfiguraciju od trenutne
}

func (blockManeger *BlockManager) GetBlockSize() int { //geter za velicinu bloka
	return blockManeger.blockSize
}

func (blockManager *BlockManager) GetSegments() []string {
	return blockManager.segments
}

func (blockManager *BlockManager) GetCurrentBlock() int {
	return blockManager.currentBlock
}

func NewBlockManager(blockSize, blocksPerSegment, poolSize int) *BlockManager { //konstruktor za BlockManager
	bufferPool := NewBufferPool(poolSize)
	walPath := "BlockManager/wal"

	files, err := os.ReadDir(walPath)
	if err != nil {
		log.Print("WAL folder ne postoji, pokrecemo kreiranje novog")

		err = os.MkdirAll(walPath, 0755)
		if err != nil {
			log.Print("Greska pri kreiranju WAL foldera, nije moguce nastaviti sa radom")
			log.Fatal("Greska: ", err)
		}
	}

	segments := make([]string, 0)

	var currentBlock int
	orderOfSegments := make(map[int]int)
	differentConfigSegments := make(map[string][]int)
	if len(files) == 0 { //pravimo pocetni segment
		segment := walPath + "/wal_0.log"
		segments = append(segments, segment)

		file, err := os.Create(segment)
		if err != nil {
			log.Print("Greska pri kreiranju pocetnog segmenta, nije moguce nastaviti sa radom")
			log.Fatal("Greska: ", err)
		}

		//dodajemo heder koji ce sadrzati informacije o velicini bloka, broju blokova po segmentu i koji je broj segmenta zbog mogucih promena
		//dodajemo i koji je prvi koristan blok u segmentu
		header := make([]byte, 16)
		binary.LittleEndian.PutUint32(header[:4], uint32(blockSize))
		binary.LittleEndian.PutUint32(header[4:8], uint32(blocksPerSegment))
		binary.LittleEndian.PutUint32(header[8:12], uint32(0))
		binary.LittleEndian.PutUint32(header[12:16], uint32(0)) //ovo se koristi za memtable rekonstrukciju i menja se posle flasovanja memtable-a

		_, err = file.Write(header)

		if err != nil {
			log.Print("Greska pri pisanju hedera u pocetni segment, nije moguce nastaviti sa radom")
			log.Fatal("Greska: ", err)
		}

		file.Close()

		currentBlock = 0

		orderOfSegments[0] = 0
	} else {
		for _, segment := range files {
			if segment.IsDir() {
				continue
			}

			headerBlock := make([]byte, blockSize)
			hederBytes := make([]byte, 16)
			file, err := os.OpenFile(walPath+"/"+segment.Name(), os.O_RDONLY, 0644)

			if err != nil {
				log.Print("Greska pri otvaranju segmenta: ", segment.Name())
				log.Print("Podaci unutar segmenta nece biti dostupni")
				file.Close()
				continue
			}

			_, err = file.Read(headerBlock)

			if err != nil {
				log.Print("Greska pri citanju hedera iz segmenta: ", segment.Name())
				log.Print("Podaci unutar segmenta nece biti dostupni")
				file.Close()
				continue
			}

			copy(hederBytes, headerBlock[:16])

			blockSizeOfSegment := int(binary.LittleEndian.Uint32(hederBytes[:4]))
			blocksPerSegmentOfSegment := int(binary.LittleEndian.Uint32(hederBytes[4:8]))
			segmentNumber := int(binary.LittleEndian.Uint32(hederBytes[8:12]))

			//provera da li je konfiguracija segmenta ista kao trenutna
			if blockSizeOfSegment != blockSize || blocksPerSegmentOfSegment != blocksPerSegment {
				differentConfigSegments[walPath+"/"+segment.Name()] = []int{blockSizeOfSegment, blocksPerSegmentOfSegment}
			}

			file.Close()

			for i := len(segments); i >= 0; i-- {
				if i == 0 || segmentNumber >= orderOfSegments[i-1] {
					segments = append(segments, "")
					copy(segments[i+1:], segments[i:])
					segments[i] = walPath + "/" + segment.Name()

					// pomeri redosled vrednosti
					for j := len(segments) - 1; j > i; j-- {
						orderOfSegments[j] = orderOfSegments[j-1]
					}
					orderOfSegments[i] = segmentNumber
					break
				}
			}
		}
	}

	return &BlockManager{
		blockSize:               blockSize,
		blocksPerSegment:        blocksPerSegment,
		bufferPool:              &bufferPool,
		currentBlock:            currentBlock,
		walPath:                 walPath,
		segments:                segments,
		orderOfSegments:         orderOfSegments,
		differentConfigSegments: differentConfigSegments,
	}
}

func (blockManager *BlockManager) Reconstruct() ([]WalRecord, map[string]int) { //funkcija koja podize sistem i rekonstruise stanje
	fullSegment := false
	endOfLastSegment := false
	fullRecords := []WalRecord{}
	fragmentedParts := []WalRecord{}
	currentFragmentedPosition := 1
	recordSegments := make(map[string]int) //mapira kljuc recorda na poslednji segment u kom se on nalazi

	i := 0 //brojac segmenata
	var lastSegment string
	var lastBlock []byte
	for _, segment := range blockManager.segments {
		fullSegment = false
		currentSegmentBlockSize := 0
		currentSegmentBlockNumber := 0

		if blockManager.differentConfigSegments[segment] != nil { //ako je konfiguracija segmenta drugacija od trenutne, citamo ga posebno
			currentSegmentBlockSize = blockManager.differentConfigSegments[segment][0]
			currentSegmentBlockNumber = blockManager.differentConfigSegments[segment][1]

			endOfLastSegment, lastBlock = blockManager.ProcessWalSegment(currentSegmentBlockNumber, currentSegmentBlockSize, i, segment, &fullRecords, &fragmentedParts, &currentFragmentedPosition, &recordSegments)
		} else {
			currentSegmentBlockSize = blockManager.blockSize
			currentSegmentBlockNumber = blockManager.blocksPerSegment

			endOfLastSegment, lastBlock = blockManager.ProcessWalSegment(currentSegmentBlockNumber, currentSegmentBlockSize, i, segment, &fullRecords, &fragmentedParts, &currentFragmentedPosition, &recordSegments)
		}
		if endOfLastSegment {
			lastSegment = segment
			break
		}

		fullSegment = true

		blockManager.currentBlock = 0
		i++
	}

	if fullSegment {
		blockManager.currentBlock = blockManager.blocksPerSegment
	}

	//ako je poslednji segment po staroj konfiguraciji moramo da ga popunimo sa padding blokovima i kreiramo novi segment
	if blockManager.differentConfigSegments[blockManager.segments[len(blockManager.segments)-1]] != nil {
		paddingSize := blockManager.blockSize * (blockManager.blocksPerSegment - blockManager.currentBlock)
		padding := make([]byte, paddingSize)
		blockManager.currentBlock = blockManager.blocksPerSegment
		blockManager.WriteBlock(padding, blockManager.differentConfigSegments[blockManager.segments[len(blockManager.segments)-1]][0], blockManager.currentBlock, blockManager.segments[len(blockManager.segments)-1]) //upisujemo padding blokove u poslednji segment
	} else {
		key := lastSegment + "-" + strconv.Itoa(blockManager.currentBlock)
		blockManager.PutBlock(key, lastBlock) //upisujemo poslednji blok u wal
	}

	//ako je poslednji zapis fragmentovan, treba ga dodati u fullRecords
	if len(fragmentedParts) > 0 {
		keyBytes := []byte{}
		valueBytes := []byte{}

		for _, part := range fragmentedParts {
			keyBytes = append(keyBytes, part.Key...)
			valueBytes = append(valueBytes, part.Value...)
		}

		fullRecord := fragmentedParts[0]
		fullRecord.Key = string(keyBytes)
		fullRecord.Value = valueBytes

		fullRecords = append(fullRecords, fullRecord)
	}
	return fullRecords, recordSegments
}

func (blockManager *BlockManager) ProcessWalSegment(blocksPerSegment, blockSize, i int, segment string, fullRecords, fragmentedParts *[]WalRecord, currentFragmentedPosition *int, recordSegments *map[string]int) (bool, []byte) {
	endOfLastSegment := false
	var lastBlock []byte
	var firstHeaderBlock []byte

	firstTimeHeader := true //da li je prvi put citamo header segmenta
	for blockManager.currentBlock <= blocksPerSegment {
		if blockManager.currentBlock == 0 && firstTimeHeader {
			//citamo prvi korisni blok iz headera segmenta
			firstTimeHeader = false
			firstHeaderBlock = blockManager.GetWalBlock(segment, blockManager.currentBlock, blockSize, false)
			firstBlock := int(binary.LittleEndian.Uint32(firstHeaderBlock[12:16]))
			blockManager.currentBlock = firstBlock
			continue
		}

		var block []byte
		if blockManager.currentBlock == 0 {
			block = blockManager.GetWalBlock(segment, blockManager.currentBlock, blockSize, true)
		} else {
			block = blockManager.GetWalBlock(segment, blockManager.currentBlock, blockSize, false)
		}
		lastBlock = block
		if block != nil {
			var isHeaderBlock bool
			if blockManager.currentBlock == 0 {
				isHeaderBlock = true
			} else {
				isHeaderBlock = false
			}
			records := ReadAllRecords(block, int64(blockSize), isHeaderBlock)

			for _, record := range records {
				(*recordSegments)[record.Key] = i

				if record.Type == 0 {
					if len(*fragmentedParts) > 0 { //ako imamo fragmentovane delove, treba ih spojiti u jedan zapis
						*currentFragmentedPosition = 1

						keyBytes := []byte{}
						valueBytes := []byte{}

						for _, part := range *fragmentedParts {
							keyBytes = append(keyBytes, part.Key...)
							valueBytes = append(valueBytes, part.Value...)
						}

						fullRecord := (*fragmentedParts)[0]
						fullRecord.Key = string(keyBytes)
						fullRecord.Value = valueBytes

						*fullRecords = append(*fullRecords, fullRecord)
						*fragmentedParts = []WalRecord{}
					}

					*fullRecords = append(*fullRecords, record)
				} else {

					if *currentFragmentedPosition == int(record.Type) {
						*fragmentedParts = append(*fragmentedParts, record)
						*currentFragmentedPosition++
					} else if *currentFragmentedPosition > int(record.Type) { //ako je fragmentacija resetovana, treba da spojimo sve delove u jedan zapis
						*currentFragmentedPosition = 1

						keyBytes := []byte{}
						valueBytes := []byte{}

						for _, part := range *fragmentedParts {
							keyBytes = append(keyBytes, part.Key...)
							valueBytes = append(valueBytes, part.Value...)
						}

						fullRecord := (*fragmentedParts)[0]
						fullRecord.Key = string(keyBytes)
						fullRecord.Value = valueBytes

						*fullRecords = append(*fullRecords, fullRecord)
						*fragmentedParts = []WalRecord{}

						*fragmentedParts = append(*fragmentedParts, record)
						*currentFragmentedPosition++
					}
				}
			}
		} else {
			endOfLastSegment = true
			break
		}
		blockManager.currentBlock++
	}

	if blockManager.currentBlock == 0 { //ako je u segmentu samo heder bice prepoznat kao nil pa mora rucno da se doda poslednji blok
		info, err := os.Stat(segment)
		if err == nil && info.Size() == 16 {
			lastBlock = firstHeaderBlock[:16]
		}
	}

	return endOfLastSegment, lastBlock
}

func (blockManager *BlockManager) GetWalBlock(filename string, blockPosition, blockSize int, headerBlock bool) []byte { //funkcija koja vraca zadati blok iz WAL-a
	file, err := os.OpenFile(filename, os.O_RDONLY, 0644)

	if err != nil {
		log.Println("Greska pri otvaranju segmenta: ", filename)
		log.Println("Neki podaci ce biti izgubljeni")
		return nil
	}

	defer file.Close()

	seekValue := int64(blockPosition * blockSize)
	info, err := file.Stat()
	if err != nil {
		log.Println("Greska pri dobijanju informacija o segmentu: ", filename)
		log.Println("Neki podaci ce biti izgubljeni")
		return nil
	}

	if seekValue >= info.Size() { //proveravamo da li je trazeni blok van granica fajla
		return nil
	}
	if info.Size() == 16 && headerBlock { //ako je u bloku samo header, vracamo nil jer nema podataka
		return nil
	}

	file.Seek(int64(blockPosition*blockSize), 0)

	block := make([]byte, blockSize)
	_, err = file.Read(block)

	if err != nil {
		log.Println("Greska pri citanju bloka iz segmenta: ", filename)
		log.Println("Neki podaci ce biti izgubljeni")
		return nil
	}

	if headerBlock { //ako je header blok, vracamo samo posle 16-og bajta
		block = block[16:]
	}

	return block
}

func (blockManager *BlockManager) GetBlock(key string) []byte { //funkcija koja vraca zadati blok
	value := blockManager.bufferPool.Get(key)
	if value != nil {
		return value
	}

	//get ne ide iz wala jer se on cita samo pri podizanju sistema
	//kada se bude radio sstable, ovde ce se traziti kljuc u sstable-ima
	return nil
}

func (blockManager *BlockManager) PutBlock(key string, value []byte) { //funkcija koja dodaje blok prvo u bafer pool, a ako se vrati blok koji je izmenjen, upisuje ga u WAL

	returnedValue, keyFromFlush := blockManager.bufferPool.Put(key, value, true)

	if returnedValue != nil { //ako je vracen blok znaci da je on izmenjen u bafer pool-u i treba ga upisati na disk
		parts := strings.Split(keyFromFlush, "-")
		blockNumber, err := strconv.Atoi(parts[1])
		if err != nil {
			log.Println("Greska pri konverziji broja bloka:", err)
			return
		}
		blockManager.WriteBlock(returnedValue, blockManager.blockSize, blockNumber, parts[0]) //upisujemo blok u wal
	}
}

func (blockManager *BlockManager) WriteBlock(value []byte, seekValue, blockNumber int, filePath string) { //funkcija koja upisuje blok u WAL
	file, err := os.OpenFile(filePath, os.O_WRONLY, 0644)

	if err != nil {
		if os.IsNotExist(err) {
			log.Println("Segment ", filePath, " ne postoji, ako ste ga rucno obrisali doce do gubljenja podataka")
			log.Println("Ako niste, ignorisite ovu poruku") //najverovatnije je flasovan memtable i segment je obrisan a ostali su podaci u baferu
		} else {
			log.Println("Greska pri otvaranju segmenta: ", filePath)
			log.Println("Neki podaci ce biti izgubljeni")
		}
		return
	}

	defer file.Close()

	file.Seek(int64(blockNumber*seekValue), 0)
	_, err = file.Write(value)

	if err != nil {
		log.Println("Greska pri pisanju bloka u segment: ", filePath)
		log.Println("Neki podaci ce biti izgubljeni")
	}
}

func (blockManager *BlockManager) PutRecord(value []byte) { //funkcija koja dodaje novi zapis
	lastSegment := blockManager.segments[len(blockManager.segments)-1]
	key := lastSegment + "-" + strconv.Itoa(blockManager.currentBlock)

	check := blockManager.bufferPool.Check(key)

	var block []byte
	wasInBuffer := false
	if check {
		block = blockManager.bufferPool.Get(key)
		wasInBuffer = true
	} else {
		var isHeaderBlock bool
		if blockManager.currentBlock == 0 {
			isHeaderBlock = true
		} else {
			isHeaderBlock = false
		}
		block = blockManager.GetWalBlock(lastSegment, blockManager.currentBlock, blockManager.blockSize, isHeaderBlock)
	}

	if len(block)+len(value) <= blockManager.blockSize {
		block = append(block, value...)

		if wasInBuffer {
			blockManager.bufferPool.Update(key, block)
		} else {
			blockManager.PutBlock(key, block)
		}
	} else {
		paddingSize := blockManager.blockSize - len(block)
		padding := make([]byte, paddingSize)
		block = append(block, padding...)

		if wasInBuffer {
			blockManager.bufferPool.Update(key, block)
		} else {
			blockManager.PutBlock(key, block)
		}

		var newBlock []byte
		if blockManager.currentBlock == blockManager.blocksPerSegment {
			blockManager.currentBlock = 0

			newBlock = blockManager.CreateNewSegment()
			lastSegment = blockManager.segments[len(blockManager.segments)-1]
			if len(newBlock)+len(value) > blockManager.blockSize {
				paddingSize := blockManager.blockSize - len(newBlock)
				padding := make([]byte, paddingSize)
				newBlock = append(newBlock, padding...)
				key = lastSegment + "-" + strconv.Itoa(blockManager.currentBlock)
				blockManager.PutBlock(key, newBlock) //upisujemo novi heder blok u bafer ali u njega ne staje nista vise od hedera

				blockManager.currentBlock++
				newBlock = make([]byte, 0) //pravimo novi blok
			}
		} else {
			blockManager.currentBlock++
			newBlock = make([]byte, 0)
		}

		key = lastSegment + "-" + strconv.Itoa(blockManager.currentBlock)
		newBlock = append(newBlock, value...)
		blockManager.PutBlock(key, newBlock)
	}
}

// Funckija koja blok Sstable ubacuje u buffer pool i upisuje ga na disk.
func (blockManager *BlockManager) PutSstableBlock(tableId int, blockIdx int, value []byte, path string) {
	key := fmt.Sprintf("sstable-%d-%d", tableId, blockIdx)

	blockManager.bufferPool.Put(key, value, true)                          //stavljamo blok u bafer pool
	blockManager.WriteBlock(value, blockManager.blockSize, blockIdx, path) //upisujemo blok u sstable fajl
}

// Funckija koja ce fragmentisane zapisa za Sstable ubaciti u blokove, ubaciti ih u buffer pool i upisati na disk.
func (blockManager *BlockManager) PutSstableRecords(
	fragmentsPerRecord map[string][][]byte,
	tableId int,
	path string,
	multipleFiles bool,
) (keys []string, offsets []uint64, dataLength uint64) {
	blockSize := blockManager.blockSize
	block := make([]byte, 0, blockSize)
	var blockIdx int
	if multipleFiles {
		blockIdx = 0
	} else {
		blockIdx = 1 // prvi blok je filter blok, pa pocinjemo od 1
	}
	currentOffset := uint64(0)

	for key, fragments := range fragmentsPerRecord {
		keyOffsetCaptured := false
		for _, fragment := range fragments {
			if len(fragment) > blockSize {
				panic(fmt.Sprintf("Fragment size %d exceeds block size %d – check fragmentation logic", len(fragment), blockSize))
			}

			if len(block)+len(fragment) > blockSize {
				// popuni padding i piši blok
				paddingSize := blockSize - len(block)
				block = append(block, make([]byte, paddingSize)...)

				blockManager.PutSstableBlock(tableId, blockIdx, block, path)
				dataLength += uint64(len(block))
				currentOffset += uint64(len(block))

				blockIdx++
				block = make([]byte, 0, blockSize)
			}

			if !keyOffsetCaptured {
				keys = append(keys, key)
				offsets = append(offsets, currentOffset)
				keyOffsetCaptured = true
			}

			block = append(block, fragment...)
		}
	}

	if len(block) > 0 {
		paddingSize := blockSize - len(block)
		block = append(block, make([]byte, paddingSize)...)

		blockManager.PutSstableBlock(tableId, blockIdx, block, path)
		dataLength += uint64(len(block))
		currentOffset += uint64(len(block))
		blockIdx++
	}

	return keys, offsets, dataLength
}

// Funkcija za ubacivanje zapisa u blokove i upis za ostale strukture Sstable.
func (bm *BlockManager) PutSstableRawBlock(
	fragmentsPerKey map[string][][]byte,
	filename string,
	multipleFiles bool,
	offset uint64,
	returnOffsets bool,
) (writtenKeys []string, offsets []uint64, totalWritten uint64) {
	blockSize := bm.GetBlockSize()
	block := make([]byte, 0, blockSize)
	blockOffset := offset
	totalWritten = 0

	file, err := os.OpenFile(filename, os.O_WRONLY, 0644)
	if err != nil {
		log.Fatalf("Greska pri otvaranju fajla %s: %v", filename, err)
	}
	defer file.Close()

	if !multipleFiles {
		_, err := file.Seek(int64(offset), 0)
		if err != nil {
			log.Fatalf("Greska pri seek-u: %v", err)
		}
	}

	writtenKeys = []string{}
	offsets = []uint64{}

	// Priprema redosleda ključeva
	keys := make([]string, 0, len(fragmentsPerKey))
	for k := range fragmentsPerKey {
		keys = append(keys, k)
	}

	if returnOffsets {
		sort.Strings(keys)
	} else {
		minKey := "MIN"
		maxKey := "MAX"

		otherKeys := make([]string, 0, len(keys))
		for _, k := range keys {
			if k != minKey && k != maxKey {
				otherKeys = append(otherKeys, k)
			}
		}
		sort.Strings(otherKeys)

		keys = []string{}
		if _, ok := fragmentsPerKey[minKey]; ok {
			keys = append(keys, minKey)
		}
		if _, ok := fragmentsPerKey[maxKey]; ok {
			keys = append(keys, maxKey)
		}
		keys = append(keys, otherKeys...)
	}

	for _, key := range keys {
		fragments := fragmentsPerKey[key]
		keyOffsetCaptured := false

		for i, fragment := range fragments {
			if len(fragment) > blockSize {
				panic(fmt.Sprintf("Fragment size %d exceeds block size %d – check fragmentation logic", len(fragment), blockSize))
			}

			if len(block)+len(fragment) > blockSize {
				padding := make([]byte, blockSize-len(block))
				block = append(block, padding...)

				_, err := file.Write(block)
				if err != nil {
					log.Fatalf("Greska pri upisu bloka: %v", err)
				}

				totalWritten += uint64(len(block))
				blockOffset += uint64(len(block))
				block = make([]byte, 0, blockSize)
			}

			if returnOffsets && !keyOffsetCaptured {
				writtenKeys = append(writtenKeys, key)
				offsets = append(offsets, blockOffset)
				keyOffsetCaptured = true
			}

			block = append(block, fragment...)

			// Ako je poslednji fragment i ključ je "MAX", dodaj padding i upiši blok
			if !returnOffsets && key == "MAX" && i == len(fragments)-1 {
				padding := make([]byte, blockSize-len(block))
				block = append(block, padding...)

				_, err := file.Write(block)
				if err != nil {
					log.Fatalf("Greska pri upisu bloka posle MAX: %v", err)
				}

				totalWritten += uint64(len(block))
				blockOffset += uint64(len(block))
				block = make([]byte, 0, blockSize)
			}
		}
	}

	if len(block) > 0 {
		padding := make([]byte, blockSize-len(block))
		block = append(block, padding...)

		_, err := file.Write(block)
		if err != nil {
			log.Fatalf("Greska pri upisu poslednjeg bloka: %v", err)
		}

		totalWritten += uint64(len(block))
	}

	return writtenKeys, offsets, totalWritten
}

func (bm *BlockManager) ReadSstableRecordFromBlock(path string, startOffset uint64, blockSize int) (*WalRecord, error) {
	blockIdx := int(startOffset / uint64(blockSize))
	var fragments []WalRecord
	expectedType := uint8(1)
	foundStart := false

	for {
		block := bm.GetWalBlock(path, blockIdx, blockSize, false)
		if block == nil {
			// Ako nema više blokova i imamo fragmente — stitchuj i vrati
			if foundStart && len(fragments) > 0 {
				return stitchFragments(fragments), nil
			}
			break
		}

		records := ReadAllRecordsSstable(block, int64(blockSize), false, true)
		for _, record := range records {
			if record.CRC == 0 {
				break // padding
			}

			if record.Type == 0 {
				return &record, nil
			}

			if record.Type == 1 && !foundStart {
				fragments = []WalRecord{record}
				foundStart = true
				expectedType = 2
			} else if foundStart && record.Type == expectedType {
				fragments = append(fragments, record)
				expectedType++
			} else if foundStart && record.Type < expectedType {
				return stitchFragments(fragments), nil
			}
		}

		blockIdx++
	}

	if foundStart && len(fragments) > 0 {
		return stitchFragments(fragments), nil
	}

	return nil, fmt.Errorf("no valid record found from offset %d", startOffset)
}

func (bm *BlockManager) ReadSstableRecordsFromBlocks(
	path string,
	startOffset uint64,
	blockSize int,
	maxDataOffset uint64,
) ([]*WalRecord, error) {
	blockIdx := int(startOffset / uint64(blockSize))
	var result []*WalRecord
	var fragments []WalRecord
	expectedType := uint8(1)
	foundStart := false

	for {
		blockOffset := uint64(blockIdx) * uint64(blockSize)
		if blockOffset >= maxDataOffset {
			break
		}

		block := bm.GetWalBlock(path, blockIdx, blockSize, false)
		if block == nil {
			break
		}

		records := ReadAllRecordsSstable(block, int64(blockSize), false, true)
		if len(records) == 0 {
			break
		}

		for _, record := range records {
			if record.CRC == 0 {
				continue // padding
			}

			switch {
			case record.Type == 0:
				// Full record
				result = append(result, &record)

			case record.Type == 1 && !foundStart:
				// Start of fragmentation
				fragments = []WalRecord{record}
				foundStart = true
				expectedType = 2

			case foundStart && record.Type == expectedType:
				// Valid continuation
				fragments = append(fragments, record)
				expectedType++

			case foundStart && record.Type < expectedType:
				// Fragmentacija završena
				full := stitchFragments(fragments)
				result = append(result, full)

				// Reset for potential next record
				fragments = nil
				foundStart = false
				expectedType = 1
			}
		}

		blockIdx++
	}

	// Ako fragmentacija ide do kraja fajla — stitchuj poslednji fragment
	if foundStart && len(fragments) > 0 {
		full := stitchFragments(fragments)
		result = append(result, full)
	}

	if len(result) == 0 {
		return nil, fmt.Errorf("no records found starting at offset %d", startOffset)
	}
	return result, nil
}

// Zapisuje Bloom filter u blok u datoteci na datom offsetu.
func (bm *BlockManager) WriteBloomFilter(filterData []byte, filename string) (written uint64) {
	blockSize := bm.GetBlockSize()

	// Napuni blok paddingom ako
	block := make([]byte, blockSize)
	copy(block, filterData)

	file, err := os.OpenFile(filename, os.O_WRONLY, 0644)
	if err != nil {
		log.Fatalf("Greška pri otvaranju fajla: %v", err)
	}
	defer file.Close()

	n, err := file.Write(block)
	if err != nil {
		log.Fatalf("Greška pri upisu Bloom filter bloka: %v", err)
	}

	return uint64(n)
}

func stitchFragments(fragments []WalRecord) *WalRecord {
	fullValue := []byte{}
	fullKey := []byte{}
	for _, frag := range fragments {
		fullValue = append(fullValue, frag.Value...)
		fullKey = append(fullKey, frag.Key...)
	}

	// Poslednji fragment sadrži TOMBSTONE
	last := fragments[len(fragments)-1]

	return &WalRecord{
		CRC:       0,
		Size:      0,
		Type:      0,
		Timestamp: 0,
		Tombstone: last.Tombstone,
		KeySize:   uint64(len(fullKey)),
		ValueSize: uint64(len(fullValue)),
		Key:       string(fullKey),
		Value:     fullValue,
	}
}

func (blockManager *BlockManager) CreateNewSegment() []byte { //funkcija koja kreira novi segment u WAL-u
	lastSegment := blockManager.segments[len(blockManager.segments)-1]
	lastSegmentName := strings.TrimSuffix(lastSegment, ".log")
	parts := strings.Split(lastSegmentName, "_")
	lastSegmentNumber, err := strconv.Atoi(parts[len(parts)-1])
	if err != nil {
		log.Fatal("Greska: ", err)
		return nil
	}
	newSegmentNumber := lastSegmentNumber + 1
	newSegment := blockManager.walPath + "/wal_" + strconv.Itoa(newSegmentNumber) + ".log"
	blockManager.segments = append(blockManager.segments, newSegment)

	file, err := os.Create(newSegment)
	if err != nil {
		log.Println("Greska pri kreiranju novog segmenta: ", newSegment)
		log.Println("Nije moguce nastaviti sa radom")
		log.Fatal("Greska: ", err)
		return nil
	}

	header := make([]byte, 16)
	binary.LittleEndian.PutUint32(header[:4], uint32(blockManager.blockSize))
	binary.LittleEndian.PutUint32(header[4:8], uint32(blockManager.blocksPerSegment))
	binary.LittleEndian.PutUint32(header[8:12], uint32(newSegmentNumber))
	binary.LittleEndian.PutUint32(header[12:16], uint32(0))

	_, err = file.Write(header)

	if err != nil {
		log.Println("Greska pri pisanju hedera u novi segment: ", newSegment)
		log.Println("Nije moguce nastaviti sa radom")
		log.Fatal("Greska: ", err)
		return nil
	}

	file.Close()

	blockManager.orderOfSegments[len(blockManager.segments)-1] = newSegmentNumber

	return header //vracamo header blok koji je upisan u novi segment
}

func (blockManager *BlockManager) DeleteSegments(lowWatermark int) { //funkcija koja nakon flush-a brise segmente iz WAL-a koji se vise ne koriste
	for i := lowWatermark - 1; i >= 0; i-- {
		os.Remove(blockManager.segments[i])
		blockManager.segments = append(blockManager.segments[:i], blockManager.segments[i+1:]...)
	}
}

func (blockManager *BlockManager) FlushUnsaved() { //funkcija koja upisuje sve neupisane blokove u WAL
	blocksForFlush := blockManager.bufferPool.Flush()

	for key, block := range blocksForFlush {
		parts := strings.Split(key, "-")
		blockNumber, err := strconv.Atoi(parts[1])
		if err != nil {
			log.Println("Greska pri konverziji broja bloka:", err)
			continue
		}

		if len(block) != blockManager.blockSize {
			paddingSize := blockManager.blockSize - len(block)
			padding := make([]byte, paddingSize)
			block = append(block, padding...)
		}
		blockManager.WriteBlock(block, blockManager.blockSize, blockNumber, parts[0]) //upisujemo blok u wal
	}
}

func (blockManager *BlockManager) UpdateWalStart(startBlock int) { // azuriram prvi korisni blok prvog segmenta nakon brisanja segmenata
	segment := blockManager.segments[0]

	file, err := os.OpenFile(segment, os.O_WRONLY, 0644)

	if err != nil {
		panic(err)
	}

	defer file.Close()
	file.Seek(12, 0)
	header := make([]byte, 4)
	binary.LittleEndian.PutUint32(header, uint32(startBlock))
	_, err = file.Write(header)
	if err != nil {
		panic(err)
	}
}
