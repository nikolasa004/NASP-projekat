package Memtable

import (
	"math/rand"
)

type Node struct {
	key string
	value *MemtableRecord
	next []*Node
}

type SkipList struct {
	maxHeight int
	level int
	headNode *Node
	state string
	linkedSegment int
	lastBlockPosition int
}

func (s *SkipList) GetLastBlockPosition() int {
	return s.lastBlockPosition
}

func (s *SkipList) SetLastBlockPosition(position int) {
	s.lastBlockPosition = position
}


//clear skip list
func (s *SkipList) Clear() {
	headNext := make([]*Node, s.maxHeight)
	s.headNode = &Node{key: "",value:nil, next: headNext}
	s.level = 1
	s.linkedSegment = 0
	s.lastBlockPosition = 0
	s.state = "empty"
}

func (s *SkipList) GetLength() int {
	length := 0
	current := s.headNode
	for current.next[0] != nil {
		current = current.next[0]
		length++
	}
	return length
}

func (s *SkipList) GetLinkedSegment() int {
	return s.linkedSegment
}

func (s *SkipList) SetLinkedSegment(segment int) {
	s.linkedSegment = segment
}

func (s *SkipList) SetState(state string) {
	s.state = state
}

func (s *SkipList) GetState() string {
	return s.state
}

func NewSkipList(maxHeight int) *SkipList {
	headNext := make([]*Node, maxHeight)
	n := &Node{key: "",value:nil, next: headNext}

	return &SkipList{maxHeight: maxHeight, level:1, headNode: n, state: "empty", linkedSegment: 0, lastBlockPosition: 0}
}


func (skipList *SkipList) FindRecord(k string) *MemtableRecord {
	current := skipList.headNode
	for i := skipList.level - 1; i >= 0 ; i-- { 
		for current.next[i] != nil && current.next[i].key < k {
			current = current.next[i]
		} 
	}
	current = current.next[0]
	if current != nil && current.key == k {
		return current.value
	} else {
		return nil
	}
}



func (skipList *SkipList) AddRecord(record *MemtableRecord) {
	key := record.Key
	newLevel := skipList.roll()

	if newLevel > skipList.level {
		skipList.level = newLevel
	}
	update := make([]*Node, skipList.level)
	current := skipList.headNode
	for i := skipList.level - 1; i >= 0 ; i-- { 
		for current.next[i] != nil && current.next[i].key < key {
			current = current.next[i]
		} 
		update[i] = current
	}

	if current.next[0] != nil && current.next[0].key == key {
		current.next[0].value = record
		return
	}

	newNode := &Node{key:key,value:record,next:make([]*Node, newLevel)}

	for i := 0; i < newLevel; i++ {
		newNode.next[i] = update[i].next[i]
		update[i].next[i] = newNode
	}
}



func (s *SkipList) roll() int {
	level := 1
	// possible ret values from rand are 0 and 1
	// we stop shen we get a 0
	for ; rand.Int31n(2) == 1; level++ {
		if level >= s.maxHeight{
			return level
		}
	}
	return level
}


func (skipList *SkipList) Iterator() <-chan *MemtableRecord {
	ch := make(chan *MemtableRecord)
	go func() {
		current := skipList.headNode
		for current.next[0] != nil {
			current = current.next[0]
			ch <- current.value
		}
		close(ch)
	}()
	return ch
}
