package Iterator

type ScanRecord struct {
	Key       string
	Value     []byte
	Tombstone bool
}

func NewScanRecord(key string, value []byte, tombstone bool) *ScanRecord {
	return &ScanRecord{
		Key:       key,
		Value:     value,
		Tombstone: tombstone,
	}
}

type InterfaceIterator interface {
	Next()                           // Prelazi na sledeći zapis
	GetValue() *ScanRecord           // Vraća trenutni rekord
	ChangePage(targetPage int)       // Prelazi na neku stranicu
	FinishPage()                     //Oznacava da je trenutna stranica zavrsena i prelazi na sledecu(postavlja oznaku u mapi)
	AlreadyRead(targetPage int) bool //Proverava da li je strana ranije procitana
}
