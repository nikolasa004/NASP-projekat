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
}


func NewSkipList(maxHeight int) *SkipList {
	headNext := make([]*Node, maxHeight)
	n := &Node{key: "",value:nil, next: headNext}

	return &SkipList{maxHeight: maxHeight, level:1, headNode: n}
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


// func (skipList *SkipList) removeRecord(k string) error{
// 	update := make([]*Node, skipList.level)
// 	current := skipList.headNode
// 	for i := skipList.level - 1; i >= 0 ; i-- { 
// 		for current.next[i] != nil && current.next[i].key < k {
// 			current = current.next[i]
// 		} 
// 		update[i] = current
// 	}

// 	current = current.next[0]
// 	if current != nil  && current.key == k {
// 		for i := 0; i < skipList.level ; i++ {
// 			if update[i].next[i] != current {
// 				break
// 			}
// 			update[i].next[i] = current.next[i]
// 		} 

// 		for skipList.level > 1 && skipList.headNode.next[skipList.level-1] == nil {
// 			skipList.level--
// 		}
// 	} else {
// 		return errors.New("element doesn't exist")
// 	}

// 	return nil
// }


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
