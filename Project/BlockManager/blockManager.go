package BlockManager

//importovanje potrebnih paketa
import (
	"encoding/binary"
	"fmt"
	"os"
	"strconv"
)

type BlockManager struct {
	blockSize        int
	blocksPerSegment int
	bufferPool       *BufferPool
	currentSegment   int
	currentBlock     int
	walPath          string        //putanja do wal foldera
	segments         []string      //lista segmenata
	orderOfSegments  map[int]int   //redosled segmenata, kljuc je redosled, vrednost je broj segmenta jer ne mora da bude od 0
	memTableSegments map[int][]int //mapira koji segmenti su od kog memtable-a
}

func (blockManeger *BlockManager) GetBlockSize() int { //geter za velicinu bloka
	return blockManeger.blockSize
}


func (blockManager *BlockManager) GetSegments() []string {
	return blockManager.segments
}

func (blockManager *BlockManager) GetCurrentSegment() int {
	return blockManager.currentSegment
}

func (blockManager *BlockManager) GetCurrentBlock() int {
	return blockManager.currentBlock
}

func NewBlockManager(blockSize, blocksPerSegment, poolSize int, walPath string) *BlockManager { //konstruktor za BlockManager
	bufferPool := NewBufferPool(poolSize)

	files, err := os.ReadDir(walPath)
	if err != nil {
		panic(err)
	}

	segments := make([]string, 0)

	var currentSegment int
	var currentBlock int
	orderOfSegments := make(map[int]int)
	if len(files) == 0 { //pravimo pocetni segment
		segment := walPath + "/wal_0.log"
		segments = append(segments, segment)

		file, err := os.Create(segment)
		if err != nil {
			panic(err)
		}

		//dodajemo heder koji ce sadrzati informacije o velicini bloka, broju blokova po segmentu i koji je broj segmenta zbog mogucih promena
		//dodajemo i koji je prvi koristan blok u segmentu
		headerBlock := make([]byte, blockSize-16)
		header := make([]byte, 16)
		binary.LittleEndian.PutUint32(header[:4], uint32(blockSize))
		binary.LittleEndian.PutUint32(header[4:8], uint32(blocksPerSegment))
		binary.LittleEndian.PutUint32(header[8:12], uint32(0))
		binary.LittleEndian.PutUint32(header[12:16], uint32(1)) //ovo se koristi za memtable rekonstrukciju i menja se posle flasovanja memtable-a

		headerBlock = append(header, headerBlock...)

		_, err = file.Write(headerBlock)

		if err != nil {
			panic(err)
		}

		file.Close()

		currentSegment = 0
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
				panic(err)
			}

			_, err = file.Read(headerBlock)

			if err != nil {
				panic(err)
			}

			copy(hederBytes, headerBlock[:16])

			blockSizeOfSegment := int(binary.LittleEndian.Uint32(hederBytes[:4]))
			blocksPerSegmentOfSegment := int(binary.LittleEndian.Uint32(hederBytes[4:8]))
			segmentNumber := int(binary.LittleEndian.Uint32(hederBytes[8:12]))

			//ovo treba da se napravi da moze da se oporavi sistem ako se promeni velicina bloka ili broj blokova po segmentu
			//za sada samo proveravamo da li su isti
			if blockSizeOfSegment != blockSize || blocksPerSegmentOfSegment != blocksPerSegment {
				panic("Segment " + segment.Name() + " has different block size or blocks per segment")
			}

			file.Close()

			for i := 0; i <= len(segments); i++ { //prolazimo kroz segmente i trazimo gde da ubacimo novi segment da bude sortiran
				if i == len(segments) {
					segments = append(segments, walPath+"/"+segment.Name())
					orderOfSegments[i] = segmentNumber
					break
				}

				if segmentNumber < orderOfSegments[i] {
					segments = append(segments, "")
					copy(segments[i+1:], segments[i:])
					segments[i] = walPath + "/" + segment.Name()

					for j := i; j < len(segments); j++ {
						orderOfSegments[j]++
					}

					orderOfSegments[i] = segmentNumber
					break
				}
			}
		}
	}

	return &BlockManager{
		blockSize:        blockSize,
		blocksPerSegment: blocksPerSegment,
		bufferPool:       &bufferPool,
		currentSegment:   currentSegment,
		currentBlock:     currentBlock,
		walPath:          walPath,
		segments:         segments,
		orderOfSegments:  orderOfSegments,
		memTableSegments: make(map[int][]int),
	}
}

func (blockManager *BlockManager) Reconstruct() []WalRecord{ //funkcija koja podize sistem i rekonstruise stanje
	endOfLastSegment := false
	fullSegment := false

	fullRecords := []WalRecord{}
	fragmentedParts := []WalRecord{}
	currentFragmentedPosition := 1
	for _, segment := range blockManager.segments {
		fullSegment = false
		for blockManager.currentBlock <= blockManager.blocksPerSegment {
			key := segment + "-" + strconv.Itoa(blockManager.currentBlock)

			if blockManager.currentBlock == 0 {
				block := blockManager.GetWalBlock(segment, blockManager.currentBlock)
				firstBlock := int(binary.LittleEndian.Uint32(block[12:16]))
				blockManager.currentBlock = firstBlock
				fmt.Println("First block: ", firstBlock)
				continue
			}

			block := blockManager.GetWalBlock(segment, blockManager.currentBlock)
			if block != nil {
				blockManager.bufferPool.Put(key, block, false)

				//ovde treba da se blok prosledi memtable-u na rekonstrukciju
				//radi provere sada samo ispisujemo blok
				records := ReadAllRecords(block, int64(blockManager.blockSize))

				for _, record := range records {
					if record.Tombstone {
						continue
					}

					if record.Type == 0 {
						fullRecords = append(fullRecords, record)
					} else {

						if currentFragmentedPosition == int(record.Type) {
							fragmentedParts = append(fragmentedParts, record)
							currentFragmentedPosition++
						} else {
							currentFragmentedPosition = 1

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
							fragmentedParts = []WalRecord{}

							fragmentedParts = append(fragmentedParts, record)
							currentFragmentedPosition++
						}
					}
				}
			} else {
				endOfLastSegment = true
				break
			}
			blockManager.currentBlock++
		}
		if endOfLastSegment {
			break
		}

		fullSegment = true

		blockManager.currentBlock = 0
		blockManager.currentSegment++
	}

	if fullSegment {
		blockManager.currentSegment--
		blockManager.currentBlock = blockManager.blocksPerSegment
	}

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

	i := 0
	for _, record := range fullRecords {
		value := string(record.Value)
		println(i, " - ", value)
		i++
	}
	return fullRecords
}

func (blockManager *BlockManager) GetWalBlock(filename string, blockPosition int) []byte { //funkcija koja vraca zadati blok iz WAL-a
	file, err := os.OpenFile(filename, os.O_RDONLY, 0644)

	if err != nil {
		panic(err)
	}

	defer file.Close()

	seekValue := int64(blockPosition * blockManager.blockSize)
	info, err := file.Stat()
	if err != nil {
		panic(err)
	}

	if seekValue >= info.Size() {
		return nil
	}

	file.Seek(int64(blockPosition*blockManager.blockSize), 0)

	block := make([]byte, blockManager.blockSize)
	_, err = file.Read(block)

	if err != nil {
		panic(err)
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

func (blockManager *BlockManager) PutBlock(key string, value []byte) { //funkcija koja dodaje blok
	check := blockManager.bufferPool.Check(key)
	var returnedValue []byte
	if check {
		blockManager.bufferPool.Update(key, value)
		return
	} else {
		returnedValue = blockManager.bufferPool.Put(key, value, true)
	}

	if returnedValue != nil && key[0] == 'w' { //ako je vracen blok znaci da je on izmenjen u bafer pool-u i treba ga upisati na disk ali samo ako je iz wal-a
		blockManager.writeBlock(returnedValue)
	}
}

func (blockManager *BlockManager) writeBlock(value []byte) { //funkcija koja upisuje blok u WAL
	segment := blockManager.segments[blockManager.currentSegment]

	file, err := os.OpenFile(segment, os.O_WRONLY, 0644)

	if err != nil {
		panic(err)
	}

	defer file.Close()

	file.Seek(int64(blockManager.currentBlock*blockManager.blockSize), 0)
	_, err = file.Write(value)

	if err != nil {
		panic(err)
	}

	if blockManager.currentBlock == blockManager.blocksPerSegment {
		blockManager.currentBlock = 0
		blockManager.currentSegment++

		segment := blockManager.walPath + "/wal_" + strconv.Itoa(blockManager.currentSegment) + ".log"
		blockManager.segments = append(blockManager.segments, segment)

		file, err := os.Create(segment)
		if err != nil {
			panic(err)
		}

		headerBlock := make([]byte, blockManager.blockSize-16)
		header := make([]byte, 16)
		binary.LittleEndian.PutUint32(header[:4], uint32(blockManager.blockSize))
		binary.LittleEndian.PutUint32(header[4:8], uint32(blockManager.blocksPerSegment))
		binary.LittleEndian.PutUint32(header[8:12], uint32(blockManager.currentSegment))
		binary.LittleEndian.PutUint32(header[12:16], uint32(1))

		headerBlock = append(header, headerBlock...)

		_, err = file.Write(headerBlock)

		if err != nil {
			panic(err)
		}

		file.Close()

		blockManager.currentBlock = 1

		blockManager.orderOfSegments[len(blockManager.segments)-1] = blockManager.currentSegment
	} else {
		blockManager.currentBlock++
	}
}

func (blockManager *BlockManager) PutRecord(value []byte) { //funkcija koja dodaje novi zapis
	key := blockManager.segments[blockManager.currentSegment] + "-" + strconv.Itoa(blockManager.currentBlock)

	check := blockManager.bufferPool.Check(key)

	if check {
		block := blockManager.bufferPool.Get(key)
		if len(block)+len(value) <= blockManager.blockSize {
			block = append(block, value...)
			blockManager.bufferPool.Update(key, block)
		} else {
			paddingSize := blockManager.blockSize - len(block)
			padding := make([]byte, paddingSize)
			block = append(block, padding...)
			blockManager.bufferPool.Update(key, block)

			blockManager.writeBlock(block)

			key = blockManager.segments[blockManager.currentSegment] + "-" + strconv.Itoa(blockManager.currentBlock)
			newBlock := []byte{}
			newBlock = append(newBlock, value...)
			blockManager.PutBlock(key, newBlock)
		}
	} else {
		newBlock := []byte{}
		newBlock = append(newBlock, value...)
		blockManager.PutBlock(key, newBlock)
	}
}

func (blockManager *BlockManager) DeleteSegments(lowWatermark int) { //funkcija koja brise segmente iz WAL-a koji se vise ne koriste
	for i := 0; i < lowWatermark; i++ {
		os.Remove(blockManager.segments[i])
	}
	blockManager.segments = blockManager.segments[lowWatermark:]
}
