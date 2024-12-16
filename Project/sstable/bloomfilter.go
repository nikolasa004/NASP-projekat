package main

import (
	"crypto/md5"
	"encoding/binary"
	"encoding/gob"
	"math"
	"os"
	"time"
)

type BloomFilter struct {
	BitSet []byte
	K      uint
	M      uint
	Hashes []HashWithSeed
}

func NewBloomFilter(n int, p float64) *BloomFilter {
	m := CalculateM(n, p)
	k := CalculateK(n, m)

	bf := &BloomFilter{
		BitSet: make([]byte, (m+7)/8),
		K:      k,
		M:      m,
		Hashes: CreateHashFunctions(uint32(k)),
	}

	return bf
}

func (bf *BloomFilter) Clear() {
	bf.BitSet = nil
	bf.K = 0
	bf.M = 0
	bf.Hashes = nil
}

func (bf *BloomFilter) Add(item string) {
	for _, hashFunc := range bf.Hashes {
		index := int(hashFunc.Hash([]byte(item)) % uint64(bf.M))
		byteIndex := index / 8
		bitPosition := index % 8
		mask := byte(1 << bitPosition)
		bf.BitSet[byteIndex] |= mask
	}
	print("Add succesefully")
}

func (bf *BloomFilter) Check(item string) bool {
	for _, hashFunc := range bf.Hashes {
		index := int(hashFunc.Hash([]byte(item)) % uint64(bf.M))
		byteIndex := index / 8
		bitPosition := index % 8
		mask := byte(1 << bitPosition)

		if bf.BitSet[byteIndex]&mask == 0 {
			return false
		}
	}
	return true
}

func (bf *BloomFilter) Serialize(filename string) (error, uint64) {
	var offset uint64
	file, err := os.Open(filename)

	if err != nil {
		return err, offset
	}
	defer file.Close()

	encoder := gob.NewEncoder(file)
	err = encoder.Encode(bf)
	offset += uint64(len(bf.BitSet))

	if err != nil {
		return err, offset
	}

	return nil, offset
}

func Deserialize(filename string) (*BloomFilter, error) {
	file, err := os.Open(filename)

	if err != nil {
		return nil, err
	}
	defer file.Close()

	var bf BloomFilter
	decoder := gob.NewDecoder(file)
	err = decoder.Decode(&bf)

	if err != nil {
		return nil, err
	}

	return &bf, nil
}

type HashWithSeed struct {
	Seed []byte
}

func (h HashWithSeed) Hash(data []byte) uint64 {
	fn := md5.New()
	fn.Write(append(data, h.Seed...))
	return binary.BigEndian.Uint64(fn.Sum(nil))
}

func CreateHashFunctions(k uint32) []HashWithSeed {
	h := make([]HashWithSeed, k)
	ts := uint32(time.Now().Unix())
	for i := uint32(0); i < k; i++ {
		seed := make([]byte, 4)
		binary.BigEndian.PutUint32(seed, ts+i)
		hfn := HashWithSeed{Seed: seed}
		h[i] = hfn
	}
	return h
}

func CalculateM(expectedElements int, falsePositiveRate float64) uint {
	return uint(math.Ceil(float64(expectedElements) * math.Abs(math.Log(falsePositiveRate)) / math.Pow(math.Log(2), float64(2))))
}

func CalculateK(expectedElements int, m uint) uint {
	return uint(math.Ceil((float64(m) / float64(expectedElements)) * math.Log(2)))
}
