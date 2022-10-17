---
title: "Hash tables, binary search, or... something else?"
date: "2022-10-17"
---

I've been working on a little project (read: distraction) in Go recently,
and having observed btree lookup performance was a bottleneck, I decided to
write a [Radix Tree](https://en.wikipedia.org/wiki/Radix_tree).

This post is NOT about radix trees, or whether that is an appropriate data
structure (in my benchmarks, it's a big win). But for those that are curious,
the implementation can be found at
https://github.com/akmistry/go-util/tree/master/radix-tree.

A component of the radix tree is the lookup table of children in internal
nodes. In my case, since I was using integer keys, it made sense for the tree
to be radix-256, where each node in the tree has a 256-element table of child
nodes, based on the next byte in the integer key.

Example:
```
Lookup 0x1234

Node:
- child[0]:
- child[1]:
- ...
- child[0x12]: ->
  node
  - child[0]:
  - child[1]:
  - ...
  - child[0x34]: -> value
```

The beauty of a radix tree is that at each node, finding the next node is an
array lookup based on the key, and does not require any "comparison"
operations per-se, except for the final element to check whether or not it
matches the lookup key.

Since the list of node children will be sparse, using a 256-element array would
waste a lot of space.

So I did a little exploration building a sparse 256-element array.

## The obvious solution: hash tables

This is the dead-obvious solution to implementing a sparse array, use a hash
table. The key is just the array index. The hash table will handle resizing
as elements are addad and removed, so that the table will consume ~O(# of
elements) of space at any given point in time. Insertion and lookups are
amortised constant time.

However, there is one big problem with hash tables: they're unordered. Ordered
iteration is a key requirement. Although this can be simulated with a hash
tables, by looping over the array indices in order and probing the table to see
if it has the element, this can be quite expensive. Particularly if the table is
sparse. For example, if there are only 16 elements of the array populated,
iterating over them in order would still require 256 hash tables lookups.

## The second-obvious solution: ordered array with binary search

This solution involves storing the array index and value as a pair in an array,
sorted by index. To lookup an array index, do a binary search over the pairs
to find the one which has a matching array index. Space efficiency is similar
to the hash table, but lookups are much more expense due to the binary search.
And insertions are much more expensive due to having to shift subsequent
elements when inserting into the middle of the array. Using an ordered array
with binary search is a common technique, including in the B-Tree
implementation I was formerly using:
https://github.com/google/btree/blob/8e29150ba321eef204059de2ab494f179b6cff2c/btree.go#L190

However, this has the advantage that elements are ordered, making ordered
iteration trivial and inexpensive.

## A slight improvement to ordered arrays: split the key and value arrays

Storing the array index and value as a pair has a memory usage problem.
To demonstrate, here's what it would look like in Go:
```go
type binaryArrayItem struct {
  index uint8
  v     interface{}
}
```

Since the array will only hold 256 elements, using a single byte as the array index
is fine. However, that byte will actually consume 8 bytes (on a 64-bit system)
due to alignment, and the whole structure consumes 24 bytes (interfaces are 2
pointers, hence 16 bytes). So for each element of the array, we're wasting 7
bytes, or ~29% of the space.

The solution here is to split the array indices and values into two arrays
which are maintained in parallel. Storing the uint8's as a seperate array
allows them to be packed together, eliminating the wasted space, at a cost of
some extra maintenance operations.

Again, this technique is well known for cases where the struct elements can't
be packed together. In fact, Go's map implementation does exactly this for the
same reason:
https://github.com/golang/go/blob/7feb68728dda2f9d86c0a1158307212f5a4297ce/src/runtime/map.go#L156

## Measuring performance

With three implementation, it's probably a good idea to measure performance to
compare them. My test system is an old Intel i5-6260U desktop, running Ubuntu
22.04 with a 5.15 kernel.

To compare the implementations, I wrote a basic sparse-ish vector
implementation to make use of sparse 256-element array:
```go
type Sparse256Array interface {
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

func (v *SparseishVector) Put(i int, val interface{}) {
  v.blocks[i/256].Put(uint8(i), val)
}

func (v *SparseishVector) Get(i int) interface{} {
  return v.blocks[i/256].Get(uint8(i))
}
```

The goal here is to be a testbed for the various array implementations, and
not something that would actually be used in real-life.

With this, I create a vector with a capacity of 50 million elements, and
randomly populate it to between 1% and 99% capacity. Then, do a lookup on those
populated elements:
| Percent Full | MapArray | BinaryArray | SplitBinaryArray |
| --- | --- | --- | --- |
| 1 | 280.1 ns/op | 202.9 ns/op | 219.5 ns/op |
| 5 | 303.7 ns/op | 349.0 ns/op | 352.6 ns/op |
| 10 | 320.8 ns/op | 423.6 ns/op | 374.9 ns/op |
| 25 | 340.6 ns/op | 520.6 ns/op | 400.4 ns/op |
| 50 | 370.7 ns/op | 563.9 ns/op | 418.1 ns/op |
| 75 | 351.1 ns/op | 586.2 ns/op | 433.1 ns/op |
| 90 | 353.1 ns/op | 593.0 ns/op | 453.9 ns/op |
| 95 | 352.3 ns/op | 585.0 ns/op | 431.0 ns/op |
| 99 | 363.5 ns/op | 595.3 ns/op | 428.6 ns/op |

(All the benchmarking code can be found at TODO(insert URL))

As expected, the hash table is fast and performance is fairly consistent, due
to the O(1) lookups. Although the hash tables starts off slower, that can
likely be attributed to the overhead of using a hash table (i.e. key hashing).
The two arrays are much slower, as expected, due to binary searching. And both
arrays get slower as the table is filled, since binary search is O(log n) on
the number of elements.

What's interesting is the performance between the two array implementations.
Even though the only difference is that indices and values are split into
separate arrays, for memory savings, there is a very noticable gap in
performance.

## Profiling

We can use Go's built-in profiler to look at where the CPU is being spent
in the two array implementations.

Before looking at the profiles, it needs to be noted that the benchmarks have
very high setup overhead. So I'll only look at the array implementations, and
not the benchmark as a whole.

Let's have a look at Get() performance at 25% capacity:
```
% go test -bench 'Get/^BinaryArray/25' -cpuprofile cpu.prof
goos: linux
goarch: amd64
pkg: github.com/akmistry/random-tests/sparsevec
cpu: Intel(R) Core(TM) i5-6260U CPU @ 1.80GHz
BenchmarkArrayGet/BinaryArray/25%-4            2346938         502.8 ns/op
PASS
ok    github.com/akmistry/random-tests/sparsevec  8.400s
% go tool pprof cpu.prof
Total: 9.01s
ROUTINE ======================== sparsevec.(*BinaryArray).Get
     290ms      1.36s (flat, cum) 15.09% of Total
         .          .    101:   a.items[index].index = i
         .          .    102:   a.items[index].v = v
         .          .    103: }
         .          .    104:}
         .          .    105:
      20ms       20ms    106:func (a *BinaryArray) Get(i uint8) interface{} {
     270ms      1.34s    107: index := sort.Search(len(a.items), func(n int) bool {
         .          .    108:   return a.items[n].index >= i
         .          .    109: })
         .          .    110: if index < len(a.items) && a.items[index].index == i {
         .          .    111:   return a.items[index].v
         .          .    112: }
ROUTINE ======================== sparsevec.(*BinaryArray).Get.func1
     850ms      850ms (flat, cum)  9.43% of Total
         .          .    102:   a.items[index].v = v
         .          .    103: }
         .          .    104:}
         .          .    105:
         .          .    106:func (a *BinaryArray) Get(i uint8) interface{} {
      10ms       10ms    107: index := sort.Search(len(a.items), func(n int) bool {
     840ms      840ms    108:   return a.items[n].index >= i
         .          .    109: })
         .          .    110: if index < len(a.items) && a.items[index].index == i {
         .          .    111:   return a.items[index].v
         .          .    112: }
         .          .    113: return nil
```

We can see binary searching dominating performance. But looking at the split
array, it tells a slightly different story:
```
% go test -bench 'Get/^SplitBinaryArray/25' -cpuprofile cpu.prof
goos: linux
goarch: amd64
pkg: github.com/akmistry/random-tests/sparsevec
cpu: Intel(R) Core(TM) i5-6260U CPU @ 1.80GHz
BenchmarkArrayGet/SplitBinaryArray/25%-4           3011503         402.1 ns/op
PASS
ok    github.com/akmistry/random-tests/sparsevec  8.171s
% go tool pprof cpu.prof
...
(pprof) list SplitBinaryArray
Total: 8.30s
ROUTINE ======================== sparsevec.(*SplitBinaryArray).Get
     770ms      1.45s (flat, cum) 17.47% of Total
         .          .    146:   a.values[index] = v
         .          .    147: }
         .          .    148:}
         .          .    149:
         .          .    150:func (a *SplitBinaryArray) Get(i uint8) interface{} {
     330ms      1.01s    151: index := sort.Search(len(a.indexes), func(n int) bool {
         .          .    152:   return a.indexes[n] >= i
         .          .    153: })
      30ms       30ms    154: if index < len(a.indexes) && a.indexes[index] == i {
     410ms      410ms    155:   return a.values[index]
         .          .    156: }
         .          .    157: return nil
         .          .    158:}
         .          .    159:
         .          .    160:type BitmapArray struct {
ROUTINE ======================== sparsevec.(*SplitBinaryArray).Get.func1
     440ms      440ms (flat, cum)  5.30% of Total
         .          .    146:   a.values[index] = v
         .          .    147: }
         .          .    148:}
         .          .    149:
         .          .    150:func (a *SplitBinaryArray) Get(i uint8) interface{} {
      30ms       30ms    151: index := sort.Search(len(a.indexes), func(n int) bool {
     410ms      410ms    152:   return a.indexes[n] >= i
         .          .    153: })
         .          .    154: if index < len(a.indexes) && a.indexes[index] == i {
         .          .    155:   return a.values[index]
         .          .    156: }
         .          .    157: return nil
```

Here, although binary searching still dominates, it's only ~70% of the time as
opposed to ~98%. And since SplitBinaryArray is generally faster, the binary
search itself is also taking less time absolutely.

And for comparison, lets have a look at the map version:
```
% go test -bench 'Get/^MapArray/25' -cpuprofile cpu.prof
goos: linux
goarch: amd64
pkg: github.com/akmistry/random-tests/sparsevec
cpu: Intel(R) Core(TM) i5-6260U CPU @ 1.80GHz
BenchmarkArrayGet/MapArray/25%-4           3579357         343.9 ns/op
PASS
ok    github.com/akmistry/random-tests/sparsevec  9.868s
% go tool pprof cpu.prof
(pprof) list MapArray
Total: 11.45s
ROUTINE ======================== sparsevec.(*MapArray).Get
      60ms      1.42s (flat, cum) 12.40% of Total
         .          .     65: } else {
         .          .     66:   a.m[i] = v
         .          .     67: }
         .          .     68:}
         .          .     69:
      10ms       10ms     70:func (a *MapArray) Get(i uint8) interface{} {
      50ms      1.41s     71: return a.m[i]
         .          .     72:}
         .          .     73:
         .          .     74:type binaryArrayItem struct {
         .          .     75: index uint8
         .          .     76: v     interface{}
```

... which doesn't really tell us anything. We'd have to have a closer look at
the map implementation (`runtime.mapaccess1`), but that's a topic for another
day.

I'll skip to what I believe is the reason for the performance gap... CPU
caches. By locating all the indices together, we're creating spacial locallity
and increasing the rate of cache hits when doing the binary search. In this
benchmark, on average, each sparse 256-element array contains 64 elements (at
25% capacity). A cache line on my x86-64 CPU is 64 bytes, hence the binary
search of indices will usually only hit a single cache line (the average should
be just over 1). Even in the worst case of 256 elements, the entire array of
indices will only be 256 bytes, which spans 4 cache lines. And the binary
search will only hit at most 3 cache lines.

Compare this to the first array implementation, which keeps index and values
together. The pair structure, as notes above, is 24 bytes, and only 2 of these
will fit in a cache line. A binary search of 64 elements needs to read 6 of
them. Since the pairs are contiguous in memory, the final two comparisons may
share a cache line, but the rest will not. Hence the binary search will result
in hitting 5 cache lines. Compared to 1 in the split array case, it's clear
why the split array is a winner in terms of performance.

What I've said above is theory, but we can measure this in practice using
the perf tool which is part of the Linux kernel source:
```
% perf stat -d -- go test -bench 'Get/^BinaryArray/25'
...
BenchmarkArrayGet/BinaryArray/25%-4            2424010         494.0 ns/op
...
        46,498,378      LLC-loads                 #    4.308 M/sec                    (50.20%)
        36,644,241      LLC-load-misses           #   78.81% of all LL-cache accesses  (50.08%)
...
% perf stat -d -- go test -bench 'Get/^SplitBinaryArray/25'
...
BenchmarkArrayGet/SplitBinaryArray/25%-4           3099848         393.9 ns/op
...
        37,477,212      LLC-loads                 #    3.715 M/sec                    (50.39%)
        21,858,623      LLC-load-misses           #   58.33% of all LL-cache accesses  (50.04%)
...
```

I noted above these benchmarks have a high setup overhead, so I'll establish a
baseline by commenting out the Get() calls from the benchmark:
```
 % perf stat -d -- go test -bench 'Get/^BinaryArray/25'
...
BenchmarkArrayGet/BinaryArray/25%-4           100000000         10.02 ns/op
...
        12,022,320      LLC-loads                 #    1.182 M/sec                    (50.04%)
         3,956,669      LLC-load-misses           #   32.91% of all LL-cache accesses  (50.34%)
...
% perf stat -d -- go test -bench 'Get/^SplitBinaryArray/25'
...
BenchmarkArrayGet/SplitBinaryArray/25%-4          100000000         10.04 ns/op
...
        11,113,549      LLC-loads                 #    1.160 M/sec                    (50.45%)
         3,204,466      LLC-load-misses           #   28.83% of all LL-cache accesses  (50.48%)
...
```

We can see that even though the split array benchmark does more operations, it
results in fewer LLC (last-level-cache, the L3 cache on this CPU) cache misses.
We can do a back-of-the-envelope calculation to see cache-misses per op:
|  | Ops | LLC-misses | Baseline | LLC-misses - baseline | Misses/op |
| --- | --- | --- | --- | --- | --- |
| BinaryArray | 2424010 | 36644241 | 3956669 | 32687572 | 13.48491631635183 |
| SplitBinaryArray | 3099848 | 21858623 | 3204466 | 18654157 | 6.01776506460962 |

As expected, there is a significant difference in misses/op between the two.
Note that in both cases, the misses/op is much higher than expected. There are
lots of other things going on in the benchmarking framework, Go runtime, other
system activity (like me writing this post in vim). And also keep in mind that
the reported number of operations is NOT the total number, but rather the
number of operations in the final benchmark loop. These numbers should be
compared to each other, not taken as absolute values (I'm not doing a rigorous
analysis here).

It's important to note why there are so many cache misses. This is intentional
by design of the benchmark. I noted that the sparse vector has a capacity of
50 million elements. The vector of 50 million is split into blocks of 256 elements,
resulting in an array of 195313 pointers. Each pointer to the 256 element block
is 16 bytes, since they are interfaces, so the array consumes 3125008 bytes
(~3 MB). This is almost the entire L3 cache on this CPU, leaving little room
for the sparse arrays. Since element are accessed randomly, and we're doing a
very hand wavey analysis, we can assume that the first level in the sparse
vector causes no cache misses, but every access of the sparse 256-element array
causes a cache miss.

## A different approach?

Since binary searching is expensive, and I want to maintain ordering, I
wondered if there was another way to go. My first thought was to use a
present/not-present bitmap to avoid searching for an element that isn't in the
array, but that has limited utility since negative lookups are relatively in my
usage.

But thinking more about using the bitmap, I realised it could be used to avoid
the binary search entirely.

Quickly, for a 256-element array, the bitmap would be 256 entries, one for each
element of the array. A bitmap entry would be true if the corresponding index
existed in the array, and false otherwise. The first observation is that the
count of true bits in the bitmap (the "population count") is the same as the
number of elements in the sparse array. The second observation follows from the
first. For any [0, N) subset of the array, the number of elements in the sparse
array is the count of true bits in the [0, N) subset of the bitmap. This feels
obvious when spelled out. But that means the element at for array index N is at
position true count [0, N) in the sparse array.

We can look at this property by induction. For array element 0, it is always at
position 0 in the sparse array. This is the base case. For array element 1, if
array element 0 exist in the sparse array, element 1 will be at position 1.
This is the case of a "normal" dense array. But if element 0 is missing, it will
be at position 0 instead. This leads to the inductive case:
```
Let 'i' be the index into the 256-element array.
Let S(i) be the index into the sparse array correspoding to element 'i'
Let B(i) be the bitmap entry for element 'i':
    True if the element exists,
    False otherwise

S(i+i) = S(i) + { 1 if B(i) == true
                { 0 if B(i) == false
```

Coding this up is pretty straight forward. Looking at lookup performance:
| Percent Full | MapArray | BinaryArray | SplitBinaryArray | BitmapArray |
| --- | --- | --- | --- | --- |
| 1 | 280.1 ns/op | 202.9 ns/op | 219.5 ns/op | 159.1 ns/op |
| 5 | 303.7 ns/op | 349.0 ns/op | 352.6 ns/op | 174.3 ns/op |
| 10 | 320.8 ns/op | 423.6 ns/op | 374.9 ns/op | 172.3 ns/op |
| 25 | 340.6 ns/op | 520.6 ns/op | 400.4 ns/op | 190.4 ns/op |
| 50 | 370.7 ns/op | 563.9 ns/op | 418.1 ns/op | 210.1 ns/op |
| 75 | 351.1 ns/op | 586.2 ns/op | 433.1 ns/op | 215.1 ns/op |
| 90 | 353.1 ns/op | 593.0 ns/op | 453.9 ns/op | 208.9 ns/op |
| 95 | 352.3 ns/op | 585.0 ns/op | 431.0 ns/op | 204.4 ns/op |
| 99 | 363.5 ns/op | 595.3 ns/op | 428.6 ns/op | 208.2 ns/op |

Lookup performance is consistently better than the other three implementations.
Although the lack of binary search is a big reason for the performance
difference, another big part is the cache efficiency. A 256-element bitmap
consumes 32 bytes (256 / 8 bits/byte). In Go, a slice is 24 bytes. Therefore,
the bitmap plus slice can fit in a single cache line (with room to spare).
Determining the position in the sparse array of an element only accesses a
single cache line, in all cases. Retrieving the element itself is another 1
cache line access. So we have at most 2 misses, always. We can see this
in the perf tool:
|  | Ops | LLC-misses | Baseline | LLC-misses - baseline | Misses/op |
| --- | --- | --- | --- | --- | --- |
| MapArray | 3570840 | 29920283 | 5538842 | 24381441 | 6.8279287226534935 |
| BinaryArray | 2424010 | 36644241 | 3956669 | 32687572 | 13.48491631635183 |
| SplitBinaryArray | 3099848 | 21858623 | 3204466 | 18654157 | 6.01776506460962 |
| BitmapArray | 6534843 | 25315941 | 2918852 | 22397089 | 3.4273339084045324 |

## Caveats

There are a few big gotchas to what I've written. The biggest one is that I've
only considered lookup performance. Insertion and deletion are are also
important. The performance of these operations is affected by slightly
different factors (i.e. memcpy() performance), but some aspects are the same
(i.e. binary search cost). Since my application is dominated by lookup
performance, I've chosen to focus on that.

Another factor is that my benchmark CPU is ~7 years old. CPUs today have bigger
caches, improved performance on certain operations (i.e. memcpy), and are just
generally better. The performance trade-offs might be different.

Lastly, a 256-element sparse array is a VERY specialised data structure with
specific use cases. I don't expect what I've written to be generally applicable.

## Conclusion

Do I need to have a conclusion? Sorry, I don't have one. Well, maybe that
general purpose data structures are just that, general purpose. You might be
able to do better if you can specialise to your needs. But beware, it might not
be reusable for any other case.
