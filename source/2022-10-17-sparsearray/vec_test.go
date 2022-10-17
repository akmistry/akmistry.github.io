package vectest

import (
	"fmt"
	"log"
	"math/rand"
	"sort"
	"testing"
	"unsafe"

	"github.com/akmistry/go-util/bitmap"
)

type Sparse256Array interface {
	Clear()
	Put(i uint8, v interface{})
	Get(i uint8) interface{}
}

type SparseishVector struct {
	blocks []Sparse256Array
	len    int
}

func NewSparseishVector(length int, allocArray func() Sparse256Array) *SparseishVector {
	v := &SparseishVector{
		blocks: make([]Sparse256Array, (length+255)/256),
		len:    length,
	}
	for i := range v.blocks {
		v.blocks[i] = allocArray()
	}
	return v
}

func (v *SparseishVector) Len() int {
	return v.len
}

func (v *SparseishVector) Clear() {
	for _, b := range v.blocks {
		b.Clear()
	}
}

func (v *SparseishVector) Put(i int, val interface{}) {
	v.blocks[i/256].Put(uint8(i), val)
}

func (v *SparseishVector) Get(i int) interface{} {
	return v.blocks[i/256].Get(uint8(i))
}

type MapArray struct {
	m map[uint8]interface{}
}

func (a *MapArray) Clear() {
	a.m = make(map[uint8]interface{})
}

func (a *MapArray) Put(i uint8, v interface{}) {
	if v == nil {
		delete(a.m, i)
	} else {
		a.m[i] = v
	}
}

func (a *MapArray) Get(i uint8) interface{} {
	return a.m[i]
}

type binaryArrayItem struct {
	index uint8
	v     interface{}
}

type BinaryArray struct {
	items []binaryArrayItem
}

func (a *BinaryArray) Clear() {
	a.items = nil
}

func (a *BinaryArray) Put(i uint8, v interface{}) {
	index := sort.Search(len(a.items), func(n int) bool {
		return a.items[n].index >= i
	})
	if index < len(a.items) && a.items[index].index == i {
		if v == nil {
			copy(a.items[index:], a.items[index+1:])
			a.items = a.items[:len(a.items)-1]
		} else {
			a.items[index].v = v
		}
	} else {
		a.items = append(a.items, binaryArrayItem{})
		copy(a.items[index+1:], a.items[index:])
		a.items[index].index = i
		a.items[index].v = v
	}
}

func (a *BinaryArray) Get(i uint8) interface{} {
	index := sort.Search(len(a.items), func(n int) bool {
		return a.items[n].index >= i
	})
	if index < len(a.items) && a.items[index].index == i {
		return a.items[index].v
	}
	return nil
}

type SplitBinaryArray struct {
	indexes []uint8
	values  []interface{}
}

func (a *SplitBinaryArray) Clear() {
	a.indexes, a.values = nil, nil
}

func (a *SplitBinaryArray) Put(i uint8, v interface{}) {
	index := sort.Search(len(a.indexes), func(n int) bool {
		return a.indexes[n] >= i
	})
	if index < len(a.indexes) && a.indexes[index] == i {
		if v == nil {
			copy(a.indexes[index:], a.indexes[index+1:])
			a.indexes = a.indexes[:len(a.indexes)-1]

			copy(a.values[index:], a.values[index+1:])
			a.values = a.values[:len(a.values)-1]
		} else {
			a.values[index] = v
		}
	} else {
		a.indexes = append(a.indexes, 0)
		copy(a.indexes[index+1:], a.indexes[index:])
		a.indexes[index] = i

		a.values = append(a.values, nil)
		copy(a.values[index+1:], a.values[index:])
		a.values[index] = v
	}
}

func (a *SplitBinaryArray) Get(i uint8) interface{} {
	index := sort.Search(len(a.indexes), func(n int) bool {
		return a.indexes[n] >= i
	})
	if index < len(a.indexes) && a.indexes[index] == i {
		return a.values[index]
	}
	return nil
}

type BitmapArray struct {
	bm     bitmap.Bitmap256
	values []interface{}
}

func (a *BitmapArray) Clear() {
	a.bm = bitmap.Bitmap256{}
	a.values = nil
}

func (a *BitmapArray) Put(i uint8, v interface{}) {
	index := a.bm.CountLess(i)
	if index < len(a.values) && a.bm.Get(i) {
		if v == nil {
			a.bm.Clear(i)
			copy(a.values[index:], a.values[index+1:])
			a.values = a.values[:len(a.values)-1]
		} else {
			a.values[index] = v
		}
	} else if v != nil {
		a.bm.Set(i)
		a.values = append(a.values, nil)
		copy(a.values[index+1:], a.values[index:])
		a.values[index] = v
	}
}

func (a *BitmapArray) Get(i uint8) interface{} {
	index := a.bm.CountLess(i)
	if index < len(a.values) && a.bm.Get(i) {
		return a.values[index]
	}
	return nil
}

type arrayType struct {
	name  string
	alloc func() Sparse256Array
}

var arrayTypes = []arrayType{
	{"MapArray", func() Sparse256Array {
		return &MapArray{m: make(map[uint8]interface{})}
	}},
	{"BinaryArray", func() Sparse256Array {
		return &BinaryArray{}
	}},
	{"SplitBinaryArray", func() Sparse256Array {
		return &SplitBinaryArray{}
	}},
	{"BitmapArray", func() Sparse256Array {
		return &BitmapArray{}
	}},
}

func init() {
	log.Printf("sizeof(MapArray): %d", unsafe.Sizeof(MapArray{}))
	log.Printf("sizeof(BinaryArray): %d", unsafe.Sizeof(BinaryArray{}))
	log.Printf("sizeof(binaryArrayItem): %d", unsafe.Sizeof(binaryArrayItem{}))
	log.Printf("sizeof(SplitBinaryArray): %d", unsafe.Sizeof(SplitBinaryArray{}))
	log.Printf("sizeof(BitmapArray): %d", unsafe.Sizeof(BitmapArray{}))
}

func generateTestData(size, maxInt int) []int {
	d := make([]int, size)
	for i := range d {
		d[i] = rand.Intn(maxInt)
	}
	return d
}

const (
	ArraySize = 50 * 1000 * 1000
)

var (
	FillPercentiles = []int{1, 5, 10, 25, 50, 75, 90, 95, 99}
	staticTestData  = generateTestData(ArraySize, ArraySize)
)

func BenchmarkArrayPut(b *testing.B) {
	for _, p := range FillPercentiles {
		fillItems := (ArraySize * p) / 100
		testData := staticTestData[:fillItems]
		for _, t := range arrayTypes {
			testName := fmt.Sprintf("%s/%d%%", t.name, p)
			v := NewSparseishVector(ArraySize, t.alloc)
			b.Run(testName, func(b *testing.B) {
				for i := 0; i < b.N; i++ {
					if i > 0 && i%fillItems == 0 {
						b.StopTimer()
						v.Clear()
						b.StartTimer()
					}
					k := testData[i%fillItems]
					v.Put(k, k)
				}
			})
		}
	}
}

func BenchmarkArrayGet(b *testing.B) {
	for _, p := range FillPercentiles {
		fillItems := (ArraySize * p) / 100
		testData := staticTestData[:fillItems]
		var sortedTestData []int

		for _, t := range arrayTypes {
			testName := fmt.Sprintf("%s/%d%%", t.name, p)
			v := NewSparseishVector(ArraySize, t.alloc)
			initVec := true
			b.Run(testName, func(b *testing.B) {
				if sortedTestData == nil {
					// Sorting the elements to be inserted is much quicker than
					// doing it randomly, due to improved cache locality.
					sortedTestData = append([]int(nil), testData...)
					sort.Sort(sort.IntSlice(sortedTestData))
				}
				if initVec {
					for _, k := range sortedTestData {
						v.Put(k, k)
					}
					initVec = false
				}
				b.ResetTimer()

				for i := 0; i < b.N; i++ {
					k := testData[i%fillItems]
					v.Get(k)
				}
			})
		}
	}
}

func BenchmarkArray256Worse(b *testing.B) {
	var a BitmapArray
	for i := 0; i < 256; i++ {
		a.Put(uint8(i), 7)
	}

	for i := 0; i < b.N; i++ {
		a.Put(0, nil)
		a.Put(0, 1)
	}
}

func BenchmarkArray256Mid(b *testing.B) {
	var a BitmapArray
	for i := 0; i < 256; i++ {
		a.Put(uint8(i), 7)
	}

	for i := 0; i < b.N; i++ {
		a.Put(128, nil)
		a.Put(128, 1)
	}
}

func BenchmarkArray256Best(b *testing.B) {
	var a BitmapArray
	for i := 0; i < 256; i++ {
		a.Put(uint8(i), 7)
	}

	for i := 0; i < b.N; i++ {
		a.Put(255, nil)
		a.Put(255, 1)
	}
}

func BenchmarkArray256Assign(b *testing.B) {
	var a BitmapArray

	for i := 0; i < b.N; i++ {
		a.Put(uint8(i), i)
	}
}

func BenchmarkArray256Get(b *testing.B) {
	var a BitmapArray
	for i := 0; i < 16; i++ {
		a.Put(uint8(i), i)
	}

	for i := 0; i < b.N; i++ {
		_ = a.Get(uint8(i))
	}
}

func BenchmarkMap(b *testing.B) {
	a := make(map[uint8]interface{})
	for i := 1; i < 16; i++ {
		a[uint8(i)] = i
	}

	for i := 0; i < b.N; i++ {
		a[uint8(i)] = 1
		delete(a, uint8(i))
	}
}

func BenchmarkMapAssign(b *testing.B) {
	a := make(map[uint8]interface{})

	for i := 0; i < b.N; i++ {
		a[uint8(i)] = 1
	}
}

func BenchmarkMapGet(b *testing.B) {
	a := make(map[uint8]interface{})
	for i := 0; i < 16; i++ {
		a[uint8(i)] = i
	}

	for i := 0; i < b.N; i++ {
		_ = a[uint8(i)]
	}
}
