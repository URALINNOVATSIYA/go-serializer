package serializer

import (
	"fmt"
	"reflect"
	"strconv"
	"testing"
	"unsafe"
)

type (
	testBool          bool
	testStr           string
	testUint8         uint8
	testInt8          int8
	testUint16        uint16
	testInt16         int16
	testUint32        uint32
	testInt32         int32
	testUint64        uint64
	testInt64         int64
	testUint          uint
	testInt           int
	testUintptr       uintptr
	testUnsafePointer unsafe.Pointer
	testFloat32       float32
	testFloat64       float64
	testComplex64     complex64
	testComplex128    complex128
	testChan          <-chan *bool
	testInterface     any
	testArray         [3]int
	testSlice         []string
	testRecSlice      []testRecSlice
	testMap           map[string]uint64
	testRecMap        map[byte]testRecMap
	testBoolPtr       *bool
	testRecPtr        *testRecPtr
	testS1            struct {
		F1 any
	}
	testS2 struct {
		F1 any
		F2 any
	}
	testS3 struct {
		F1 any
		F2 any
		F3 any
	}
	testS4 struct {
		F1 any
		F2 any
		F3 any
		F4 any
	}
	testS3Bool struct {
		F1 *bool
		F2 any
		F3 any
	}
	testS3Any struct {
		F1 *any
		F2 any
		F3 any
	}
	testS1Ptr struct {
		F1 *testS1Ptr
	}
	testNestedS1 struct {
		f1 testS1
		f2 testS1Ptr
		testS2
	}
	testS1Tags struct {
		f1 int    `codec:"id=20"`
		f2 bool   `codec:"id=40"`
		F3 string `codec:"id=30"`
		F4 byte
		f5 string `codec:"id=10"`
	}
	taggedChild struct {
		Value   int `codec:"id=21"`
		Ignored string
	}
	taggedRecord struct {
		Ignored    string
		Child      *taggedChild `codec:"id=40"`
		Deprecated bool         `codec:"id=5,deprecated"`
		Any        any          `codec:"id=10"`
	}
	graphNode struct {
		Value int
		Next  *graphNode
		Prev  *graphNode
		Any   any
	}
	graphMixed struct {
		Node  *graphNode
		Items []any
		Table map[string]any
		Box   any
	}
	graphArray struct {
		A [4]int
		P *int
	}
	channelHolder struct {
		A chan int
		B chan int
	}
)

type testFunc struct {
	F int
}

func (a testFunc) Print() int {
	return a.F
}

type testSerializableInt int

func (i testSerializableInt) Serialize() []byte {
	return []byte(strconv.Itoa(int(i)))
}

func (i testSerializableInt) Unserialize(b []byte) (any, error) {
	return strconv.Atoi(string(b))
}

type testSerializableBool bool

func (b *testSerializableBool) Serialize() []byte {
	if *b {
		return []byte{1}
	}
	return []byte{0}
}

func (flag *testSerializableBool) Unserialize(data []byte) (any, error) {
	if len(data) != 1 {
		return nil, fmt.Errorf("invalid Flag data length: %d", len(data))
	}
	return testSerializableBool(data[0] == 1), nil
}

type testSerializablePointerInt int

func (i *testSerializablePointerInt) Serialize() []byte {
	return []byte{byte(*i + 1)}
}

func (i *testSerializablePointerInt) Unserialize(data []byte) (any, error) {
	return testSerializablePointerInt(data[0]), nil
}

type testSerializableGraph struct {
	node *int
}

func (g testSerializableGraph) Serialize() []byte {
	return []byte{byte(*g.node)}
}

func (m testSerializableGraph) Unserialize(b []byte) (any, error) {
	n := int(b[0])
	return testSerializableGraph{node: &n}, nil
}

type testNode struct {
	prev *testNode
	next *testNode
	lst  *lst
}

type lst struct {
	root testNode
}

func newLst() *lst {
	l := &lst{}
	l.root.next = &l.root
	l.root.prev = &l.root
	return l
}

func (l *lst) push() *testNode {
	at := l.root.prev
	el := &testNode{}
	el.prev = at
	el.next = at.next
	el.prev.next = el
	el.next.prev = el
	el.lst = l
	return el
}

func testSum(a, b int) int {
	return a + b
}

func testDiv(a, b int) int {
	return a / b
}

func Test_TypeIdByValue(t *testing.T) {
	items := []struct {
		value       any
		encodedType uint16
	}{
		{nil, 1},
		{false, 2},
		{true, 2},
		{"", 3},
		{0, 4},
		{(*any)(nil), 5},
		{(*bool)(nil), 6},
		{(*[]any)(nil), 7},
		{[]struct{}{}, 8},
		{(*int)(nil), 9},
		{testBoolPtr(nil), 10},
		{(*bool)(nil), 6},
		{testRecPtr(nil), 11},
	}
	reg := NewTypeRegistry(true)
	for i, item := range items {
		expected := item.encodedType
		actual := reg.typeIdByValue(reflect.ValueOf(item.value))
		if actual.id != expected {
			t.Errorf("type of value #%d (%#v) must be encoded as %#v, but received %#v", i+1, item.value, expected, actual)
		}
	}
}
