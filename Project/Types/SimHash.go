package Types

import (
	"crypto/md5"
	"strings"
)

// Lista zaustavnih reci
var stopWordsList = []string{
	"all", "was", "with", "be", "by", "has", "it", "this", "not", "what", "so",
	"he", "she", "they", "we", "you", "i", "him", "her", "their", "them", "our",
	"us", "me", "do", "will", "would", "could", "should", "about", "because",
	"over", "under", "there", "here", "out", "now", "then", "if", "when", "where",
	"who", "why", "how", "also", "such", "more", "most", "very", "these", "those",
	"like", "one", "two", "three", "no", "yes", "get", "got", "been", "each", "every",
	"through", "after", "before", "back", "much", "still", "own", "said", "made",
	"make", "know", "want", "even", "though", "between", "both", "first", "last",
	"new", "old", "use", "used", "really", "never", "always", "a", "an", "for", "is",
	"and", "any", "are", "as", "at", "but", "can", "or", "to", "than", "that", "the",
	"on", "in", "of", "its", "which", "up", "have", "had",
}

func isStopWord(word string) bool {
	for _, stopWord := range stopWordsList {
		if word == stopWord {
			return true
		}
	}
	return false
}

func removeStopWords(tokens []string) []string {
	filteredTokens := []string{}
	for _, token := range tokens {
		if !isStopWord(token) {
			filteredTokens = append(filteredTokens, token)
		}
	}
	return filteredTokens
}

func HammingDistance(a, b string) int {
	if len(a) != len(b) {
		return -1
	}
	count := 0
	for i := range a {
		if a[i] != b[i] {
			count++
		}
	}
	return count
}

func hashToBIts(word string) []int {
	hash := md5.Sum([]byte(word))
	bits := make([]int, 128)
	for i, b := range hash {
		for j := 0; j < 8; j++ {
			if b&(1<<(7-j)) != 0 {
				bits[i*8+j] = 1
			} else {
				bits[i*8+j] = -1
			}
		}
	}
	return bits
}

func ComputeFingerprint(text string) string {
	tokens := strings.Fields(text)
	tokens = removeStopWords(tokens)
	if len(tokens) == 0 {
		return "00000000"
	}

	weights := make(map[string]int)
	for _, token := range tokens {
		weights[token]++
	}

	type item struct {
		bits    []int
		weights int
	}

	entries := []item{}
	for token, weight := range weights {
		entries = append(entries, item{
			bits:    hashToBIts(token),
			weights: weight,
		})
	}

	result := ""
	for col := 0; col < 8; col++ {
		sum := 0
		for _, e := range entries {
			sum += e.bits[col] * e.weights

		}
		if sum > 0 {
			result += "1"
		} else {
			result += "0"
		}

	}
	return result
}
