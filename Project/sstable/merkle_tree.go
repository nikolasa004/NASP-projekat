package sstable

import (
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"fmt"
	"os"
	"project/BlockManager"
	"strings"
)

func hash(data []byte) string {
	sum := sha256.Sum256(data)
	return hex.EncodeToString(sum[:])
}

type MerkleNode struct {
	Hash  string      `json:"hash"`
	Left  *MerkleNode `json:"left,omitempty"`
	Right *MerkleNode `json:"right,omitempty"`
}

func BuildTree(data [][]byte) *MerkleNode {
	if len(data) == 0 {
		return nil
	}

	// Kreiranje listova
	nodes := make([]*MerkleNode, 0, len(data))
	for _, d := range data {
		h := hash(d)
		node := &MerkleNode{Hash: h}
		nodes = append(nodes, node)
	}

	// Gradnja stabla
	for len(nodes) > 1 {
		if len(nodes)%2 != 0 {
			nodes = append(nodes, nodes[len(nodes)-1])
		}

		nextLevel := make([]*MerkleNode, 0, len(nodes)/2)
		for i := 0; i < len(nodes); i += 2 {
			left := nodes[i]
			right := nodes[i+1]
			parentHash := hash([]byte(left.Hash + right.Hash))
			parent := &MerkleNode{
				Hash:  parentHash,
				Left:  left,
				Right: right,
			}
			nextLevel = append(nextLevel, parent)
		}
		nodes = nextLevel
	}

	return nodes[0]
}

func GetRoot(root *MerkleNode) string {
	if root == nil {
		return ""
	}
	return root.Hash
}

func ValidateMerkleTree(expected, actual *MerkleNode, blockIndex *int) []int {
	var invalidBlocks []int

	var check func(e, a *MerkleNode)
	check = func(e, a *MerkleNode) {
		if e == nil || a == nil {
			return
		}
		if e.Left == nil && e.Right == nil && a.Left == nil && a.Right == nil {
			if e.Hash != a.Hash {
				invalidBlocks = append(invalidBlocks, *blockIndex)
			}
			*blockIndex = *blockIndex + 1 // ✅ ispravno inkrementiranje
			return
		}
		if e.Hash != a.Hash {
			check(e.Left, a.Left)
			check(e.Right, a.Right)
		}
	}

	check(expected, actual)
	return invalidBlocks
}

func CompareMerkleTrees(expectedRoot, actualRoot *MerkleNode) {
	if expectedRoot == nil || actualRoot == nil {
		fmt.Println("One of the Merkle trees is nil.")
		return
	}

	if expectedRoot.Hash == actualRoot.Hash {
		fmt.Println("Merkle trees are identical.")
		return
	}

	index := 0
	invalid := ValidateMerkleTree(expectedRoot, actualRoot, &index)

	if len(invalid) == 0 {
		fmt.Println("Merkle trees are different, but no specific invalid blocks found.")
	} else {
		fmt.Println("Merkle trees are different. Invalid blocks:")
		for _, idx := range invalid {
			fmt.Println("- Block:", idx)
		}
	}
}

func SaveMerkleTree(root *MerkleNode, filename string) error {
	data, err := json.MarshalIndent(root, "", "  ")
	if err != nil {
		return fmt.Errorf("Unsuccessful Merkle tree serialization: %v", err)
	}
	err = os.WriteFile(filename, data, 0644)
	if err != nil {
		return fmt.Errorf("Unsuccessful write to file: %v", err)
	}
	return nil
}

func LoadMerkleTree(filename string) (*MerkleNode, error) {
	data, err := os.ReadFile(filename)
	if err != nil {
		return nil, err
	}

	var root MerkleNode
	err = json.Unmarshal(data, &root)
	if err != nil {
		return nil, err
	}
	return &root, nil
}

func GetMerkleFilename(dataFilename string) string {
	// Ako je fajl `sstable-0001-Data.bin`, Merkle je `sstable-0001-Merkle.bin`
	// Ako je fajl `sstable-0001.bin`, Merkle je `sstable-0001-Merkle.bin`
	base := dataFilename
	if strings.HasSuffix(base, "-Data.bin") {
		base = strings.TrimSuffix(base, "-Data.bin")
	} else if strings.HasSuffix(base, ".bin") {
		base = strings.TrimSuffix(base, ".bin")
	}
	return base + "-Merkle.bin"
}
func IsSingleFile(filename string) bool {
	return !strings.HasSuffix(filename, "-Data.bin")
}

func VerifySstableMerkle(tocInput string, blockManager *BlockManager.BlockManager) error {
	tocFilename, err := FindTOCPath(tocInput)
	if err != nil {
		return fmt.Errorf("[Error] SSTable not found: %v", err)
	}

	toc, err := ReadTOCFile(tocFilename)
	if err != nil {
		return fmt.Errorf("[Error] Error reading TOC file: %v", err)
	}

	if toc.SingleFile {
		sstableFilename := toc.Files[0]
		data, err := ReadSSTable(sstableFilename, blockManager)
		if err != nil {
			return fmt.Errorf("[Error] Error reading SSTable (single): %v", err)
		}
		newMerkle := BuildTree(data)

		metadata, err := ReadMetadata(sstableFilename)
		if err != nil {
			return fmt.Errorf("[Error] Error reading metadata: %v", err)
		}
		file, err := os.Open(sstableFilename)
		if err != nil {
			return fmt.Errorf("[Error] Error opening file: %v", err)
		}
		defer file.Close()
		merkleBytes := make([]byte, metadata.MerkleTreeLength)
		_, err = file.ReadAt(merkleBytes, int64(metadata.MerkleTreeOffset))
		if err != nil {
			return fmt.Errorf("[Error] Error reading Merkle tree from file: %v", err)
		}
		storedMerkle := &MerkleNode{}
		err = json.Unmarshal(merkleBytes, storedMerkle)
		if err != nil {
			return fmt.Errorf("[Error] Error parsing Merkle tree: %v", err)
		}
		CompareMerkleTrees(newMerkle, storedMerkle)
	} else {
		merkleFilename := toc.Files[len(toc.Files)-1]
		data, err := ReadSSTableMultiMode(tocFilename, blockManager)
		if err != nil {
			return fmt.Errorf("[Error] Error reading SSTable (multi): %v", err)
		}
		newMerkle := BuildTree(data)
		storedMerkle, err := LoadMerkleTree(merkleFilename)
		if err != nil {
			return fmt.Errorf("[Error] Error loading Merkle tree: %v", err)
		}
		CompareMerkleTrees(newMerkle, storedMerkle)
	}
	return nil
}
func PrintMerkleTree(node *MerkleNode, level int) {
	if node == nil {
		return
	}
	indent := strings.Repeat("  ", level)
	fmt.Printf("%sLevel %d: %s\n", indent, level, node.Hash)
	PrintMerkleTree(node.Left, level+1)
	PrintMerkleTree(node.Right, level+1)
}
