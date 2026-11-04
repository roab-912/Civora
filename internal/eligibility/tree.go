// Package eligibility implements Semaphore v4's Poseidon LeanIMT semantics.
package eligibility

import (
	"civora/internal/codec"
	"fmt"
	"github.com/iden3/go-iden3-crypto/poseidon"
	"math/big"
	"math/bits"
	"strconv"
)

const ScalarModulus = "21888242871839275222246405745257275088548364400416034343698204186575808495617"

var modulus, _ = new(big.Int).SetString(ScalarModulus, 10)

type Node [32]byte

func ParseField(s string, nonzero bool) (Node, error) {
	var out Node
	if s == "" || len(s) > 77 || (len(s) > 1 && s[0] == '0') {
		return out, fmt.Errorf("ERR_FIELD")
	}
	for _, r := range s {
		if r < '0' || r > '9' {
			return out, fmt.Errorf("ERR_FIELD")
		}
	}
	n, ok := new(big.Int).SetString(s, 10)
	if !ok || n.Sign() < 0 || n.Cmp(modulus) >= 0 || nonzero && n.Sign() == 0 {
		return out, fmt.Errorf("ERR_FIELD")
	}
	n.FillBytes(out[:])
	return out, nil
}
func (n Node) String() string { return new(big.Int).SetBytes(n[:]).String() }
func Hash(left, right Node) Node {
	n, e := poseidon.Hash([]*big.Int{new(big.Int).SetBytes(left[:]), new(big.Int).SetBytes(right[:])})
	if e != nil {
		panic(e)
	}
	var out Node
	n.FillBytes(out[:])
	return out
}
func depth(size uint64) int {
	if size < 2 {
		return 0
	}
	return bits.Len64(size - 1)
}
func width(size uint64, level int) uint64 {
	unit := uint64(1) << level
	return (size + unit - 1) / unit
}
func ValidateDepth(d uint8) error {
	if d != 16 && d != 26 {
		return fmt.Errorf("ERR_CIRCUIT_DEPTH")
	}
	return nil
}

type Tree struct {
	CircuitDepth uint8
	Size, Active uint64
	Levels       [][]Node
}

func New(depth uint8) (*Tree, error) {
	if e := ValidateDepth(depth); e != nil {
		return nil, e
	}
	return &Tree{CircuitDepth: depth, Levels: [][]Node{{}}}, nil
}
func (t *Tree) Clone() *Tree {
	n := *t
	n.Levels = make([][]Node, len(t.Levels))
	for i, l := range t.Levels {
		n.Levels[i] = append([]Node{}, l...)
	}
	return &n
}
func (t *Tree) Root() *string {
	if t.Size == 0 {
		return nil
	}
	r := t.Levels[depth(t.Size)][0].String()
	return &r
}
func (t *Tree) Append(values ...string) error {
	if uint64(len(values)) > (uint64(1)<<t.CircuitDepth)-t.Size {
		return fmt.Errorf("ERR_CAPACITY")
	}
	nodes := make([]Node, len(values))
	for i, s := range values {
		n, e := ParseField(s, true)
		if e != nil {
			return e
		}
		nodes[i] = n
	}
	old := t.Size
	t.Size += uint64(len(nodes))
	t.Active += uint64(len(nodes))
	t.Levels[0] = append(t.Levels[0], nodes...)
	for level := 1; level <= depth(t.Size); level++ {
		for len(t.Levels) <= level {
			t.Levels = append(t.Levels, []Node{})
		}
		need := int(width(t.Size, level))
		for len(t.Levels[level]) < need {
			t.Levels[level] = append(t.Levels[level], Node{})
		}
		for i := old >> level; i < uint64(need); i++ {
			left := t.Levels[level-1][2*i]
			value := left
			if 2*i+1 < uint64(len(t.Levels[level-1])) {
				value = Hash(left, t.Levels[level-1][2*i+1])
			}
			t.Levels[level][i] = value
		}
	}
	return nil
}
func (t *Tree) Update(index uint64, value string) error {
	if index >= t.Size {
		return fmt.Errorf("ERR_INDEX")
	}
	n, e := ParseField(value, false)
	if e != nil {
		return e
	}
	old := t.Levels[0][index]
	if old == (Node{}) && n != (Node{}) {
		return fmt.Errorf("ERR_RETIRED_POSITION")
	}
	if old != (Node{}) && n == (Node{}) {
		t.Active--
	}
	t.Levels[0][index] = n
	for level := 1; level <= depth(t.Size); level++ {
		index /= 2
		left := t.Levels[level-1][2*index]
		n = left
		if 2*index+1 < uint64(len(t.Levels[level-1])) {
			n = Hash(left, t.Levels[level-1][2*index+1])
		}
		t.Levels[level][index] = n
	}
	return nil
}
func (t *Tree) Rotate(index uint64, commitment string) error {
	if _, e := ParseField(commitment, true); e != nil {
		return e
	}
	if index >= t.Size || t.Levels[0][index] == (Node{}) {
		return fmt.Errorf("ERR_INDEX")
	}
	if t.Size == uint64(1)<<t.CircuitDepth {
		return fmt.Errorf("ERR_CAPACITY")
	}
	if e := t.Update(index, "0"); e != nil {
		return e
	}
	return t.Append(commitment)
}

type Metadata struct {
	NetworkID    string  `json:"network_id"`
	Epoch        string  `json:"protocol_epoch"`
	TreeID       string  `json:"tree_id"`
	SuiteID      string  `json:"suite_id"`
	Height       string  `json:"height"`
	CircuitDepth string  `json:"circuit_depth"`
	Size         string  `json:"size"`
	Active       string  `json:"active_count"`
	Root         *string `json:"root"`
	SnapshotID   string  `json:"snapshot_id"`
}

func (m Metadata) ID() string {
	return codec.Hash("eligibility/snapshot", []any{m.NetworkID, m.Epoch, m.TreeID, m.SuiteID, m.Height, m.CircuitDepth, m.Size, m.Active, m.Root})
}
func (m Metadata) Validate() error {
	if m.SnapshotID != m.ID() {
		return fmt.Errorf("ERR_SNAPSHOT_ID")
	}
	d, e := codec.Uint(m.CircuitDepth)
	if e != nil || d != 16 && d != 26 {
		return fmt.Errorf("ERR_CIRCUIT_DEPTH")
	}
	if m.SuiteID != "semaphore-v4.14.5-depth-"+m.CircuitDepth || len(m.TreeID) == 0 || len(m.TreeID) > 96 {
		return fmt.Errorf("ERR_SUITE")
	}
	size, e := codec.Uint(m.Size)
	if e != nil || size > uint64(1)<<d {
		return fmt.Errorf("ERR_CAPACITY")
	}
	a, e := codec.Uint(m.Active)
	if e != nil || a > size {
		return fmt.Errorf("ERR_ACTIVE_COUNT")
	}
	if _, e = codec.Uint(m.Height); e != nil {
		return e
	}
	epoch, e := codec.Uint(m.Epoch)
	if e != nil || epoch == 0 {
		return fmt.Errorf("ERR_EPOCH")
	}
	if _, e = codec.ChainID(m.NetworkID); e != nil {
		return e
	}
	if (m.Root == nil) != (size == 0) {
		return fmt.Errorf("ERR_ROOT")
	}
	if m.Root != nil {
		if _, e = ParseField(*m.Root, false); e != nil {
			return e
		}
	}
	return nil
}
func (t *Tree) Metadata(network, epoch, treeID, suite, height string) Metadata {
	m := Metadata{NetworkID: network, Epoch: epoch, TreeID: treeID, SuiteID: suite, Height: height, CircuitDepth: strconv.Itoa(int(t.CircuitDepth)), Size: strconv.FormatUint(t.Size, 10), Active: strconv.FormatUint(t.Active, 10), Root: t.Root()}
	m.SnapshotID = m.ID()
	return m
}

type Path struct {
	Snapshot  Metadata `json:"snapshot"`
	LeafIndex string   `json:"leaf_index"`
	Leaf      string   `json:"leaf"`
	Index     string   `json:"index"`
	Siblings  []string `json:"siblings"`
}

func makePath(m Metadata, physical uint64, get func(int, uint64) (Node, error)) (Path, error) {
	if e := m.Validate(); e != nil {
		return Path{}, e
	}
	size, _ := codec.Uint(m.Size)
	if physical >= size {
		return Path{}, fmt.Errorf("ERR_INDEX")
	}
	leaf, e := get(0, physical)
	if e != nil {
		return Path{}, e
	}
	if leaf == (Node{}) {
		return Path{}, fmt.Errorf("ERR_RETIRED_POSITION")
	}
	p := Path{Snapshot: m, LeafIndex: strconv.FormatUint(physical, 10), Leaf: leaf.String(), Siblings: []string{}}
	index, packed := physical, uint64(0)
	for level := 0; level < depth(size); level++ {
		sibling := index ^ 1
		if sibling < width(size, level) {
			node, e := get(level, sibling)
			if e != nil {
				return Path{}, e
			}
			packed |= (index & 1) << len(p.Siblings)
			p.Siblings = append(p.Siblings, node.String())
		}
		index /= 2
	}
	p.Index = strconv.FormatUint(packed, 10)
	if e = p.Verify(); e != nil {
		return Path{}, e
	}
	return p, nil
}
func (t *Tree) Path(m Metadata, index uint64) (Path, error) {
	if m.Size != strconv.FormatUint(t.Size, 10) || m.Root == nil || t.Root() == nil || *m.Root != *t.Root() {
		return Path{}, fmt.Errorf("ERR_SNAPSHOT")
	}
	return makePath(m, index, func(level int, index uint64) (Node, error) { return t.Levels[level][index], nil })
}
func (p Path) Verify() error {
	if e := p.Snapshot.Validate(); e != nil {
		return e
	}
	size, _ := codec.Uint(p.Snapshot.Size)
	physical, e := codec.Uint(p.LeafIndex)
	if e != nil || physical >= size {
		return fmt.Errorf("ERR_INDEX")
	}
	n, e := ParseField(p.Leaf, true)
	if e != nil {
		return e
	}
	packed, e := codec.Uint(p.Index)
	if e != nil {
		return e
	}
	count := 0
	idx := physical
	expected := uint64(0)
	for level := 0; level < depth(size); level++ {
		if idx^1 < width(size, level) {
			if count >= len(p.Siblings) {
				return fmt.Errorf("ERR_PATH")
			}
			expected |= (idx & 1) << count
			sibling, e := ParseField(p.Siblings[count], false)
			if e != nil {
				return e
			}
			if idx&1 == 0 {
				n = Hash(n, sibling)
			} else {
				n = Hash(sibling, n)
			}
			count++
		}
		idx /= 2
	}
	if count != len(p.Siblings) || expected != packed || p.Snapshot.Root == nil || n.String() != *p.Snapshot.Root {
		return fmt.Errorf("ERR_PATH")
	}
	return nil
}
