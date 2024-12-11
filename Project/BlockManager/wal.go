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

const ( //konstante koje predstavljaju pocetne pozicije svakog dela zapisa
	CRC_SIZE        = 4
	SIZE_SIZE       = 4
	TYPE_SIZE       = 1
	TIMESTAMP_SIZE  = 8
	TOMBSTONE_SIZE  = 1
	KEY_SIZE_SIZE   = 8
	VALUE_SIZE_SIZE = 8

	CRC_START        = 0
	SIZE_START       = CRC_START + CRC_SIZE
	TYPE_START       = SIZE_START + SIZE_SIZE
	TIMESTAMP_START  = TYPE_START + TYPE_SIZE
	TOMBSTONE_START  = TIMESTAMP_START + TIMESTAMP_SIZE
	KEY_SIZE_START   = TOMBSTONE_START + TOMBSTONE_SIZE
	VALUE_SIZE_START = KEY_SIZE_START + KEY_SIZE_SIZE
	KEY_START        = VALUE_SIZE_START + VALUE_SIZE_SIZE
)

func CRC32(data []byte) uint32 { //funkcija koja racuna CRC32 vrednost za zadati niz bajtova
	return crc32.ChecksumIEEE(data)
}

func CreateBinaryRecordsWithFragmentation(tombstone bool, key string, value []byte, blockSize int) [][]byte { //funkcija koja kreira zapise sa fragmentacijom
	binaryRecords := [][]byte{}

	keySize := uint64(len(key))
	valueSize := uint64(len(value))

	size := CRC_SIZE + SIZE_SIZE + TYPE_SIZE + TIMESTAMP_SIZE + TOMBSTONE_SIZE + KEY_SIZE_SIZE + VALUE_SIZE_SIZE + keySize + valueSize

	if 3*uint64(blockSize) < size {
		log.Fatal("Key or value size is bigger than 3*block size, fragmentation is not possible")
	}

	constMaxSize := uint64(blockSize) - CRC_SIZE - SIZE_SIZE - TYPE_SIZE - TIMESTAMP_SIZE - TOMBSTONE_SIZE - KEY_SIZE_SIZE - VALUE_SIZE_SIZE

	if keySize > constMaxSize || valueSize > constMaxSize {
		log.Fatal("Key or value size is bigger than block size")
	}

	if keySize+valueSize <= constMaxSize {
		walRecord := CreateWalRecord(tombstone, key, value, 0)
		binaryRecord := walRecord.CreateBinaryRecord()
		binaryRecords = append(binaryRecords, binaryRecord)
	} else {
		fragmentedConstMaxSize := blockSize - CRC_SIZE - SIZE_SIZE - TYPE_SIZE - KEY_SIZE_SIZE - VALUE_SIZE_SIZE

		if keySize > constMaxSize { //ako je kljuc veci od jednog pocetnog bloka
			newKeySize := constMaxSize

			newValueSize := 0

			newKey := key[:newKeySize]
			newValue := []byte{}

			firstWalRecord := CreateWalRecord(tombstone, newKey, newValue, 1)
			firstBinaryRecord := firstWalRecord.CreateBinaryRecord()

			binaryRecords = append(binaryRecords, firstBinaryRecord)

			newKeySize = keySize - constMaxSize

			if newKeySize > uint64(fragmentedConstMaxSize) { //ako je i dalje ostatak kljuca veci od fragmentisanog bloka
				newKeySize = uint64(fragmentedConstMaxSize)

				newKey = key[constMaxSize : constMaxSize+newKeySize]

				secondWalRecord := CreateFragmentedWalRecord(newKey, newValue, 2)
				secondBinaryrecord := secondWalRecord.CreateFragmentedBinaryRecord()

				binaryRecords = append(binaryRecords, secondBinaryrecord)

				newKey = key[constMaxSize+uint64(fragmentedConstMaxSize):]

				newValue = value

				thirdWalRecord := CreateFragmentedWalRecord(newKey, newValue, 3)
				thirdBinaryRecord := thirdWalRecord.CreateFragmentedBinaryRecord()

				binaryRecords = append(binaryRecords, thirdBinaryRecord)
			} else if int(valueSize) > fragmentedConstMaxSize { //ako je vrednost veca od jednog fragmentisanog bloka
				newKey = key[constMaxSize:]
				newKeySize = keySize - constMaxSize

				newValueSize = fragmentedConstMaxSize - int(newKeySize)

				newValue = value[:newValueSize]

				secondWalRecord := CreateFragmentedWalRecord(newKey, newValue, 2)
				secondBinaryRecord := secondWalRecord.CreateFragmentedBinaryRecord()

				binaryRecords = append(binaryRecords, secondBinaryRecord)

				newValue = value[fragmentedConstMaxSize-int(newKeySize):]

				newKey = ""

				thirdWalRecord := CreateFragmentedWalRecord(newKey, newValue, 3)
				thirdBinaryRecord := thirdWalRecord.CreateFragmentedBinaryRecord()

				binaryRecords = append(binaryRecords, thirdBinaryRecord)
			} else { //ako je i kljuc i vrednost manji od fragmentisanog bloka nece biti treceg zapisa
				newKey = key[constMaxSize:]
				newValue = value

				secondWalRecord := CreateFragmentedWalRecord(newKey, newValue, 2)
				secondBinaryRecord := secondWalRecord.CreateFragmentedBinaryRecord()

				binaryRecords = append(binaryRecords, secondBinaryRecord)
			}
		} else { //ako je kljuc manji od pocetnog bloka a vrednost veca od pocetnog bloka
			newKeySize := keySize

			newValueSize := int(constMaxSize) - int(newKeySize)

			newKey := key

			newValue := value[:newValueSize]

			firstWalRecord := CreateWalRecord(tombstone, newKey, newValue, 1)
			firstBinaryRecord := firstWalRecord.CreateBinaryRecord()

			binaryRecords = append(binaryRecords, firstBinaryRecord)

			if valueSize-uint64(newValueSize) > uint64(fragmentedConstMaxSize) { //ako je ostatak vrednosti veci od fragmentisanog bloka
				newValue = value[constMaxSize:]

				newKey = ""

				secondWalRecord := CreateFragmentedWalRecord(newKey, newValue, 2)
				secondBinaryRecord := secondWalRecord.CreateFragmentedBinaryRecord()

				binaryRecords = append(binaryRecords, secondBinaryRecord)

				newValue = value[constMaxSize:]

				thirdWalRecord := CreateFragmentedWalRecord(newKey, newValue, 3)
				thirdBinaryRecord := thirdWalRecord.CreateFragmentedBinaryRecord()

				binaryRecords = append(binaryRecords, thirdBinaryRecord)

			} else { //ako je ostatak vrednosti manji od fragmentisanog bloka nece biti treceg zapisa
				newValue = value[newValueSize:]

				newKey = ""

				secondWalRecord := CreateFragmentedWalRecord(newKey, newValue, 2)
				secondBinaryRecord := secondWalRecord.CreateFragmentedBinaryRecord()

				binaryRecords = append(binaryRecords, secondBinaryRecord)
			}
		}
	}

	return binaryRecords
}

func CreateWalRecord(tombstone bool, key string, value []byte, fragment int) WalRecord { //funkcija koja kreira objekat WalRecord, tj. objekat za zapis u WAL-u

	crcData := make([]byte, 0)

	keySize := uint64(len(key))
	valueSize := uint64(len(value))

	size := CRC_SIZE + SIZE_SIZE + TYPE_SIZE + TIMESTAMP_SIZE + TOMBSTONE_SIZE + KEY_SIZE_SIZE + VALUE_SIZE_SIZE + keySize + valueSize

	timeStamp := time.Now().Unix()

	sizeBytes := make([]byte, SIZE_SIZE)
	binary.LittleEndian.PutUint32(sizeBytes, uint32(size))
	typeBytes := make([]byte, TYPE_SIZE)
	typeBytes[0] = byte(fragment)
	timeStampBytes := make([]byte, TIMESTAMP_SIZE)
	binary.LittleEndian.PutUint64(timeStampBytes, uint64(timeStamp))
	tombstoneBytes := make([]byte, TOMBSTONE_SIZE)
	if tombstone {
		tombstoneBytes[0] = 1
	} else {
		tombstoneBytes[0] = 0
	}
	keySizeBytes := make([]byte, KEY_SIZE_SIZE)
	binary.LittleEndian.PutUint64(keySizeBytes, keySize)
	valueSizeBytes := make([]byte, VALUE_SIZE_SIZE)
	binary.LittleEndian.PutUint64(valueSizeBytes, valueSize)
	keyBytes := []byte(key)

	crcData = append(crcData, sizeBytes...)
	crcData = append(crcData, typeBytes...)
	crcData = append(crcData, timeStampBytes...)
	crcData = append(crcData, tombstoneBytes...)
	crcData = append(crcData, keySizeBytes...)
	crcData = append(crcData, valueSizeBytes...)
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

func (walRecord *WalRecord) CreateBinaryRecord() []byte { //funkcija koja pretvara zapis u niz bajtova

	bytesRecord := []byte{}

	crcBytes := make([]byte, 4)
	binary.LittleEndian.PutUint32(crcBytes, walRecord.CRC)
	bytesRecord = append(bytesRecord, crcBytes...)

	sizeBytes := make([]byte, 4)
	binary.LittleEndian.PutUint32(sizeBytes, walRecord.Size)
	bytesRecord = append(bytesRecord, sizeBytes...)

	typeBytes := []byte{byte(walRecord.Type)}
	bytesRecord = append(bytesRecord, typeBytes...)

	timeStampBytes := make([]byte, 8)
	binary.LittleEndian.PutUint64(timeStampBytes, walRecord.Timestamp)
	bytesRecord = append(bytesRecord, timeStampBytes...)

	tombstoneBytes := make([]byte, 1)
	if walRecord.Tombstone {
		tombstoneBytes[0] = 1
	} else {
		tombstoneBytes[0] = 0
	}
	bytesRecord = append(bytesRecord, tombstoneBytes...)

	keySizeBytes := make([]byte, 8)
	binary.LittleEndian.PutUint64(keySizeBytes, walRecord.KeySize)
	bytesRecord = append(bytesRecord, keySizeBytes...)

	valueSizeBytes := make([]byte, 8)
	binary.LittleEndian.PutUint64(valueSizeBytes, walRecord.ValueSize)
	bytesRecord = append(bytesRecord, valueSizeBytes...)

	keyBytes := []byte(walRecord.Key)
	bytesRecord = append(bytesRecord, keyBytes...)

	bytesRecord = append(bytesRecord, walRecord.Value...)

	return bytesRecord
}

func CreateFragmentedWalRecord(key string, value []byte, fragment int) WalRecord { //funkcija koja kreira fragmentisani zapis
	crcData := make([]byte, 0)

	keySize := uint64(len(key))
	valueSize := uint64(len(value))

	size := CRC_SIZE + SIZE_SIZE + TYPE_SIZE + KEY_SIZE_SIZE + VALUE_SIZE_SIZE + keySize + valueSize

	typeRecord := uint8(fragment)

	sizeBytes := make([]byte, SIZE_SIZE)
	binary.LittleEndian.PutUint32(sizeBytes, uint32(size))
	typeBytes := make([]byte, TYPE_SIZE)
	typeBytes[0] = byte(typeRecord)
	keySizeBytes := make([]byte, KEY_SIZE_SIZE)
	binary.LittleEndian.PutUint64(keySizeBytes, keySize)
	valueSizeBytes := make([]byte, VALUE_SIZE_SIZE)
	binary.LittleEndian.PutUint64(valueSizeBytes, valueSize)
	keyBytes := []byte(key)

	crcData = append(crcData, sizeBytes...)
	crcData = append(crcData, typeBytes...)
	crcData = append(crcData, keySizeBytes...)
	crcData = append(crcData, valueSizeBytes...)
	crcData = append(crcData, keyBytes...)
	crcData = append(crcData, value...)

	crc := CRC32(crcData)

	return WalRecord{
		CRC:       crc,
		Size:      uint32(size),
		Type:      typeRecord,
		Timestamp: 0,
		Tombstone: false,
		KeySize:   keySize,
		ValueSize: valueSize,
		Key:       key,
		Value:     value,
	}
}

func (walRecord *WalRecord) CreateFragmentedBinaryRecord() []byte { //funkcija koja pravi niz bajtova od fragmentisanog zapisa
	bytesRecord := []byte{}

	crcBytes := make([]byte, 4)
	binary.LittleEndian.PutUint32(crcBytes, walRecord.CRC)
	bytesRecord = append(bytesRecord, crcBytes...)

	sizeBytes := make([]byte, 4)
	binary.LittleEndian.PutUint32(sizeBytes, walRecord.Size)
	bytesRecord = append(bytesRecord, sizeBytes...)

	typeBytes := []byte{byte(walRecord.Type)}
	bytesRecord = append(bytesRecord, typeBytes...)

	keySizeBytes := make([]byte, 8)
	binary.LittleEndian.PutUint64(keySizeBytes, walRecord.KeySize)
	bytesRecord = append(bytesRecord, keySizeBytes...)

	valueSizeBytes := make([]byte, 8)
	binary.LittleEndian.PutUint64(valueSizeBytes, walRecord.ValueSize)
	bytesRecord = append(bytesRecord, valueSizeBytes...)

	keyBytes := []byte(walRecord.Key)
	bytesRecord = append(bytesRecord, keyBytes...)

	bytesRecord = append(bytesRecord, walRecord.Value...)

	return bytesRecord
}

func ReadAllRecords(block []byte, blockSize int64) []WalRecord { //funkcija koja cita sve zapise iz jednog bloka
	records := []WalRecord{}

	i := int64(0)
	for i < blockSize {
		if i+int64(CRC_SIZE+SIZE_SIZE+TYPE_SIZE+TIMESTAMP_SIZE+TOMBSTONE_SIZE+KEY_SIZE_SIZE+VALUE_SIZE_SIZE) > blockSize { //provera da nije padding
			break
		}
		crcBytes := make([]byte, 4)
		copy(crcBytes, block[i:i+CRC_SIZE])

		crc := binary.LittleEndian.Uint32(crcBytes)

		if crc == 0 { //druga provera da nije padding
			break
		}

		sizeBytes := make([]byte, 4)
		copy(sizeBytes, block[i+SIZE_START:i+SIZE_START+SIZE_SIZE])

		size := binary.LittleEndian.Uint32(sizeBytes)

		typeBytes := make([]byte, 1)
		copy(typeBytes, block[i+TYPE_START:i+TYPE_START+TYPE_SIZE])

		typeRecord := typeBytes[0]

		var timeStamp uint64
		var tombstone bool
		var tombstoneBytes []byte
		var timeStampBytes []byte
		NEW_KEY_SIZE_START := TYPE_START + TYPE_SIZE
		NEW_VALUE_SIZE_START := NEW_KEY_SIZE_START + KEY_SIZE_SIZE
		NEW_KEY_START := NEW_VALUE_SIZE_START + VALUE_SIZE_SIZE
		if typeRecord == 0 || typeRecord == 1 {

			timeStampBytes = make([]byte, 8)
			copy(timeStampBytes, block[i+TIMESTAMP_START:i+TIMESTAMP_START+TIMESTAMP_SIZE])

			timeStamp = binary.LittleEndian.Uint64(timeStampBytes)

			tombstoneBytes = make([]byte, 1)
			copy(tombstoneBytes, block[i+TOMBSTONE_START:i+TOMBSTONE_START+TOMBSTONE_SIZE])

			if tombstoneBytes[0] == 1 {
				tombstone = true
			} else {
				tombstone = false
			}

		}

		keySizeBytes := make([]byte, 8)
		if typeRecord == 0 || typeRecord == 1 {
			copy(keySizeBytes, block[i+KEY_SIZE_START:i+KEY_SIZE_START+KEY_SIZE_SIZE])
		} else {
			copy(keySizeBytes, block[i+int64(NEW_KEY_SIZE_START):i+int64(NEW_KEY_SIZE_START)+KEY_SIZE_SIZE])
		}

		keySize := binary.LittleEndian.Uint64(keySizeBytes)

		valueSizeBytes := make([]byte, 8)
		if typeRecord == 0 || typeRecord == 1 {
			copy(valueSizeBytes, block[i+VALUE_SIZE_START:i+VALUE_SIZE_START+VALUE_SIZE_SIZE])
		} else {
			copy(valueSizeBytes, block[i+int64(NEW_VALUE_SIZE_START):i+int64(NEW_VALUE_SIZE_START)+VALUE_SIZE_SIZE])
		}

		valueSize := binary.LittleEndian.Uint64(valueSizeBytes)

		keyBytes := make([]byte, keySize)
		if typeRecord == 0 || typeRecord == 1 {
			copy(keyBytes, block[i+KEY_START:i+KEY_START+int64(keySize)])
		} else {

			copy(keyBytes, block[i+int64(NEW_KEY_START):i+int64(NEW_KEY_START)+int64(keySize)])
		}

		key := string(keyBytes)

		valueBytes := make([]byte, valueSize)
		if typeRecord == 0 || typeRecord == 1 {
			copy(valueBytes, block[i+KEY_START+int64(keySize):i+KEY_START+int64(keySize)+int64(valueSize)])
		} else {
			copy(valueBytes, block[i+int64(NEW_KEY_START)+int64(keySize):i+int64(NEW_KEY_START)+int64(keySize)+int64(valueSize)])
		}

		readCRCData := make([]byte, 0) //provera CRC32 vrednosti

		readCRCData = append(readCRCData, sizeBytes...)
		readCRCData = append(readCRCData, typeBytes...)

		if typeRecord == 0 || typeRecord == 1 {
			readCRCData = append(readCRCData, timeStampBytes...)
			readCRCData = append(readCRCData, tombstoneBytes...)
		}
		readCRCData = append(readCRCData, keySizeBytes...)
		readCRCData = append(readCRCData, valueSizeBytes...)
		readCRCData = append(readCRCData, keyBytes...)
		readCRCData = append(readCRCData, valueBytes...)

		readCRC := CRC32(readCRCData)

		if readCRC != crc {
			log.Fatal("CRC32 check failed")
		}

		newWalRecord := WalRecord{
			CRC:       crc,
			Size:      size,
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
