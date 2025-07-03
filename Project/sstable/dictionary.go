package sstable

import (
	"encoding/binary"
	"fmt"
	"project/BlockManager"
)

type Dictionary struct { // Struktura mape koja sluzi za kompresiju kljuceva u SSTabeli
	keysToVals map[string]int // mapira kljuceve na njihove indekse
	size       int            // broj kljuceva u mapi
	filename   string	 // putanja do fajla u kom se cuva mapa
}

const KEY_SIZE_SIZE = 8

func NewDictionary() *Dictionary {
	return &Dictionary{
		keysToVals: make(map[string]int),
		size:       0,
		filename:   "sstable/dict/dictionary.bin", // putanja do fajla u kom se cuva mapa
	}
}

func (d *Dictionary) Delete(key string) { // brise kljuc iz mape
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
	val, exist := d.keysToVals[key] // proverava da li kljuc vec postoji
	if !exist {                     // ako ne postoji, dodaje ga u mapu
		d.keysToVals[key] = d.size
		d.size++          // povecava broj kljuceva u mapi
		return fmt.Sprintf("%d", d.size - 1) // vraca novu vrednost
	}
	return fmt.Sprintf("%d", val) // ako kljuc vec postoji
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
		record = append(record, keyBytes...) // zatim dodajemo kljuc
		record = append(record, valBytes...) // na kraju dodajemo vrednost
		
		data = append(data, record...)
	}
	return data
}


func (d *Dictionary) Deserialize(blockManager *BlockManager.BlockManager) { // ucitava mapu iz fajla
	data,err := blockManager.ReadDictionary(d.filename)
	if err != nil {
		panic("Failed to read dictionary: " + err.Error())
	}
	if len(data) == 0 { // ako je mapa prazna, vraca se
		d.size = 0
		d.keysToVals = make(map[string]int)
		return
	}

	for i:= 0; i < len(data); {
		kSize := binary.LittleEndian.Uint64(data[i : i+KEY_SIZE_SIZE]) // cita velicinu kljuca
		i += KEY_SIZE_SIZE
		key := string(data[i : i+int(kSize)]) // cita kljuc
		i += int(kSize)
		val := binary.LittleEndian.Uint32(data[i : i+4]) // cita vrednost
		i += 4

		d.keysToVals[key] = int(val) // dodaje kljuc i vrednost u mapu
		if d.size <= int(val) {      // ako je nova vrednost veca od broja kljuceva, povecava broj kljuceva
			d.size = int(val) + 1
		}
	}
	if len(data) > 0 {
		d.size-- // smanjuje broj kljuceva za 1 jer je poslednji kljuc dodat kao poslednji
	}
	if d.size < 0 {
		d.size = 0 // ako je broj kljuceva manji od 0, postavlja ga na 0
	}
}