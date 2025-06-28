package Types

import (
	"bytes"
	"encoding/binary"
	"math"
)

type CountMinSketch struct {
	Key           string
	K             uint32 //duzina
	M             uint32 //sirina
	Matrix        [][]uint32
	HashFunctions []HashWithSeed
}

func (cms CountMinSketch) Add(new []byte) {
	for i, hashFunction := range cms.HashFunctions {
		index := hashFunction.Hash(new) % uint64(cms.M)
		cms.Matrix[i][index]++
	}
}

func (cms *CountMinSketch) Count(find []byte) uint32 {
	minimum := uint32(math.MaxUint32)
	for i, hashFunction := range cms.HashFunctions {
		index := hashFunction.Hash(find) % uint64(cms.M)
		if cms.Matrix[i][index] < minimum {
			minimum = cms.Matrix[i][index]
		}
	}
	return minimum
}

func NewCountMinSketch(k, m uint32, keyName string) *CountMinSketch {
	k1 := uint(k)
	hashFunctions := CreateHashFunctions(k1)
	matrix := [][]uint32{}
	for i := 0; i < int(k); i++ {
		matrix = append(matrix, []uint32{})
	}
	for i := range matrix {
		matrix[i] = []uint32{}
		for j := 0; j < int(m); j++ {
			matrix[i] = append(matrix[i], 0)
		}
	}
	return &CountMinSketch{
		Key:           keyName,
		K:             k,
		M:             m,
		Matrix:        matrix,
		HashFunctions: hashFunctions,
	}
}

func Deserialize(data []byte) (*CountMinSketch, error) {
	buf := bytes.NewReader(data)
	var k, m uint32
	if err := binary.Read(buf, binary.BigEndian, &k); err != nil {
		return nil, err
	}
	if err := binary.Read(buf, binary.BigEndian, &m); err != nil {
		return nil, err
	}

	//Kljuc
	var keyLen uint32
	if err := binary.Read(buf, binary.BigEndian, &keyLen); err != nil {
		return nil, err
	}
	keyBytes := make([]byte, keyLen)
	if _, err := buf.Read(keyBytes); err != nil {
		return nil, err
	}

	//matrica
	matrix := make([][]uint32, k)
	for i := uint32(0); i < k; i++ {
		matrix[i] = make([]uint32, m)
		for j := uint32(0); j < m; j++ {
			if err := binary.Read(buf, binary.BigEndian, &matrix[i][j]); err != nil {
				return nil, err
			}
		}
	}

	//Hash fukncije
	hashFnc := make([]HashWithSeed, k)
	for i := uint32(0); i < k; i++ {
		var seedLen uint32
		if err := binary.Read(buf, binary.BigEndian, &seedLen); err != nil {
			return nil, err
		}
		seed := make([]byte, seedLen)
		if _, err := buf.Read(seed); err != nil {
			return nil, err
		}
		hashFnc[i] = HashWithSeed{Seed: seed}
	}
	return &CountMinSketch{
		Key:           string(keyBytes),
		K:             k,
		M:             m,
		Matrix:        matrix,
		HashFunctions: hashFnc,
	}, nil
}

func (cms *CountMinSketch) Serialize() ([]byte, error) {
	buf := new(bytes.Buffer)

	//k i m
	binary.Write(buf, binary.BigEndian, cms.K)
	binary.Write(buf, binary.BigEndian, cms.M)

	//ključ i dužina ključa
	keyBytes := []byte(cms.Key)
	binary.Write(buf, binary.BigEndian, uint32(len(keyBytes)))
	buf.Write(keyBytes)

	//matrica
	for i := uint32(0); i < cms.K; i++ {
		for j := uint32(0); j < cms.M; j++ {
			binary.Write(buf, binary.BigEndian, cms.Matrix[i][j])
		}
	}
	//hash fukncije, dužina i bajtovi
	for _, h := range cms.HashFunctions {
		binary.Write(buf, binary.BigEndian, uint32(len(h.Seed)))
		buf.Write(h.Seed)
	}
	return buf.Bytes(), nil
}
