package Types

import (
	"bytes"
	"encoding/binary"
	"errors"
	"fmt"
	"math"
	"math/bits"
)

type HLL struct {
	Key string
	M   uint64  //broj registara m=2 na p-ti stepen
	P   uint8   // preciznost, borj bitova za indeksiranje
	Reg []uint8 //registri niz od m slotovo, tu se cuva max vodecih nula
	Hf  HashWithSeed
}

const (
	HLL_MIN_PRECISION = 4
	HLL_MAX_PRECISION = 16
)

func firstKbits(value, k uint64) uint64 {
	return value >> (64 - k)
}

//func trailingZeroBits(value uint64) int {
//	return bits.TrailingZeros64(value)
//}

func (hll *HLL) Estimate() float64 {
	sum := 0.0
	for _, val := range hll.Reg {
		sum += math.Pow(math.Pow(2.0, float64(val)), -1)
	}

	alpha := 0.7213 / (1.0 + 1.079/float64(hll.M))
	estimation := alpha * math.Pow(float64(hll.M), 2.0) / sum
	emptyRegs := hll.emptyCount()
	if estimation <= 2.5*float64(hll.M) { // do small range correction
		if emptyRegs > 0 {
			estimation = float64(hll.M) * math.Log(float64(hll.M)/float64(emptyRegs))
		}
	} else if estimation > 1/30.0*math.Pow(2.0, 32.0) { // do large range correction
		estimation = -math.Pow(2.0, 32.0) * math.Log(1.0-estimation/math.Pow(2.0, 32.0))
	}
	return estimation
}

func (hll *HLL) emptyCount() int {
	sum := 0
	for _, val := range hll.Reg {
		if val == 0 {
			sum++
		}
	}
	return sum
}

func newHLL(p uint8, key string) *HLL {
	for {
		if p < HLL_MIN_PRECISION || p > HLL_MAX_PRECISION {
			fmt.Println("P mora biti izmedju 4 i 16!")
		} else {
			break
		}
	}

	m := uint64(1) << p
	return &HLL{
		Key: key,
		M:   m,
		P:   p,
		Reg: make([]uint8, m),
		Hf:  CreateHashFunctions(1)[0], //jedna hash funkcija
	}
}

func (hll *HLL) Add(data []byte) {
	hash := hll.Hf.Hash(data)
	idx := firstKbits(hash, uint64(hll.P))     // prvih p bitova za indeks
	w := hash << uint64(hll.P)                 //ostali bitovi
	zeros := uint8(bits.LeadingZeros64(w)) + 1 //broj vodecih nula +1
	if zeros > hll.Reg[idx] {
		hll.Reg[idx] = zeros
	}
}

func (hll *HLL) SerializedHLL() ([]byte, error) {
	buf := new(bytes.Buffer)

	//kljuc
	keyBytes := []byte(hll.Key)
	keyLen := uint32(len(keyBytes))
	if err := binary.Write(buf, binary.LittleEndian, keyLen); err != nil {
		return nil, err
	}
	if _, err := buf.Write(keyBytes); err != nil {
		return nil, err
	}

	//M i P
	if err := binary.Write(buf, binary.LittleEndian, hll.M); err != nil {
		return nil, err
	}
	if err := binary.Write(buf, binary.LittleEndian, hll.P); err != nil {
		return nil, err
	}

	//reg
	regLen := uint64(len(hll.Reg))
	if err := binary.Write(buf, binary.LittleEndian, regLen); err != nil {
		return nil, err
	}
	if _, err := buf.Write(hll.Reg); err != nil {
		return nil, err
	}
	return buf.Bytes(), nil
}

func DesirializeHLL(data []byte) (*HLL, error) {
	buf := bytes.NewReader(data)

	//kljuc
	var keyLen uint32
	if err := binary.Read(buf, binary.LittleEndian, &keyLen); err != nil {
		return nil, err
	}
	keyBytes := make([]byte, keyLen)
	if _, err := buf.Read(keyBytes); err != nil {
		return nil, err
	}
	key := string(keyBytes)
	//M i P
	var M uint64
	var P uint8
	if err := binary.Read(buf, binary.LittleEndian, &M); err != nil {
		return nil, err
	}
	if err := binary.Read(buf, binary.LittleEndian, &P); err != nil {
		return nil, err
	}
	//reg

	var regLen uint64
	if err := binary.Read(buf, binary.LittleEndian, &regLen); err != nil {

		return nil, err
	}
	//var reg []uint8
	reg := make([]uint8, regLen)
	if _, err := buf.Read(reg); err != nil {
		return nil, err
	}

	hll := &HLL{
		Key: key,
		M:   M,
		P:   P,
		Reg: reg,
		Hf:  CreateHashFunctions(1)[0],
	}
	if uint64(len(hll.Reg)) != hll.M {
		return nil, errors.New("nevalidna duzina registra")
	}

	return hll, nil
}
