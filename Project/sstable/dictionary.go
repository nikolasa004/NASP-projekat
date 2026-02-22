package sstable

import (
	"encoding/binary"
	"fmt"
	"log"
	"os"
	"project/BlockManager"
	"project/Memtable"
)

type Dictionary struct { // Struktura mape koja sluzi za kompresiju kljuceva u SSTabeli
	keysToVals map[string]int
	size       int
	filename   string
}

const KEY_SIZE_SIZE = 8

func NewDictionary() *Dictionary {
	dictPath := "sstable/dict"
	os.MkdirAll(dictPath, os.ModePerm)

	_, err := os.ReadDir(dictPath)
	if err != nil {
		log.Print("[Error] Directory for compression dictionary not found, creating new one...")

		err = os.MkdirAll(dictPath, 0755)
		if err != nil {
			log.Print("[Error] Error creating directory for compression dictionary, cannot continue")
			log.Fatal("Error: ", err)
		}
	}


	return &Dictionary{
		keysToVals: make(map[string]int),
		size:       0,
		filename:   "sstable/dict/dictionary.bin",
	}
}

func (d *Dictionary) Delete(key string) {
	delete(d.keysToVals, key)
	if d.size > 0 {
		d.size--
	}
}

func (d *Dictionary) Get(key string) (string, bool) { // dobavlja vrednost za dati kljuc ako postoji
	val, exists := d.keysToVals[key]
	return fmt.Sprintf("%d", val), exists
}

func (d *Dictionary) Put(key string) string { // dodaje kljuc u mapu i vraca njegovu mapiranu vrednost
	val, exist := d.keysToVals[key]
	if !exist {
		d.keysToVals[key] = d.size
		d.size++ 
		return fmt.Sprintf("%d", d.size-1)
	}
	return fmt.Sprintf("%d", val)
}

func (d *Dictionary) Serialize() []byte {
	data := make([]byte, 0)

	for key, val := range d.keysToVals {
		record := make([]byte, 0)
		kSizeBytes := make([]byte, KEY_SIZE_SIZE)
		valBytes := make([]byte, 4)

		binary.LittleEndian.PutUint32(valBytes, uint32(val))
		keyBytes := []byte(key)
		binary.LittleEndian.PutUint64(kSizeBytes, uint64(len(keyBytes)))

		record = append(record, kSizeBytes...) // prvo dodajemo velicinu kljuca
		record = append(record, keyBytes...)   // zatim dodajemo kljuc
		record = append(record, valBytes...)   // na kraju dodajemo vrednost

		data = append(data, record...)
	}
	return data
}

func (d *Dictionary) Deserialize(blockManager *BlockManager.BlockManager) {
	data, err := blockManager.ReadDictionary(d.filename)
	if err != nil {
		panic("Failed to read dictionary: " + err.Error())
	}
	if len(data) == 0 {
		d.size = 0
		d.keysToVals = make(map[string]int)
		return
	}

	for i := 0; i < len(data); {
		kSize := binary.LittleEndian.Uint64(data[i : i+KEY_SIZE_SIZE])
		i += KEY_SIZE_SIZE
		key := string(data[i : i+int(kSize)])
		i += int(kSize)
		val := binary.LittleEndian.Uint32(data[i : i+4])
		i += 4

		d.keysToVals[key] = int(val)
		if d.size <= int(val) {
			d.size = int(val) + 1
		}
	}
	if d.size < 0 {
		d.size = 0
	}
}


func (d *Dictionary) StoreKeys(blockManager *BlockManager.BlockManager, records []*Memtable.MemtableRecord) []*Memtable.MemtableRecord {
	compressedRecords := make([]*Memtable.MemtableRecord, 0, len(records))

	// Kreiraj nove rekorde sa kompresovanim ključevima, ostavi originalne netaknute
	for _, record := range records {
		compressedRecord := &Memtable.MemtableRecord{
			Key:       d.Put(record.Key), // kompresovan ključ
			Value:     record.Value,
			Timestamp: record.Timestamp,
			Tombstone: record.Tombstone,
		}
		compressedRecords = append(compressedRecords, compressedRecord)
	}

	err := blockManager.WriteDictionary(d.Serialize(), d.filename)
	if err != nil {
		log.Fatalf("Failed to write dictionary: %v", err)
	}
	return compressedRecords
}

// FindOriginalKey vraca originalni ključ za dati kompresovani ključ (koji je string broja).
// Ako nije pronađen, vraca ulaz kao fallback.
func (d *Dictionary) FindOriginalKey(compressed string) string {
	for original, idx := range d.keysToVals {
		if fmt.Sprintf("%d", idx) == compressed {
			return original
		}
	}
	return compressed
}
