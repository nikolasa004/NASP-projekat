package BlockManager

type BufferPool struct { //struktura koja predstavlja bafer pool
	maxSize     int               //maksimalna velicina bafer pool-a
	Buffer      map[string][]byte //mapa koja cuva kljuc-vrednost parove
	lruList     []string          //lista koja cuva kljuceve u redosledu poslednjeg pristupa
	dirtyBlocks map[string]bool   //mapa koja cuva informaciju da li je zapis u bafer pool-u promenjen, samo promenjeni se flush-uju na wal
}

func NewBufferPool(maxSize int) BufferPool { //konstruktor za BufferPool
	return BufferPool{
		maxSize:     maxSize,
		Buffer:      make(map[string][]byte, maxSize),
		lruList:     make([]string, 0),
		dirtyBlocks: make(map[string]bool),
	}
}

func (bufferPool *BufferPool) Check(key string) bool { //funkcija koja proverava da li se zadati kljuc nalazi u bafer pool-u
	_, exists := bufferPool.Buffer[key]
	return exists
}

func (bufferPool *BufferPool) updateLRU(key string) { //funkcija koja azurira listu LRU
	for i, value := range bufferPool.lruList {
		if value == key {
			bufferPool.lruList = append(bufferPool.lruList[:i], bufferPool.lruList[i+1:]...)
			break
		}
	}

	bufferPool.lruList = append(bufferPool.lruList, key)
}

func (bufferPool *BufferPool) Get(key string) []byte { //funkcija koja vraca vrednost iz bafer pool-a za zadati kljuc ili nil ako kljuc ne postoji
	value, exists := bufferPool.Buffer[key]

	if exists {
		bufferPool.updateLRU(key)
		return value
	}
	return nil
}

func (bufferPool *BufferPool) deleteBlock(key string) { //funkcija koja brise zapis iz bafer pool-a
	delete(bufferPool.Buffer, key)
	delete(bufferPool.dirtyBlocks, key)
	for i, value := range bufferPool.lruList {
		if value == key {
			bufferPool.lruList = append(bufferPool.lruList[:i], bufferPool.lruList[i+1:]...)
			break
		}
	}
}

func (bufferPool *BufferPool) Put(key string, value []byte, newBlock bool) []byte { //funkcija koja dodaje novi zapis u bafer pool
	var blockForFlush []byte
	if len(bufferPool.Buffer) == bufferPool.maxSize {
		if bufferPool.dirtyBlocks[bufferPool.lruList[0]] {
			blockForFlush = bufferPool.Buffer[bufferPool.lruList[0]]
			bufferPool.dirtyBlocks[bufferPool.lruList[0]] = false
		}
		bufferPool.deleteBlock(bufferPool.lruList[0])
	}

	bufferPool.Buffer[key] = value
	if newBlock {
		bufferPool.dirtyBlocks[key] = true
	} else {
		bufferPool.dirtyBlocks[key] = false
	}
	bufferPool.lruList = append(bufferPool.lruList, key)

	return blockForFlush
}

func (bufferPool *BufferPool) Update(key string, value []byte) { //funkcija koja azurira postojeci zapis u bafer pool-u
	bufferPool.Buffer[key] = value
	bufferPool.dirtyBlocks[key] = true
	bufferPool.updateLRU(key)
}

func (bufferPool *BufferPool) Flush() map[string][]byte { //funkcija koja vraca sve promenjene zapise iz bafer pool-a
	flushedBlocks := make(map[string][]byte)
	for key, value := range bufferPool.Buffer {
		if bufferPool.dirtyBlocks[key] {
			flushedBlocks[key] = value
			bufferPool.dirtyBlocks[key] = false
		}
	}
	return flushedBlocks
}
