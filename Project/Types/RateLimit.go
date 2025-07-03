package Types

import (
	"bytes"
	"encoding/binary"
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

func (r *RateLimitState) CanConsumeToken() bool {
	now := time.Now().Unix()
	elapsed := now - r.StartTime

	//dodaje token ako je prosao interval
	//	if elapsed >= r.Interval {
	//		newTokens := uint32(elapsed / r.Interval)
	//		r.Count += newTokens
	//		if r.Count > r.Capacity {
	//			r.Count = r.Capacity
	//		}
	//		r.StartTime = now
	//	}
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
