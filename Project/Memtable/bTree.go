package Memtable

type TreeNode struct {
	keys     []string
	values   []*MemtableRecord
	children []*TreeNode
	parent   *TreeNode
}

func newNode(size int, values []*MemtableRecord, parent *TreeNode) *TreeNode {
	node := TreeNode{keys: make([]string, 0, size), values: make([]*MemtableRecord, 0, size), children: make([]*TreeNode, 0, size+1), parent: parent}
	if len(values) == 0 {
		return &node
	}
	for _, v := range values {
		node.addKey(v)
	}

	return &node
}

func (n *TreeNode) addKey(value *MemtableRecord) int { // dodaje novi element u cvor i vraca poziciju na koju je dodao
	newKey := value.Key

	if len(n.keys) == 0 {
		n.keys = append(n.keys, newKey)
		n.values = append(n.values, value)
		return 0
	}

	for i, k := range n.keys {
		if k > newKey {
			n.keys = append(n.keys, "")
			copy(n.keys[i+1:], n.keys[i:])
			n.keys[i] = newKey
			n.values = append(n.values, nil)
			copy(n.values[i+1:], n.values[i:])
			n.values[i] = value
			return i
		}
	}

	n.keys = append(n.keys, newKey)
	n.values = append(n.values, value)
	return len(n.keys) - 1
}

func (n *TreeNode) removeKey(index int) *MemtableRecord { // uklanja element iz cvora sa odredjene pozicije
	targetEl := n.values[index]
	n.keys = append(n.keys[:index], n.keys[index+1:]...)
	n.values = append(n.values[:index], n.values[index+1:]...)
	return targetEl
}

func (n *TreeNode) insertChild(index int, child *TreeNode) { // insertuje dete na datu poziciju
	n.children = append(n.children, nil)
	if index < len(n.children)-1 {
		copy(n.children[index+1:], n.children[index:])
		n.children[index] = child
	} else {
		n.children[index] = child
	}
}

type Tree struct {
	m                 int // maksimalni broj elemenata u cvoru
	root              *TreeNode
	size              int
	state             string
	linkedSegment     int
	lastBlockPosition int
}

func (t *Tree) GetLastBlockPosition() int {
	return t.lastBlockPosition
}

func (t *Tree) SetLastBlockPosition(position int) {
	t.lastBlockPosition = position
}

func (t *Tree) GetLength() int {
	return t.size
}

func (t *Tree) Clear() {
	valueArr := make([]*MemtableRecord, 0, t.m)
	t.root = newNode(t.m, valueArr, nil)
	t.size = 0
	t.state = "empty"
	t.linkedSegment = 0
	t.lastBlockPosition = 0
}

func (t *Tree) SetState(state string) {
	t.state = state
}

func (t *Tree) GetState() string {
	return t.state
}

func (t *Tree) GetLinkedSegment() int {
	return t.linkedSegment
}

func (t *Tree) SetLinkedSegment(segment int) {
	t.linkedSegment = segment
}

func NewTree(m int) *Tree {
	valueArr := make([]*MemtableRecord, 0, m)
	return &Tree{m: m, root: newNode(m, valueArr, nil), size: 0, state: "empty", linkedSegment: 0, lastBlockPosition: 0}
}

func (t *Tree) AddRecord(newRecord *MemtableRecord) { // dodaje novi zapis u stablo
	newKey := newRecord.Key
	node, i := t.findNode(newKey)

	if i != -1 { // pronadjen zapis sa ovim kljucem - azurira se
		t.updateValue(node, i, newRecord)
		return
	}

	node.addKey(newRecord)
	t.size++
	for len(node.keys) == t.m { // prekoracili smo maksimalan broj elemenata u cvoru
		if node != t.root {
			if t.rotateNode(node) {
				return
			}
		}
		node = t.splitNode(node)
	}
}

func (t *Tree) updateValue(node *TreeNode, index int, value *MemtableRecord) {
	node.values[index] = value
}

func (t *Tree) findRotationSibling(node *TreeNode) (int, int, *TreeNode) { // trazi siblinga za rotaciju
	parent := node.parent
	myPosition := -1
	siblingPosition := -1
	var sib *TreeNode = nil
	for i, n := range parent.children {
		if n == node {
			myPosition = i
			if siblingPosition != -1 {
				break
			}
		}
		if len(n.keys) < t.m-1 {
			siblingPosition = i
			sib = n
			if myPosition != -1 {
				break
			}
		}
	}
	return myPosition, siblingPosition, sib
}

// rasterecujemo trenutni prepunjeni cvor tako sto pomeramo jedan element u roditelja
// roditelj pomera jedan element u cvor nekog drugog deteta
func (t *Tree) rotateNode(node *TreeNode) bool {
	parent := node.parent
	myPosition, sibPosition, sibling := t.findRotationSibling(node)

	if sibling != nil {
		if sibPosition < myPosition {
			// Sibling je levo od moje pozicije, pomeram prvi u roditelja
			parent.addKey(node.values[0])
			node.removeKey(0)
		} else {
			// Sibling je desno od moje pozicije, pomeram poslednji u roditelja
			parent.addKey(node.values[len(node.values)-1])
			node.removeKey(len(node.values) - 1)
		}
		// Pomeram iz roditelja u siblinga
		if sibPosition == 0 {
			sibling.addKey(parent.values[0])
			parent.removeKey(0)
		} else if sibPosition == len(parent.children)-1 {
			sibling.addKey(parent.values[len(parent.values)-1])
			parent.removeKey(len(parent.values) - 1)
		} else {
			sibling.addKey(parent.values[sibPosition])
			parent.removeKey(sibPosition)
		}
		return true
	} else {
		return false
	}
}

// pomeramo jedan od elemenata u roditeljski cvor i onda se pocetni cvor deli na dva dela
// to radimo ili dok roditeljski cvor vise ne mora da se deli ili dok ne dodjemo do korena
func (t *Tree) splitNode(node *TreeNode) *TreeNode {
	if node == t.root {
		newRoot := newNode(t.m, []*MemtableRecord{node.values[len(node.values)/2]}, nil)
		newNode := newNode(t.m, node.values[len(node.values)/2+1:], newRoot)
		node.keys = node.keys[:len(node.keys)/2]
		node.values = node.values[:len(node.values)/2]
		node.parent = newRoot
		newRoot.children = append(newRoot.children, node, newNode)
		t.root = newRoot
		//podeli i decu podeljenog cvora
		if len(node.children) > 0 {
			newNode.children = append(newNode.children, node.children[len(node.children)/2+1:]...)
			for _, c := range newNode.children {
				c.parent = newNode
			}
			node.children = node.children[:len(node.children)/2+1]
		}
		return newRoot
	} else {
		parent := node.parent
		//dodaj srednji kljuc u roditeljski cvor, podeli trenutni cvor na dva dela
		pos := parent.addKey(node.values[len(node.values)/2])
		newNode := newNode(t.m, node.values[len(node.values)/2+1:], parent)
		node.keys = node.keys[:len(node.keys)/2]
		node.values = node.values[:len(node.values)/2]
		node.parent = parent
		parent.insertChild(pos+1, newNode)
		//podeli i decu podeljenog cvora
		if len(node.children) > 0 {
			newNode.children = append(newNode.children, node.children[len(node.children)/2+1:]...)
			for _, c := range newNode.children {
				c.parent = newNode
			}
			node.children = node.children[:len(node.children)/2+1]
		}
		return parent
	}
}

// pretrazuje kljuc u stablu, ako ga pronadje vraca cvor i poziciju elementa
// ako ga ne pronadje vraca cvor u kom je trebao da bude
func (t *Tree) findNode(key string) (*TreeNode, int) {
	curr_node := t.root
	var found_node *TreeNode = nil
	node_position := -1
	if len(curr_node.keys) == 0 {
		return curr_node, node_position
	}
	for {
		for i, k := range curr_node.keys {
			if k == key { // pronadjen
				found_node = curr_node
				node_position = i
				break
			} else if k > key {
				if len(curr_node.children) == 0 { //nema ovakvog elementa u cvoru
					return curr_node, -1
				}
				curr_node = curr_node.children[i] // prelazimo na dete
				break
			} else if i == len(curr_node.keys)-1 {
				if len(curr_node.children) == 0 { // nema ovakvog elementa u cvoru
					return curr_node, -1
				}
				curr_node = curr_node.children[i+1]
				break
			}
		}
		if found_node != nil {
			break
		}
	}
	return found_node, node_position
}

func (t *Tree) FindRecord(key string) *MemtableRecord {
	node, index := t.findNode(key)

	if index == -1 {
		return nil
	}

	return node.values[index]
}

// inorder obilazak kroz stablo, sluzi kako bih vratio sortirane vrednosti iz stabla
func (t *Tree) Iterator() <-chan *MemtableRecord {
	ch := make(chan *MemtableRecord)
	go func() {
		t.inorder(t.root, ch)
		close(ch)
	}()
	return ch
}

func (t *Tree) inorder(node *TreeNode, ch chan<- *MemtableRecord) {
	if node == nil {
		return
	}
	if len(node.children) == 0 {
		for _, v := range node.values {
			ch <- v
		}
		return
	}
	for i, c := range node.children {
		t.inorder(c, ch)
		if i < len(node.values) {
			ch <- node.values[i]
		}
	}
}

func (t *Tree) GetSortedRecords() []*MemtableRecord { // vrednosti u stablu su sortirane po kljucevima
	if t.root == nil {
		return nil
	} else {
		values := make([]*MemtableRecord, 0, t.size)
		for record := range t.Iterator() {
			values = append(values, record)
		}
		return values
	}
}