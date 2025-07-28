package RateLimit

import (
	"bytes"
	"encoding/binary"
	"fmt"
	"project/BlockManager"
	"project/Memtable"
	"project/sstable"
	"time"
)

type RateLimitState struct {
	StartTime int64
	Count     uint32
	Capacity  uint32 //max tokena
	Interval  int64  //sekunde
}

func SerializeRateLimit(state *RateLimitState) ([]byte, error) {
	buf := new(bytes.Buffer)
	err := binary.Write(buf, binary.LittleEndian, state.StartTime)
	if err != nil {
		return nil, err
	}
	err = binary.Write(buf, binary.LittleEndian, state.Count)
	if err != nil {
		return nil, err
	}
	err = binary.Write(buf, binary.LittleEndian, state.Capacity)
	if err != nil {
		return nil, err
	}
	err = binary.Write(buf, binary.LittleEndian, state.Interval)
	if err != nil {
		return nil, err
	}
	return buf.Bytes(), err
}

func DeserializeRateLimit(data []byte) (*RateLimitState, error) {
	buf := bytes.NewReader(data)
	var state RateLimitState
	err := binary.Read(buf, binary.LittleEndian, &state.StartTime)
	if err != nil {
		return nil, err
	}
	err = binary.Read(buf, binary.LittleEndian, &state.Count)
	if err != nil {
		return nil, err
	}
	err = binary.Read(buf, binary.LittleEndian, &state.Capacity)
	if err != nil {
		return nil, err
	}
	err = binary.Read(buf, binary.LittleEndian, &state.Interval)
	if err != nil {
		return nil, err
	}
	return &state, err
}

// kad prodje interval vrati sve tokene u bucket, skini jedan token, ako ima
func (r *RateLimitState) CanConsumeToken() bool {
	now := time.Now().UnixMicro()
	elapsed := now - r.StartTime

	if elapsed >= r.Interval {
		r.Count = r.Capacity
		r.StartTime = now
	}
	if r.Count == 0 {
		return false
	}
	r.Count--
	return true
}

// ide se kroz sistem i traži postojeći __ratelimit__ ključ, pojede token ako moze, vraća u sistem novi zapis sa jednim tokenom manje
func CheckRateLimit(tokenBucketKey string, memtable *Memtable.Memtable, blockManager *BlockManager.BlockManager, blockSize int, summarySampleRate int, oneFile bool, hasCompression bool, compressionDictionary *sstable.Dictionary) bool {
	var state *RateLimitState
	var err error

	if record, found := memtable.GetRecord(tokenBucketKey); found {
		state, err = DeserializeRateLimit([]byte(record.Value))
		if err != nil {
			fmt.Println("Greška pri čitanju token bucketa iz memtable-a:", err)
			return false
		}
	} else if SSrecord, found, _, _ := sstable.FindInSstables(tokenBucketKey, *blockManager, compressionDictionary); found {
		state, err = DeserializeRateLimit([]byte(SSrecord.Value))
		if err != nil {
			fmt.Println("Greška pri čitanju token bucketa iz SSTable-a:", err)
			return false
		}
	} else {
		fmt.Println("Nema token bucketa. Neispravna konfiguracija!")
		return false
	}

	if !state.CanConsumeToken() {
		fmt.Println("Prekoračenje tokena, pokušajte kasnije.")
		return false
	}

	serialized, err := SerializeRateLimit(state)
	if err != nil {
		fmt.Println("Greška pri serijalizaciji token bucketa:", err)
		return false
	}

	sysRecords := BlockManager.CreateBinaryRecordsWithFragmentation(false, tokenBucketKey, serialized, blockSize, false, 0)
	for _, r := range sysRecords {
		blockManager.PutRecord(r)
	}

	flushedSys, sysFlushed := memtable.PutRecord(false, tokenBucketKey, string(serialized), blockManager, len(blockManager.GetSegments())-1)
	if sysFlushed {
		sstable.WriteSstable(flushedSys, summarySampleRate, false, oneFile, blockManager, hasCompression, 0)
	}

	return true
}
