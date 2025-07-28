package BlockManager

import (
	"encoding/binary"
	"hash/crc32"
	"log"
	"time"
)

/*
   +---------------+------------+----------+------+---------------+---------------+-----------------+-...-+--...--+
   |    CRC (4B)   |  Size(4B)  | Type(1B) | Timestamp (8B) |Tombstone (1B) | Key Size (8B) | Value Size (8B) | Key | Value |
   +---------------+------------+----------+------+---------------+---------------+-----------------+-...-+--...--+
   CRC = provera ispravnosti podataka, da se nisu promenili
   Size = velicina zapisa
   Type = deo zapisa (0-full, 1-first, 2-middle, 3-last)
   Timestamp = vreme kada je zapis napravljen
   Tombstone = da li je zapis obrisan
   Key Size = velicina kljuca
   Value Size = velicina vrednosti
   Key = vrednost kljuca
   Value = vrednost vrednosti
*/

type WalRecord struct { //struktura koja predstavlja zapis u WAL-u
	CRC       uint32
	Size      uint32
	Type      uint8
	Timestamp uint64
	Tombstone bool
	KeySize   uint64
	ValueSize uint64
	Key       string
	Value     []byte
}

// const ( //konstante koje predstavljaju pocetne pozicije svakog dela zapisa
// 	CRC_SIZE        = 4
// 	SIZE_SIZE       = 4
// 	TYPE_SIZE       = 1
// 	TIMESTAMP_SIZE  = 8
// 	TOMBSTONE_SIZE  = 1
// 	KEY_SIZE_SIZE   = 8
// 	VALUE_SIZE_SIZE = 8

// 	CRC_START        = 0
// 	SIZE_START       = CRC_START + CRC_SIZE
// 	TYPE_START       = SIZE_START + SIZE_SIZE
// 	TIMESTAMP_START  = TYPE_START + TYPE_SIZE
// 	TOMBSTONE_START  = TIMESTAMP_START + TIMESTAMP_SIZE
// 	KEY_SIZE_START   = TOMBSTONE_START + TOMBSTONE_SIZE
// 	VALUE_SIZE_START = KEY_SIZE_START + KEY_SIZE_SIZE
// 	KEY_START        = VALUE_SIZE_START + VALUE_SIZE_SIZE
// )

func CRC32(data []byte) uint32 { //funkcija koja racuna CRC32 vrednost za zadati niz bajtova
	return crc32.ChecksumIEEE(data)
}

func CreateBinaryRecordsWithFragmentation(tombstone bool, key string, value []byte, blockSize int, forSstable bool, oldTimestamp uint64) [][]byte { //funkcija koja kreira zapise sa fragmentacijom
	binaryRecords := [][]byte{}

	keySize := uint64(len(key))
	valueSize := uint64(len(value))

	//moramo da izracunamo velicinu svakog dela zapisa da bismo mogli primetiti fragmentaciju
	typeSize := 1

	var timeStamp uint64
	if forSstable {
		timeStamp = oldTimestamp
	} else {
		timeStamp = uint64(time.Now().UnixMicro())
	}
	timeStampBytes := make([]byte, binary.MaxVarintLen64)
	timeStampSize := binary.PutUvarint(timeStampBytes, uint64(timeStamp))

	tombstoneSize := 1

	keySizeBytes := make([]byte, binary.MaxVarintLen64)
	keySizeSize := binary.PutUvarint(keySizeBytes, keySize)

	valueSizeBytes := make([]byte, binary.MaxVarintLen64)
	valueSizeSize := binary.PutUvarint(valueSizeBytes, valueSize)

	crcSize := 4
	fixedSize := uint64(typeSize) + uint64(timeStampSize) + uint64(tombstoneSize) + uint64(keySizeSize) + uint64(valueSizeSize) + keySize + valueSize + uint64(crcSize) //mora petlja jer se ne zna koliko ce zauzimati sam size
	size := fixedSize
	var sizeBytes []byte
	var sizeSize int
	for {
		sizeBytes = make([]byte, binary.MaxVarintLen64)
		sizeSize = binary.PutUvarint(sizeBytes, size)
		newSize := fixedSize + uint64(sizeSize)
		if newSize == size {
			break
		}
		size = newSize
	}

	constMaxSize := uint64(blockSize) - uint64(crcSize) - uint64(sizeSize) - uint64(typeSize) - uint64(timeStampSize) - uint64(tombstoneSize) - uint64(keySizeSize) - uint64(valueSizeSize)

	if keySize+valueSize <= constMaxSize {
		var walRecord WalRecord
		if forSstable {
			walRecord = CreateWalRecord(tombstone, key, value, 0, timeStamp)
		} else {
			walRecord = CreateWalRecord(tombstone, key, value, 0, 0) //za memtable i wal ne treba stari timestamp
		}
		binaryRecord := walRecord.CreateBinaryRecord(forSstable)
		binaryRecords = append(binaryRecords, binaryRecord)
	} else {
		fragmentedConstMaxSize := blockSize - crcSize - sizeSize - typeSize - keySizeSize - valueSizeSize

		firstFragmentation := true
		newKeySize := uint64(0)
		newValueSize := uint64(0)
		var binaryRecord []byte
		numberOfFragmentation := 1
		for { //dokle god je kljuc ili vrednost veca od maksimalne velicine bloka
			if (firstFragmentation && keySize > constMaxSize) || (!firstFragmentation && keySize > uint64(fragmentedConstMaxSize)) { //ako je kljuc veci od jednog pocetnog bloka
				if firstFragmentation {
					newKeySize = constMaxSize
				} else {
					newKeySize = uint64(fragmentedConstMaxSize)
				}

				newKey := key[:newKeySize]
				newValue := []byte{}

				if firstFragmentation {
					var walRecord WalRecord
					if forSstable {
						walRecord = CreateWalRecord(tombstone, newKey, newValue, numberOfFragmentation, timeStamp)
					} else {
						walRecord = CreateWalRecord(tombstone, newKey, newValue, numberOfFragmentation, 0) //za memtable i wal ne treba stari timestamp
					}
					binaryRecord = walRecord.CreateBinaryRecord(forSstable)
				} else {
					walRecord := CreateFragmentedWalRecord(newKey, newValue, numberOfFragmentation)
					binaryRecord = walRecord.CreateFragmentedBinaryRecord(forSstable)
				}
				binaryRecords = append(binaryRecords, binaryRecord)

				numberOfFragmentation++
				key = key[newKeySize:]
				keySize = keySize - newKeySize
			} else if (firstFragmentation && valueSize > constMaxSize) || (!firstFragmentation && valueSize > uint64(fragmentedConstMaxSize)) { //ako je vrednost veca od jednog pocetnog bloka
				if firstFragmentation {
					newValueSize = constMaxSize
				} else {
					newValueSize = uint64(fragmentedConstMaxSize)
				}

				newKey := ""
				newValue := value[:newValueSize]

				var walRecord WalRecord
				if firstFragmentation {
					if forSstable {
						walRecord = CreateWalRecord(tombstone, newKey, newValue, numberOfFragmentation, timeStamp)
					} else {
						walRecord = CreateWalRecord(tombstone, newKey, newValue, numberOfFragmentation, 0) //za memtable i wal ne treba stari timestamp
					}
					binaryRecord = walRecord.CreateBinaryRecord(forSstable)
				} else {

					if forSstable {
						walRecord = CreateFragmentedWalRecord(newKey, newValue, numberOfFragmentation)
					} else {
						walRecord = CreateFragmentedWalRecord(newKey, newValue, numberOfFragmentation)
					}
					binaryRecord = walRecord.CreateFragmentedBinaryRecord(forSstable)
				}
				binaryRecords = append(binaryRecords, binaryRecord)

				numberOfFragmentation++
				value = value[newValueSize:]
				valueSize = valueSize - newValueSize
			} else { //ako je i kljuc i vrednost manji od maksimalne velicine bloka
				walRecord := CreateFragmentedWalRecord(key, value, numberOfFragmentation)
				binaryRecord = walRecord.CreateFragmentedBinaryRecord(forSstable)
				binaryRecords = append(binaryRecords, binaryRecord)
				break
			}

			firstFragmentation = false
		}
	}

	return binaryRecords
}

func CreateWalRecord(tombstone bool, key string, value []byte, fragment int, oldTimeStamp uint64) WalRecord { //funkcija koja kreira objekat WalRecord, tj. objekat za zapis u WAL-u
	keySize := uint64(len(key))
	valueSize := uint64(len(value))

	typeSize := 1
	typeBytes := make([]byte, typeSize)
	typeBytes[0] = byte(fragment)

	var timeStamp uint64
	if oldTimeStamp == 0 {
		timeStamp = uint64(time.Now().UnixMicro())
	} else {
		timeStamp = oldTimeStamp
	}

	timeStampBytes := make([]byte, binary.MaxVarintLen64)
	timeStampSize := binary.PutUvarint(timeStampBytes, uint64(timeStamp))

	tombstoneSize := 1
	tombstoneBytes := make([]byte, tombstoneSize)
	if tombstone {
		tombstoneBytes[0] = 1
	} else {
		tombstoneBytes[0] = 0
	}

	keySizeBytes := make([]byte, binary.MaxVarintLen64)
	keySizeSize := binary.PutUvarint(keySizeBytes, keySize)

	valueSizeBytes := make([]byte, binary.MaxVarintLen64)
	valueSizeSize := binary.PutUvarint(valueSizeBytes, valueSize)

	crcSize := 4                                                                                                                                                        //CRC mora biti uvek 4 bajta
	fixedSize := uint64(typeSize) + uint64(timeStampSize) + uint64(tombstoneSize) + uint64(keySizeSize) + uint64(valueSizeSize) + keySize + valueSize + uint64(crcSize) //mora petlja jer se ne zna koliko ce zauzimati sam size
	size := fixedSize
	var sizeBytes []byte
	var sizeSize int
	for {
		sizeBytes = make([]byte, binary.MaxVarintLen64)
		sizeSize = binary.PutUvarint(sizeBytes, size)
		newSize := fixedSize + uint64(sizeSize)
		if newSize == size {
			size = newSize
			break
		}
		size = newSize
	}

	keyBytes := []byte(key)

	crcData := make([]byte, 0)
	crcData = append(crcData, sizeBytes[:sizeSize]...)
	crcData = append(crcData, typeBytes[0])
	crcData = append(crcData, timeStampBytes[:timeStampSize]...)
	crcData = append(crcData, tombstoneBytes[0])
	crcData = append(crcData, keySizeBytes[:keySizeSize]...)
	crcData = append(crcData, valueSizeBytes[:valueSizeSize]...)
	crcData = append(crcData, keyBytes...)
	crcData = append(crcData, value...)

	crc := CRC32(crcData)

	return WalRecord{
		CRC:       crc,
		Size:      uint32(size),
		Type:      uint8(fragment),
		Timestamp: uint64(timeStamp),
		Tombstone: tombstone,
		KeySize:   keySize,
		ValueSize: valueSize,
		Key:       key,
		Value:     value,
	}
}

func (walRecord *WalRecord) CreateBinaryRecord(forSstable bool) []byte { //funkcija koja pretvara zapis u niz bajtova

	bytesRecord := []byte{}

	bytesUsed := 4
	crcBytes := make([]byte, bytesUsed)
	binary.LittleEndian.PutUint32(crcBytes, walRecord.CRC)
	bytesRecord = append(bytesRecord, crcBytes...)

	sizeBytes := make([]byte, binary.MaxVarintLen64)
	bytesUsed = binary.PutUvarint(sizeBytes, uint64(walRecord.Size))
	bytesRecord = append(bytesRecord, sizeBytes[:bytesUsed]...)

	typeBytes := []byte{byte(walRecord.Type)} //svakako samo 1 bajt
	bytesRecord = append(bytesRecord, typeBytes[0])

	timeStampBytes := make([]byte, binary.MaxVarintLen64)
	bytesUsed = binary.PutUvarint(timeStampBytes, uint64(walRecord.Timestamp))
	bytesRecord = append(bytesRecord, timeStampBytes[:bytesUsed]...)

	tombstoneBytes := make([]byte, 1)
	if walRecord.Tombstone {
		tombstoneBytes[0] = 1
	} else {
		tombstoneBytes[0] = 0
	}
	bytesRecord = append(bytesRecord, tombstoneBytes[0])

	keySizeBytes := make([]byte, binary.MaxVarintLen64)
	bytesUsed = binary.PutUvarint(keySizeBytes, walRecord.KeySize)
	bytesRecord = append(bytesRecord, keySizeBytes[:bytesUsed]...)

	if !(forSstable && walRecord.Tombstone) {
		valueSizeBytes := make([]byte, binary.MaxVarintLen64)
		bytesUsed = binary.PutUvarint(valueSizeBytes, walRecord.ValueSize)
		bytesRecord = append(bytesRecord, valueSizeBytes[:bytesUsed]...)
	}

	keyBytes := []byte(walRecord.Key)
	bytesRecord = append(bytesRecord, keyBytes...)

	if !(forSstable && walRecord.Tombstone) {
		bytesRecord = append(bytesRecord, walRecord.Value...)
	}
	return bytesRecord
}

func CreateFragmentedWalRecord(key string, value []byte, fragment int) WalRecord { //funkcija koja kreira fragmentisani zapis
	keySize := uint64(len(key))
	valueSize := uint64(len(value))

	typeSize := 1
	typeBytes := make([]byte, typeSize)
	typeBytes[0] = byte(fragment)

	keySizeBytes := make([]byte, binary.MaxVarintLen64)
	keySizeSize := binary.PutUvarint(keySizeBytes, keySize)

	valueSizeBytes := make([]byte, binary.MaxVarintLen64)
	valueSizeSize := binary.PutUvarint(valueSizeBytes, valueSize)

	crcSize := 4                                                                                                        //CRC mora biti uvek 4 bajta
	fixedSize := uint64(typeSize) + uint64(keySizeSize) + uint64(valueSizeSize) + keySize + valueSize + uint64(crcSize) //mora petlja jer se ne zna koliko ce zauzimati sam size
	size := fixedSize
	var sizeBytes []byte
	var sizeSize int
	for {
		sizeBytes = make([]byte, binary.MaxVarintLen64)
		sizeSize = binary.PutUvarint(sizeBytes, size)
		newSize := fixedSize + uint64(sizeSize)
		if newSize == size {
			size = newSize
			break
		}
		size = newSize
	}

	keyBytes := []byte(key)

	crcData := make([]byte, 0)
	crcData = append(crcData, sizeBytes[:sizeSize]...)
	crcData = append(crcData, typeBytes[0])
	crcData = append(crcData, keySizeBytes[:keySizeSize]...)
	crcData = append(crcData, valueSizeBytes[:valueSizeSize]...)
	crcData = append(crcData, keyBytes...)
	crcData = append(crcData, value...)

	crc := CRC32(crcData)

	return WalRecord{
		CRC:       crc,
		Size:      uint32(size),
		Type:      typeBytes[0],
		Timestamp: 0,
		Tombstone: false,
		KeySize:   keySize,
		ValueSize: valueSize,
		Key:       key,
		Value:     value,
	}
}

func (walRecord *WalRecord) CreateFragmentedBinaryRecord(forSstable bool) []byte { //funkcija koja pravi niz bajtova od fragmentisanog zapisa
	bytesRecord := []byte{}

	bytesUsed := 4
	crcBuffer := make([]byte, bytesUsed)
	binary.LittleEndian.PutUint32(crcBuffer, walRecord.CRC)
	bytesRecord = append(bytesRecord, crcBuffer...)

	sizeBuffer := make([]byte, binary.MaxVarintLen64)
	bytesUsed = binary.PutUvarint(sizeBuffer, uint64(walRecord.Size))
	bytesRecord = append(bytesRecord, sizeBuffer[:bytesUsed]...)

	typeBytes := []byte{byte(walRecord.Type)} //svakako samo 1 bajt
	bytesRecord = append(bytesRecord, typeBytes...)

	keySizeBuffer := make([]byte, binary.MaxVarintLen64)
	bytesUsed = binary.PutUvarint(keySizeBuffer, walRecord.KeySize)
	bytesRecord = append(bytesRecord, keySizeBuffer[:bytesUsed]...)

	if !(forSstable && walRecord.Tombstone) {
		valueSizeBytes := make([]byte, binary.MaxVarintLen64)
		bytesUsed = binary.PutUvarint(valueSizeBytes, walRecord.ValueSize)
		bytesRecord = append(bytesRecord, valueSizeBytes[:bytesUsed]...)
	}

	keyBytes := []byte(walRecord.Key)
	bytesRecord = append(bytesRecord, keyBytes...)

	if !(forSstable && walRecord.Tombstone) {
		bytesRecord = append(bytesRecord, walRecord.Value...)
	}
	return bytesRecord
}

func ReadAllRecords(block []byte, blockSize int64, isHeaderBlock bool) []WalRecord { //funkcija koja cita sve zapise iz jednog bloka
	records := []WalRecord{}

	if isHeaderBlock {
		blockSize -= 16 //ako je header blok, onda treba da se oduzmu 16 bajtova za header
	}

	i := int64(0)

	for i < blockSize {
		crcStart := i
		crcSize := 4 //CRC mora biti uvek 4 bajta

		if crcStart+int64(crcSize) >= blockSize { //ako je slucajno padding
			break
		}

		crcBytes := make([]byte, 4)
		copy(crcBytes, block[crcStart:crcStart+4])
		crc := binary.LittleEndian.Uint32(crcBytes)

		sizeStart := crcStart + int64(crcSize)
		if sizeStart >= blockSize { //ako je slucajno padding
			break
		}
		size, sizeSize := binary.Uvarint(block[sizeStart:])
		if size == 0 { //provera da nije padding
			break
		}

		typeBytes := make([]byte, 1)
		typeStart := sizeStart + int64(sizeSize)
		typeSize := 1
		copy(typeBytes, block[typeStart:typeStart+int64(typeSize)])

		typeRecord := typeBytes[0]

		var timeStampStart int64
		var timeStamp uint64
		var timeStampSize int
		var tombstoneStart int64
		var tombstone bool
		var tombstoneSize int
		var tombstoneBytes []byte
		if typeRecord == 0 || typeRecord == 1 {

			timeStampStart = typeStart + int64(typeSize)
			timeStamp, timeStampSize = binary.Uvarint(block[timeStampStart:])

			tombstoneBytes = make([]byte, 1)
			tombstoneStart = timeStampStart + int64(timeStampSize)
			tombstoneSize = 1
			copy(tombstoneBytes, block[tombstoneStart:tombstoneStart+int64(tombstoneSize)])
			if tombstoneBytes[0] == 1 {
				tombstone = true
			} else {
				tombstone = false
			}
		}

		var keySizeStart int64
		var keySize uint64
		var keySizeSize int
		if typeRecord == 0 || typeRecord == 1 {
			keySizeStart = tombstoneStart + int64(tombstoneSize)
		} else {
			keySizeStart = typeStart + int64(typeSize)
		}
		keySize, keySizeSize = binary.Uvarint(block[keySizeStart:])

		valueSizeStart := keySizeStart + int64(keySizeSize)
		valueSize, valueSizeSize := binary.Uvarint(block[valueSizeStart:])

		keyStart := valueSizeStart + int64(valueSizeSize)
		keyBytes := make([]byte, keySize)
		copy(keyBytes, block[keyStart:keyStart+int64(keySize)])
		key := string(keyBytes)

		valueStart := keyStart + int64(keySize)
		valueBytes := make([]byte, valueSize)
		copy(valueBytes, block[valueStart:valueStart+int64(valueSize)])

		readCRCData := make([]byte, 0) //provera CRC32 vrednosti
		readCRCData = append(readCRCData, block[sizeStart:sizeStart+int64(sizeSize)]...)
		readCRCData = append(readCRCData, typeBytes...)
		if typeRecord == 0 || typeRecord == 1 {
			readCRCData = append(readCRCData, block[timeStampStart:timeStampStart+int64(timeStampSize)]...)
			readCRCData = append(readCRCData, tombstoneBytes...)
		}
		readCRCData = append(readCRCData, block[keySizeStart:keySizeStart+int64(keySizeSize)]...)
		readCRCData = append(readCRCData, block[valueSizeStart:valueSizeStart+int64(valueSizeSize)]...)
		readCRCData = append(readCRCData, keyBytes...)
		readCRCData = append(readCRCData, valueBytes...)

		readCRC := CRC32(readCRCData)

		if readCRC != uint32(crc) {
			log.Print("CRC32 vrednost nije ispravna, zapis je korumpiran")
			log.Print("Ovaj zapis je preskocen")
			i += int64(size) //preskacem ovaj zapis
			continue
		}

		newWalRecord := WalRecord{
			CRC:       uint32(crc),
			Size:      uint32(size),
			Type:      typeRecord,
			Timestamp: timeStamp,
			Tombstone: tombstone,
			KeySize:   keySize,
			ValueSize: valueSize,
			Key:       key,
			Value:     valueBytes,
		}

		records = append(records, newWalRecord)

		i += int64(size)
	}

	return records
}

func ReadAllRecordsSstable(block []byte, blockSize int64, isHeaderBlock bool, forSstable bool) []WalRecord {
	records := []WalRecord{}

	if isHeaderBlock {
		blockSize -= 16 //ako je header blok, onda treba da se oduzmu 16 bajtova za header
	}

	i := int64(0)

	for i < blockSize {
		crcStart := i
		crcSize := 4 //CRC mora biti uvek 4 bajta

		if crcStart+int64(crcSize) >= blockSize { //ako je slucajno padding
			break
		}

		crcBytes := make([]byte, 4)
		copy(crcBytes, block[crcStart:crcStart+4])
		crc := binary.LittleEndian.Uint32(crcBytes)

		sizeStart := crcStart + int64(crcSize)
		if sizeStart >= blockSize { //ako je slucajno padding
			break
		}
		size, sizeSize := binary.Uvarint(block[sizeStart:])
		if size == 0 { //provera da nije padding
			break
		}

		typeBytes := make([]byte, 1)
		typeStart := sizeStart + int64(sizeSize)
		typeSize := 1
		copy(typeBytes, block[typeStart:typeStart+int64(typeSize)])

		typeRecord := typeBytes[0]

		var timeStampStart int64
		var timeStamp uint64
		var timeStampSize int
		var tombstoneStart int64
		var tombstone bool
		var tombstoneSize int
		var tombstoneBytes []byte
		if typeRecord == 0 || typeRecord == 1 {

			timeStampStart = typeStart + int64(typeSize)
			timeStamp, timeStampSize = binary.Uvarint(block[timeStampStart:])

			tombstoneBytes = make([]byte, 1)
			tombstoneStart = timeStampStart + int64(timeStampSize)
			tombstoneSize = 1
			copy(tombstoneBytes, block[tombstoneStart:tombstoneStart+int64(tombstoneSize)])
			if tombstoneBytes[0] == 1 {
				tombstone = true
			} else {
				tombstone = false
			}
		}

		var keySizeStart int64
		var keySize uint64
		var keySizeSize int
		if typeRecord == 0 || typeRecord == 1 {
			keySizeStart = tombstoneStart + int64(tombstoneSize)
		} else {
			keySizeStart = typeStart + int64(typeSize)
		}
		keySize, keySizeSize = binary.Uvarint(block[keySizeStart:])

		var valueSizeStart int64
		var valueSize uint64
		var valueSizeSize int
		var keyStart int64
		if forSstable && !tombstone { // ako je obrisan, onda se valueSize ne cita
			valueSizeStart = keySizeStart + int64(keySizeSize)
			valueSize, valueSizeSize = binary.Uvarint(block[valueSizeStart:])
			keyStart = valueSizeStart + int64(valueSizeSize)
		} else {
			keyStart = keySizeStart + int64(keySizeSize)
		}

		keyBytes := make([]byte, keySize)
		copy(keyBytes, block[keyStart:keyStart+int64(keySize)])
		key := string(keyBytes)

		var valueStart int64
		var valueBytes []byte
		if forSstable && !tombstone {
			valueStart = keyStart + int64(keySize)
			valueBytes = make([]byte, valueSize)
			copy(valueBytes, block[valueStart:valueStart+int64(valueSize)])
		}

		readCRCData := make([]byte, 0) //provera CRC32 vrednosti

		readCRCData = append(readCRCData, block[sizeStart:sizeStart+int64(sizeSize)]...)
		readCRCData = append(readCRCData, typeBytes...)
		if typeRecord == 0 || typeRecord == 1 {
			readCRCData = append(readCRCData, block[timeStampStart:timeStampStart+int64(timeStampSize)]...)
			readCRCData = append(readCRCData, tombstoneBytes...)
		}
		readCRCData = append(readCRCData, block[keySizeStart:keySizeStart+int64(keySizeSize)]...)
		if forSstable && !tombstone {
			readCRCData = append(readCRCData, block[valueSizeStart:valueSizeStart+int64(valueSizeSize)]...)
		}
		readCRCData = append(readCRCData, keyBytes...)
		if forSstable && !tombstone {
			readCRCData = append(readCRCData, valueBytes...)
		}

		readCRC := CRC32(readCRCData)

		if readCRC != uint32(crc) && !forSstable {
			log.Print("CRC32 vrednost nije ispravna, zapis je korumpiran")
			log.Print("Ovaj zapis je preskocen")
			i += int64(size) //preskacem ovaj zapis
			continue
		}

		newWalRecord := WalRecord{
			CRC:       uint32(crc),
			Size:      uint32(size),
			Type:      typeRecord,
			Timestamp: timeStamp,
			Tombstone: tombstone,
			KeySize:   keySize,
			ValueSize: valueSize,
			Key:       key,
			Value:     valueBytes,
		}

		records = append(records, newWalRecord)

		if forSstable && tombstone {
			size = uint64(crcSize) +
				uint64(sizeSize) +
				uint64(typeSize) +
				uint64(timeStampSize) +
				uint64(tombstoneSize) +
				uint64(keySizeSize) +
				keySize
		}

		i += int64(size)
	}

	return records

}