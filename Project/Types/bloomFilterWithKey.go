package Types

import (
	"bytes"
	"encoding/binary"

	//"fmt"
	"project/sstable"
)

type BloomFilterWithKey struct {
	Key string
	BF  *sstable.BloomFilter
}

func (bfk *BloomFilterWithKey) SerializeBFWK() ([]byte, error) {
	buf := new(bytes.Buffer)

	//kljuc
	keyBytes := []byte(bfk.Key)
	keyLen := uint32(len(keyBytes))
	if err := binary.Write(buf, binary.LittleEndian, keyLen); err != nil {
		return nil, err
	}
	if _, err := buf.Write(keyBytes); err != nil {
		return nil, err
	}

	// m  i k
	if err := binary.Write(buf, binary.LittleEndian, bfk.BF.M); err != nil {
		return nil, err
	}
	if err := binary.Write(buf, binary.LittleEndian, bfk.BF.K); err != nil {
		return nil, err
	}
	//bitovi
	bitSetLen := uint64(len(bfk.BF.BitSet))
	if err := binary.Write(buf, binary.LittleEndian, bitSetLen); err != nil {
		return nil, err
	}
	if _, err := buf.Write(bfk.BF.BitSet); err != nil {
		return nil, err
	}

	//hash funkcije
	hashCount := uint32(len(bfk.BF.Hashes))
	if err := binary.Write(buf, binary.LittleEndian, hashCount); err != nil {
		return nil, err

	}

	for _, h := range bfk.BF.Hashes {
		if err := binary.Write(buf, binary.LittleEndian, uint32(len(h.Seed))); err != nil {
			return nil, err
		}
		if _, err := buf.Write(h.Seed); err != nil {
			return nil, err
		}
	}
	return buf.Bytes(), nil
}

func DeserializeBFWK(data []byte) (*BloomFilterWithKey, error) {
	buf := bytes.NewReader(data)

	var keyLen uint32
	if err := binary.Read(buf, binary.LittleEndian, &keyLen); err != nil {
		return nil, err
	}
	keyBytes := make([]byte, keyLen)
	if _, err := buf.Read(keyBytes); err != nil {
		return nil, err
	}
	key := string(keyBytes)

	var M, K uint64
	if err := binary.Read(buf, binary.LittleEndian, &M); err != nil {
		return nil, err
	}
	if err := binary.Read(buf, binary.LittleEndian, &K); err != nil {
		return nil, err
	}
	var bitSetLen uint64
	if err := binary.Read(buf, binary.LittleEndian, &bitSetLen); err != nil {
		return nil, err
	}
	bitSet := make([]byte, bitSetLen)
	if _, err := buf.Read(bitSet); err != nil {
		return nil, err
	}
	var hashCount uint32
	if err := binary.Read(buf, binary.LittleEndian, &hashCount); err != nil {
		return nil, err
	}
	hashes := make([]sstable.HashWithSeed, hashCount)
	for i := uint32(0); i < hashCount; i++ {
		var seedLen uint32
		if err := binary.Read(buf, binary.LittleEndian, &seedLen); err != nil {
			return nil, err
		}
		seed := make([]byte, seedLen)
		if _, err := buf.Read(seed); err != nil {
			return nil, err
		}
		hashes[i] = sstable.HashWithSeed{Seed: seed}
	}

	return &BloomFilterWithKey{
		Key: key,
		BF: &sstable.BloomFilter{
			M:      M,
			K:      K,
			BitSet: bitSet,
			Hashes: hashes,
		},
	}, nil
}
