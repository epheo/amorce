package link

import (
	"crypto/rand"
	_ "embed"
	"math/big"
	"strings"
)

// words.txt is the EFF short word list 2.0, by the Electronic Frontier
// Foundation, CC BY 3.0 US, less yo-yo: a dash reads as a separator. No two
// words share their first three letters, so those are all a person types.
//
//go:embed words.txt
var wordFile string

var wordList = strings.Fields(wordFile)

// JoinWords is how many words a join takes: about 62 bits, far beyond
// guessing in the hour they last.
const JoinWords = 6

// NewWords draws JoinWords words.
func NewWords() []string {
	out := make([]string, JoinWords)
	for i := range out {
		n, _ := rand.Int(rand.Reader, big.NewInt(int64(len(wordList))))
		out[i] = wordList[n.Int64()]
	}
	return out
}

// Expand reads typed words, each whole or by its first three letters or
// more. It returns the whole words, and the first typed one that is not on
// the list, if any.
func Expand(typed string) (words []string, unknown string) {
	for _, w := range strings.Fields(strings.ToLower(typed)) {
		found := ""
		if len(w) >= 3 {
			for _, c := range wordList {
				if strings.HasPrefix(c, w) {
					found = c
					break
				}
			}
		}
		if found == "" {
			return words, w
		}
		words = append(words, found)
	}
	return words, ""
}

// Secret is the words as the proofs use them.
func Secret(words []string) string { return strings.Join(words, " ") }
