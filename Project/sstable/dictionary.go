package sstable

type Dictionary struct { // Struktura mape koja sluzi za kompresiju kljuceva u SSTabeli
	keysToVals map[string]int // mapira kljuceve na njihove indekse
	size       int            // broj kljuceva u mapi
}

func NewDictionary() *Dictionary {
	return &Dictionary{
		keysToVals: make(map[string]int),
		size:       0,
	}
}

func (d *Dictionary) Get(key string) (int, bool) { // dobavlja vrednost za dati kljuc ako postoji
	val, exists := d.keysToVals[key]
	return val, exists
}

func (d *Dictionary) Put(key string) int { // dodaje kljuc u mapu i vraca njegovu mapiranu vrednost
	val, exist := d.keysToVals[key] // proverava da li kljuc vec postoji
	if !exist {                     // ako ne postoji, dodaje ga u mapu
		d.keysToVals[key] = d.size
		d.size++          // povecava broj kljuceva u mapi
		return d.size - 1 // vraca novu vrednost
	}
	return val // ako kljuc vec postoji
}
