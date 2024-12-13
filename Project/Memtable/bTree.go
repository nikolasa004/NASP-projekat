package Memtable

type TreeNode struct {
	keys     []string
	values   []*MemtableRecord
	children []*TreeNode
	parent   *TreeNode
}

func NewNode(size int, values []*MemtableRecord, parent *TreeNode) *TreeNode {
	node := TreeNode{keys: make([]string, 0, size), values: make([]*MemtableRecord, 0, size), children: make([]*TreeNode, 0, size+1), parent: parent}
	if len(values) == 0 {
		return &node
	}
	for _, v := range values {
		node.AddKey(v)
	}

	return &node
}

func (n *TreeNode) AddKey(value *MemtableRecord) int {
	key := value.Key
	if len(n.keys) == 0 {
		n.keys = append(n.keys, key)
		n.values = append(n.values, value)
		return 0
	}

	for i, k := range n.keys {
		if k > key {
			n.keys = append(n.keys, "")
			copy(n.keys[i+1:], n.keys[i:])
			n.keys[i] = key
			n.values = append(n.values, nil)
			copy(n.values[i+1:], n.values[i:])
			n.values[i] = value
			return i
		}
	}

	n.keys = append(n.keys, key)
	n.values = append(n.values, value)
	return len(n.keys) - 1
}

func (n *TreeNode) RemoveKey(index int) {
	n.keys = append(n.keys[:index], n.keys[index+1:]...)
	n.values = append(n.values[:index], n.values[index+1:]...)
}

func (n *TreeNode) InsertChild(index int, child *TreeNode) {
	n.children = append(n.children, nil)
	if index < len(n.children)-1 {
		copy(n.children[index+1:], n.children[index:])
		n.children[index] = child
	} else {
		n.children[index] = child
	}
}

type Tree struct {
	m    int
	root *TreeNode
}

func NewTree(m int) *Tree {
	valueArr := make([]*MemtableRecord, 0, m)
	return &Tree{m: m, root: NewNode(m, valueArr, nil)}
}

func (t *Tree) AddRecord(newRecord *MemtableRecord) {
	newKey := newRecord.Key
	node, i := t.FindNode(newKey)

	if i != -1 {
		return
	}
	node.AddKey(newRecord)
	for len(node.keys) == t.m {
		if node != t.root {
			if t.RotateNode(node) {
				return
			}
		}
		node = t.SplitNode(node)
	}

}

func (t *Tree) FindRotationSibling(node *TreeNode) (int, int, *TreeNode) {
	parent := node.parent
	my_position := -1
	sibling_position := -1
	var sib *TreeNode = nil
	for i, n := range parent.children {
		if n == node {
			my_position = i
			if sibling_position != -1 {
				break
			}
		}
		if len(n.keys) < t.m-1 {
			sibling_position = i
			sib = n
			if my_position != -1 {
				break
			}
		}
	}
	return my_position, sibling_position, sib
}

func (t *Tree) RotateNode(node *TreeNode) bool {
	parent := node.parent
	my_pos, sib_pos, sibling := t.FindRotationSibling(node)

	if sibling != nil {
		if my_pos == 0 {
			parent.AddKey(node.values[len(node.values)-1])
			node.RemoveKey(len(node.values) - 1)
		} else if my_pos == len(parent.children)-1 {
			parent.AddKey(node.values[0])
			node.RemoveKey(0)
		} else {
			parent.AddKey(node.values[len(node.values)-1])
			node.RemoveKey(len(node.values) - 1)
		}
		if sib_pos == 0 {
			sibling.AddKey(parent.values[0])
			parent.RemoveKey(0)
		} else if sib_pos == len(parent.children)-1 {
			sibling.AddKey(parent.values[len(parent.values)-1])
			parent.RemoveKey(len(parent.values) - 1)
		} else {
			sibling.AddKey(parent.values[sib_pos])
			parent.RemoveKey(sib_pos)
		}
		return true
	} else {
		return false
	}
}

func (t *Tree) SplitNode(node *TreeNode) *TreeNode {
	if node == t.root {
		newRoot := NewNode(t.m, node.values[len(node.values)/2:], nil)
		newNode := NewNode(t.m, node.values[len(node.values)/2+1:], newRoot)
		node.keys = node.keys[:len(node.keys)/2]
		node.values = node.values[:len(node.values)/2]
		node.parent = newRoot
		newRoot.children = append(newRoot.children, node, newNode)
		t.root = newRoot
		//split old children of node between new and old node
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
		//split node on two, then add middle key to parent node, and add new node to parent children
		pos := parent.AddKey(node.values[len(node.values)/2])
		newNode := NewNode(t.m, node.values[len(node.values)/2+1:], parent)
		node.keys = node.keys[:len(node.keys)/2]
		node.values = node.values[:len(node.values)/2]
		node.parent = parent
		parent.InsertChild(pos+1, newNode)
		//split old children of node between new and old node
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

func (t *Tree) FindNode(key string) (*TreeNode, int) {
	curr_node := t.root
	var found_node *TreeNode = nil
	node_position := -1
	if len(curr_node.keys) == 0 {
		return curr_node, -1
	}
	for {
		for i, k := range curr_node.keys {
			if k == key {
				found_node = curr_node
				node_position = i
				break
			} else if k > key {
				if len(curr_node.children) == 0 {
					return curr_node, -1
				}
				curr_node = curr_node.children[i]
				break
			} else if i == len(curr_node.keys)-1 {
				if len(curr_node.children) == 0 {
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
	node, index := t.FindNode(key)

	if index == -1 {
		return nil
	}

	return node.values[index]
}

func (t *Tree) RemoveElement(key string) {
	node, index := t.FindNode(key)

	if node == nil {
		return
	} else if len(node.children) != 0 {
		node, index = t.ReplaceSuccessor(node, index)
	}
	node.RemoveKey(index)
	for len(node.keys) < t.m/2-1 {
		borrow_i := t.FindBorrowSibling(node)
		if borrow_i != -1 {
			t.BorrowKey(node, index, borrow_i)
			return
		} else {
			node = t.MergeNode(node, index)
		}
	}
}

func (t *Tree) ReplaceSuccessor(node *TreeNode, index int) (*TreeNode, int) {
	var found_node *TreeNode = nil
	for len(found_node.children) > 0 {
		found_node = found_node.children[0]
	}
	node.keys[index] = found_node.keys[0]
	node.values[index] = found_node.values[0]
	found_node.keys[0] = node.keys[index]
	found_node.values[0] = node.values[index]
	return found_node, 0
}

func (t *Tree) MergeNode(node *TreeNode, node_index int) *TreeNode {
	parent := node.parent
	var sibling *TreeNode = nil
	if node_index == 0 {
		sibling = parent.children[1]

		sibling.AddKey(parent.values[0])
		for _, v := range node.values {
			sibling.AddKey(v)
		}

		parent.RemoveKey(0)
		parent.children = append(parent.children[:0], parent.children[1:]...)
		for _, c := range parent.children {
			c.parent = parent
		}
		if len(node.children) > 0 {
			//add children of node to sibling at beginning
			for _, c := range node.children {
				sibling.InsertChild(0, c)
			}
			for _, c := range sibling.children {
				c.parent = sibling
			}
		}
	} else {
		sibling = parent.children[node_index-1]

		sibling.AddKey(parent.values[node_index-1])
		for _, v := range node.values {
			sibling.AddKey(v)
		}

		parent.RemoveKey(node_index - 1)
		parent.children = append(parent.children[:node_index], parent.children[node_index+1:]...)
		for _, c := range parent.children {
			c.parent = parent
		}
		if len(node.children) > 0 {
			sibling.children = append(sibling.children, node.children...)
			for _, c := range sibling.children {
				c.parent = sibling
			}
		}
	}

	return parent
}

func (t *Tree) BorrowKey(node *TreeNode, index int, borrow_i int) {
	parent := node.parent
	for borrow_i < index {
		borrow_node := parent.children[borrow_i]
		curr_node := parent.children[borrow_i+1]
		curr_node.AddKey(parent.values[borrow_i])
		parent.keys[borrow_i] = borrow_node.keys[len(borrow_node.keys)-1]
		parent.values[borrow_i] = borrow_node.values[len(borrow_node.values)-1]
		borrow_node.RemoveKey(len(borrow_node.keys) - 1)
		borrow_i++
	}
	for borrow_i > index {
		borrow_node := parent.children[borrow_i]
		curr_node := parent.children[borrow_i-1]
		curr_node.AddKey(parent.values[borrow_i-1])
		parent.keys[borrow_i-1] = borrow_node.keys[0]
		parent.values[borrow_i-1] = borrow_node.values[0]
		borrow_node.RemoveKey(0)
		borrow_i--
	}
}

func (t *Tree) FindBorrowSibling(node *TreeNode) int {
	parent := node.parent
	for i, n := range parent.children {
		if len(n.keys) > t.m/2-1 {
			return i
		}
	}
	return -1
}