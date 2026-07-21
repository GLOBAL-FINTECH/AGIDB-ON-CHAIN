package merkle

import (
	"crypto/sha256"
	"encoding/hex"
	"sort"
	"strings"
)

func Hash(b []byte) string { h := sha256.Sum256(b); return hex.EncodeToString(h[:]) }
func Root(leaves [][]byte) string {
	if len(leaves) == 0 { return Hash(nil) }
	hs := make([][]byte, len(leaves))
	for i, l := range leaves { h := sha256.Sum256(l); hs[i] = h[:] }
	for len(hs) > 1 {
		n := make([][]byte, 0, (len(hs)+1)/2)
		for i := 0; i < len(hs); i += 2 {
			r := hs[i]
			if i+1 < len(hs) { r = hs[i+1] }
			x := append(append([]byte{}, hs[i]...), r...)
			h := sha256.Sum256(x)
			n = append(n, h[:])
		}
		hs = n
	}
	return hex.EncodeToString(hs[0])
}
func StateRoot(s map[string][]byte) string { return PrefixRoot(s, "") }
func PrefixRoot(s map[string][]byte, p string) string {
	ks := make([]string, 0)
	for k := range s { if p == "" || strings.HasPrefix(k, p) { ks = append(ks, k) } }
	sort.Strings(ks)
	leaves := make([][]byte, 0, len(ks))
	for _, k := range ks { x := append([]byte(k+"\x00"), s[k]...); leaves = append(leaves, x) }
	return Root(leaves)
}
func PrefixCount(s map[string][]byte, p string) int {
	n := 0
	for k := range s { if strings.HasPrefix(k, p) { n++ } }
	return n
}

type ProofStep struct { Hash string `json:"hash"`; Left bool `json:"left"` }
type Proof struct {
	Key string `json:"key"`
	Value []byte `json:"value,omitempty"`
	Exists bool `json:"exists"`
	LeafHash string `json:"leaf_hash,omitempty"`
	Root string `json:"root"`
	Index int `json:"index"`
	Total int `json:"total"`
	Path []ProofStep `json:"path"`
}

func StateProof(s map[string][]byte, key string) Proof {
	ks := make([]string, 0, len(s))
	for k := range s { ks = append(ks, k) }
	sort.Strings(ks)
	idx := sort.SearchStrings(ks, key)
	p := Proof{Key: key, Exists: idx < len(ks) && ks[idx] == key, Root: StateRoot(s), Index: idx, Total: len(ks)}
	if !p.Exists { return p }
	p.Value = append([]byte{}, s[key]...)
	leaves := make([][]byte, len(ks))
	for i, k := range ks { x := append([]byte(k+"\x00"), s[k]...); h := sha256.Sum256(x); leaves[i] = append([]byte{}, h[:]...) }
	p.LeafHash = hex.EncodeToString(leaves[idx])
	pos := idx
	level := leaves
	for len(level) > 1 {
		sib := pos ^ 1
		if sib >= len(level) { sib = pos }
		p.Path = append(p.Path, ProofStep{Hash: hex.EncodeToString(level[sib]), Left: sib < pos})
		next := make([][]byte, 0, (len(level)+1)/2)
		for i := 0; i < len(level); i += 2 {
			r := level[i]
			if i+1 < len(level) { r = level[i+1] }
			x := append(append([]byte{}, level[i]...), r...)
			h := sha256.Sum256(x)
			next = append(next, append([]byte{}, h[:]...))
		}
		pos /= 2
		level = next
	}
	return p
}

func VerifyProof(p Proof) bool {
	if !p.Exists { return false }
	x := append([]byte(p.Key+"\x00"), p.Value...)
	h := sha256.Sum256(x)
	cur := h[:]
	if hex.EncodeToString(cur) != p.LeafHash { return false }
	for _, st := range p.Path {
		sib, err := hex.DecodeString(st.Hash)
		if err != nil { return false }
		var in []byte
		if st.Left { in = append(append([]byte{}, sib...), cur...) } else { in = append(append([]byte{}, cur...), sib...) }
		q := sha256.Sum256(in)
		cur = q[:]
	}
	return hex.EncodeToString(cur) == p.Root
}
