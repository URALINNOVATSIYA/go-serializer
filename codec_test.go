package serializer

import (
	"bytes"
	"errors"
	"fmt"

	"sync"

	"math"
	"reflect"
	"strings"
	"testing"
	"unsafe"

	"github.com/URALINNOVATSIYA/go-serializer/testpkg"
	"github.com/URALINNOVATSIYA/reflex"
)

type eq func(any, any) bool

type testItem struct {
	value any
	data  []byte
	eq    eq
}

func runTests(items []testItem, typeRegistry *TypeRegistry, t *testing.T) {
	t.Helper()
	serializer := NewSerializer().WithTypeRegistry(typeRegistry)
	unserializer := NewUnserializer().WithTypeRegistry(typeRegistry)
	for i, item := range items {
		expected := item.value
		data := serializer.Encode(expected)
		if item.data != nil && !bytes.Equal(data, item.data) {
			t.Errorf("Test #%d: Encode(%T) must return %v, but actual value is %v", i+1, expected, item.data, data)
			continue
		}
		actual, err := unserializer.Decode(data)
		if err != nil {
			t.Errorf("Test #%d: Decode(%T) raises error: %q", i+1, expected, err)
		} else if res, err := equal(item.eq, expected, actual); res == false {
			t.Errorf("Test #%d: Decode(%T) returns wrong value %T (check err: %v)", i+1, expected, actual, err)
		}
	}
}

func equal(customEq eq, expectd, actual any) (result bool, err error) {
	defer func() {
		if e := recover(); e != nil {
			err = fmt.Errorf("%s", e)
		}
	}()
	if customEq == nil {
		return defaultEq(expectd, actual), err
	}
	return customEq(expectd, actual), err
}

func defaultEq(expected, actual any) bool {
	if expected != nil {
		switch reflect.TypeOf(expected).Kind() {
		case reflect.Chan:
			return chanEqual(expected, actual)
		case reflect.Func:
			return funcEqual(expected, actual)
		}
	}
	return reflect.DeepEqual(expected, actual)
}

func chanEqual(expected any, actual any) bool {
	if reflex.NameOf(reflect.TypeOf(expected)) != reflex.NameOf(reflect.TypeOf(actual)) {
		return false
	}
	return reflect.ValueOf(expected).Cap() == reflect.ValueOf(actual).Cap()
}

func funcEqual(expected any, actual any) bool {
	return reflex.FuncNameOf(reflect.ValueOf(expected)) == reflex.FuncNameOf(reflect.ValueOf(actual))
}

func registry() (*TypeRegistry, func(v any) byte) {
	reg := NewTypeRegistry(true)
	return reg, func(v any) byte {
		tid := reg.typeIdByValue(reflect.ValueOf(v))
		return t2b0(tid.id)
	}
}

func registryWithFuncId() (*TypeRegistry, func(v any) byte, func(v any) byte) {
	reg := NewTypeRegistry(true)
	return reg, func(v any) byte {
			tid := reg.typeIdByValue(reflect.ValueOf(v))
			return t2b0(tid.id)
		},
		func(v any) byte {
			tid := reg.funcIdByValue(reflect.ValueOf(v))
			return t2b0(tid.id)
		}
}

func Test_Nil(t *testing.T) {
	reg, typeId := registry()
	items := []testItem{
		{
			nil,
			[]byte{version, typeId(nil), meta_nil},
			nil,
		},
	}
	runTests(items, reg, t)
}

func Test_Bool(t *testing.T) {
	reg, typeId := registry()
	items := []testItem{
		{
			false,
			[]byte{version, typeId(false), meta_fls},
			nil,
		},
		{
			true,
			[]byte{version, typeId(false), meta_tru},
			nil,
		},
		{
			testBool(false),
			[]byte{version, typeId(testBool(false)), meta_fls},
			nil,
		},
		{
			testBool(true),
			[]byte{version, typeId(testBool(false)), meta_tru},
			nil,
		},
	}
	runTests(items, reg, t)
}

func Test_String(t *testing.T) {
	reg, typeId := registry()
	items := []testItem{
		// #1
		{
			"",
			[]byte{version, typeId(""), c2b0(0)},
			nil,
		},
		// #2
		{
			"0123456789",
			[]byte{version, typeId(""), c2b0(10), '0', '1', '2', '3', '4', '5', '6', '7', '8', '9'},
			nil,
		},
		// #3
		{
			strings.Repeat("a", 255),
			append(append([]byte{version, typeId("")}, c2b(nil, 255)...), []byte(strings.Repeat("a", 255))...),
			nil,
		},
		// #4
		{
			strings.Repeat("a", 65536),
			append(append([]byte{version, typeId("")}, c2b(nil, 65536)...), []byte(strings.Repeat("a", 65536))...),
			nil,
		},
		// #5
		{
			testStr("abcd"),
			[]byte{version, typeId(testStr("")), c2b0(4), 97, 98, 99, 100},
			nil,
		},
		// #6
		{
			func() any {
				data := []byte("abcdef")
				text := unsafe.String(unsafe.SliceData(data), len(data))
				return struct {
					Text  string
					Bytes []byte
				}{text, data}
			}(),
			nil,
			func(_, actual any) bool {
				a := actual.(struct {
					Text  string
					Bytes []byte
				})
				if a.Text != "abcdef" || string(a.Bytes) != "abcdef" {
					return false
				}
				a.Bytes[0] = 'z'
				return a.Text[0] == 'z'
			},
		},
		// #7
		{
			func() any {
				data := []byte("abcdef")
				text := unsafe.String(unsafe.SliceData(data), len(data))
				return struct {
					Bytes []byte
					Text  string
				}{data, text}
			}(),
			nil,
			func(_, actual any) bool {
				a := actual.(struct {
					Bytes []byte
					Text  string
				})
				if a.Text != "abcdef" || string(a.Bytes) != "abcdef" {
					return false
				}
				a.Bytes[0] = 'z'
				return a.Text[0] == 'z'
			},
		},
		// #8
		{
			func() any {
				b := []byte("0123456789")
				s := unsafe.String(unsafe.SliceData(b), len(b))
				return [3]any{b, s, s[2:7]}
			}(),
			nil,
			func(expected, actual any) bool {
				if !defaultEq(expected, actual) {
					return false
				}
				a := actual.([3]any)
				if a[1] != "0123456789" {
					return false
				}
				if a[2] != "23456" {
					return false
				}
				(a[0].([]byte))[2] = 'X'
				return a[2] == "X3456"
			},
		},
	}
	runTests(items, reg, t)
}

func Test_Uint8(t *testing.T) {
	reg, typeId := registry()
	items := []testItem{
		{
			uint8(0),
			[]byte{version, typeId(uint8(0)), 0},
			nil,
		},
		{
			uint8(1),
			[]byte{version, typeId(uint8(0)), 1},
			nil,
		},
		{
			uint8(255),
			[]byte{version, typeId(uint8(0)), 255},
			nil,
		},
		{
			testUint8(123),
			[]byte{version, typeId(testUint8(0)), 123},
			nil,
		},
	}
	runTests(items, reg, t)
}

func Test_Int8(t *testing.T) {
	reg, typeId := registry()
	items := []testItem{
		{
			int8(0),
			[]byte{version, typeId(int8(0)), 0},
			nil,
		},
		{
			int8(1),
			[]byte{version, typeId(int8(0)), 2},
			nil,
		},
		{
			int8(-1),
			[]byte{version, typeId(int8(0)), 1},
			nil,
		},
		{
			int8(127),
			[]byte{version, typeId(int8(0)), 254},
			nil,
		},
		{
			int8(-128),
			[]byte{version, typeId(int8(0)), 255},
			nil,
		},
		{
			testInt8(123),
			[]byte{version, typeId(testInt8(0)), 246},
			nil,
		},
	}
	runTests(items, reg, t)
}

func Test_Uint16(t *testing.T) {
	reg, typeId := registry()
	items := []testItem{
		{
			uint16(0),
			[]byte{version, typeId(uint16(0)), 0b0100_0000},
			nil,
		},
		{
			uint16(1),
			[]byte{version, typeId(uint16(0)), 0b0100_0000 | 1},
			nil,
		},
		{
			uint16(256),
			[]byte{version, typeId(uint16(0)), 0b1000_0000 | 1, 0},
			nil,
		},
		{
			uint16(65535),
			[]byte{version, typeId(uint16(0)), 0b1100_0000, 255, 255},
			nil,
		},
		{
			testUint16(12345),
			[]byte{version, typeId(testUint16(0)), 0b1000_0000 | 48, 57},
			nil,
		},
	}
	runTests(items, reg, t)
}

func Test_Int16(t *testing.T) {
	reg, typeId := registry()
	items := []testItem{
		{
			int16(0),
			[]byte{version, typeId(int16(0)), 0b0100_0000},
			nil,
		},
		{
			int16(1),
			[]byte{version, typeId(int16(0)), 0b0100_0000 | 2},
			nil,
		},
		{
			int16(-1),
			[]byte{version, typeId(int16(0)), 0b0100_0000 | 1},
			nil,
		},
		{
			int16(256),
			[]byte{version, typeId(int16(0)), 0b1000_0000 | 2, 0},
			nil,
		},
		{
			int16(-256),
			[]byte{version, typeId(int16(0)), 0b1000_0000 | 1, 255},
			nil,
		},
		{
			int16(32767),
			[]byte{version, typeId(int16(0)), 0b1100_0000, 255, 254},
			nil,
		},
		{
			int16(-32768),
			[]byte{version, typeId(int16(0)), 0b1100_0000, 255, 255},
			nil,
		},
		{
			testInt16(-12345),
			[]byte{version, typeId(testInt16(0)), 0b1100_0000, 96, 113},
			nil,
		},
	}
	runTests(items, reg, t)
}

func Test_Uint32(t *testing.T) {
	reg, typeId := registry()
	items := []testItem{
		{
			uint32(0),
			[]byte{version, typeId(uint32(0)), 0b0010_0000},
			nil,
		},
		{
			uint32(1),
			[]byte{version, typeId(uint32(0)), 0b0010_0000 | 1},
			nil,
		},
		{
			uint32(256),
			[]byte{version, typeId(uint32(0)), 0b0100_0000 | 1, 0},
			nil,
		},
		{
			uint32(123456),
			[]byte{version, typeId(uint32(0)), 0b0110_0000 | 1, 226, 64},
			nil,
		},
		{
			uint32(math.MaxUint32),
			[]byte{version, typeId(uint32(0)), 0b1010_0000, 255, 255, 255, 255},
			nil,
		},
		{
			testUint32(12345678),
			[]byte{version, typeId(testUint32(0)), 0b1000_0000, 188, 97, 78},
			nil,
		},
	}
	runTests(items, reg, t)
}

func Test_Int32(t *testing.T) {
	reg, typeId := registry()
	items := []testItem{
		{
			int32(0),
			[]byte{version, typeId(int32(0)), 0b0010_0000},
			nil,
		},
		{
			int32(1),
			[]byte{version, typeId(int32(0)), 0b0010_0000 | 2},
			nil,
		},
		{
			int32(-1),
			[]byte{version, typeId(int32(0)), 0b0010_0000 | 1},
			nil,
		},
		{
			int32(256),
			[]byte{version, typeId(int32(0)), 0b0100_0000 | 2, 0},
			nil,
		},
		{
			int32(-256),
			[]byte{version, typeId(int32(0)), 0b0100_0000 | 1, 255},
			nil,
		},
		{
			int32(123456),
			[]byte{version, typeId(int32(0)), 0b0110_0000 | 3, 196, 128},
			nil,
		},
		{
			int32(-123456),
			[]byte{version, typeId(int32(0)), 0b0110_0000 | 3, 196, 127},
			nil,
		},
		{
			int32(math.MaxInt32),
			[]byte{version, typeId(int32(0)), 0b1010_0000, 255, 255, 255, 254},
			nil,
		},
		{
			int32(math.MinInt32),
			[]byte{version, typeId(int32(0)), 0b1010_0000, 255, 255, 255, 255},
			nil,
		},
		{
			testInt32(1234567),
			[]byte{version, typeId(testInt32(0)), 0b1000_0000, 37, 173, 14},
			nil,
		},
	}
	runTests(items, reg, t)
}

func Test_Uint64(t *testing.T) {
	reg, typeId := registry()
	items := []testItem{
		{
			uint64(0),
			[]byte{version, typeId(uint64(0)), 0b0001_0000},
			nil,
		},
		{
			uint64(1),
			[]byte{version, typeId(uint64(0)), 0b0001_0000 | 1},
			nil,
		},
		{
			uint64(1 << 8),
			[]byte{version, typeId(uint64(0)), 0b0010_0000 | 1, 0},
			nil,
		},
		{
			uint64(1 << 16),
			[]byte{version, typeId(uint64(0)), 0b0011_0000 | 1, 0, 0},
			nil,
		},
		{
			uint64(1 << 24),
			[]byte{version, typeId(uint64(0)), 0b0100_0000 | 1, 0, 0, 0},
			nil,
		},
		{
			uint64(1 << 32),
			[]byte{version, typeId(uint64(0)), 0b0101_0000 | 1, 0, 0, 0, 0},
			nil,
		},
		{
			uint64(1 << 40),
			[]byte{version, typeId(uint64(0)), 0b0110_0000 | 1, 0, 0, 0, 0, 0},
			nil,
		},
		{
			uint64(1 << 48),
			[]byte{version, typeId(uint64(0)), 0b0111_0000 | 1, 0, 0, 0, 0, 0, 0},
			nil,
		},
		{
			uint64(1 << 56),
			[]byte{version, typeId(uint64(0)), 0b1000_0000 | 1, 0, 0, 0, 0, 0, 0, 0},
			nil,
		},
		{
			uint64(1<<64 - 1),
			[]byte{version, typeId(uint64(0)), 0b1001_0000, 255, 255, 255, 255, 255, 255, 255, 255},
			nil,
		},
		{
			testUint64(1234567890),
			[]byte{version, typeId(testUint64(0)), 0b0101_0000, 73, 150, 2, 210},
			nil,
		},
	}
	runTests(items, reg, t)
}

func Test_Int64(t *testing.T) {
	reg, typeId := registry()
	items := []testItem{
		{
			int64(0),
			[]byte{version, typeId(int64(0)), 0b0001_0000},
			nil,
		},
		{
			int64(1),
			[]byte{version, typeId(int64(0)), 0b0001_0000 | 2},
			nil,
		},
		{
			int64(-1),
			[]byte{version, typeId(int64(0)), 0b0001_0000 | 1},
			nil,
		},
		{
			int64(1 << 8),
			[]byte{version, typeId(int64(0)), 0b0010_0000 | 2, 0},
			nil,
		},
		{
			int64(-1 << 8),
			[]byte{version, typeId(int64(0)), 0b0010_0000 | 1, 255},
			nil,
		},
		{
			int64(1 << 16),
			[]byte{version, typeId(int64(0)), 0b0011_0000 | 2, 0, 0},
			nil,
		},
		{
			int64(-1 << 16),
			[]byte{version, typeId(int64(0)), 0b0011_0000 | 1, 255, 255},
			nil,
		},
		{
			int64(1 << 24),
			[]byte{version, typeId(int64(0)), 0b0100_0000 | 2, 0, 0, 0},
			nil,
		},
		{
			int64(-1 << 24),
			[]byte{version, typeId(int64(0)), 0b0100_0000 | 1, 255, 255, 255},
			nil,
		},
		{
			int64(1 << 32),
			[]byte{version, typeId(int64(0)), 0b0101_0000 | 2, 0, 0, 0, 0},
			nil,
		},
		{
			int64(-1 << 32),
			[]byte{version, typeId(int64(0)), 0b0101_0000 | 1, 255, 255, 255, 255},
			nil,
		},
		{
			int64(1 << 40),
			[]byte{version, typeId(int64(0)), 0b0110_0000 | 2, 0, 0, 0, 0, 0},
			nil,
		},
		{
			int64(-1 << 40),
			[]byte{version, typeId(int64(0)), 0b0110_0000 | 1, 255, 255, 255, 255, 255},
			nil,
		},
		{
			int64(1 << 48),
			[]byte{version, typeId(int64(0)), 0b0111_0000 | 2, 0, 0, 0, 0, 0, 0},
			nil,
		},
		{
			int64(-1 << 48),
			[]byte{version, typeId(int64(0)), 0b0111_0000 | 1, 255, 255, 255, 255, 255, 255},
			nil,
		},
		{
			int64(1 << 56),
			[]byte{version, typeId(int64(0)), 0b1000_0000 | 2, 0, 0, 0, 0, 0, 0, 0},
			nil,
		},
		{
			int64(-1 << 56),
			[]byte{version, typeId(int64(0)), 0b1000_0000 | 1, 255, 255, 255, 255, 255, 255, 255},
			nil,
		},
		{
			int64(math.MaxInt64),
			[]byte{version, typeId(int64(0)), 0b1001_0000, 255, 255, 255, 255, 255, 255, 255, 254},
			nil,
		},
		{
			int64(math.MinInt64),
			[]byte{version, typeId(int64(0)), 0b1001_0000, 255, 255, 255, 255, 255, 255, 255, 255},
			nil,
		},
		{
			testInt64(1234567890),
			[]byte{version, typeId(testInt64(0)), 0b0101_0000, 147, 44, 5, 164},
			nil,
		},
	}
	runTests(items, reg, t)
}

func Test_Uint(t *testing.T) {
	reg, typeId := registry()
	items := []testItem{
		{
			uint(0),
			[]byte{version, typeId(uint(0)), 0b0001_0000},
			nil,
		},
		{
			uint(1),
			[]byte{version, typeId(uint(0)), 0b0001_0000 | 1},
			nil,
		},
		{
			uint(1<<8 - 1),
			[]byte{version, typeId(uint(0)), 0b0010_0000, 255},
			nil,
		},
		{
			uint(1 << 8),
			[]byte{version, typeId(uint(0)), 0b0010_0000 | 1, 0},
			nil,
		},
		{
			uint(1<<16 - 1),
			[]byte{version, typeId(uint(0)), 0b0011_0000, 255, 255},
			nil,
		},
		{
			uint(1 << 16),
			[]byte{version, typeId(uint(0)), 0b0011_0000 | 1, 0, 0},
			nil,
		},
		{
			uint(1<<24 - 1),
			[]byte{version, typeId(uint(0)), 0b0100_0000, 255, 255, 255},
			nil,
		},
		{
			uint(1 << 24),
			[]byte{version, typeId(uint(0)), 0b0100_0000 | 1, 0, 0, 0},
			nil,
		},
		{
			uint(1<<32 - 1),
			[]byte{version, typeId(uint(0)), 0b0101_0000, 255, 255, 255, 255},
			nil,
		},
		{
			testUint(12345),
			[]byte{version, typeId(testUint(0)), 0b0011_0000, 48, 57},
			nil,
		},
	}
	if math.MaxUint == math.MaxUint64 {
		items = append(items, []testItem{
			{
				uint(1 << 32),
				[]byte{version, typeId(uint(0)), 0b0101_0000 | 1, 0, 0, 0, 0},
				nil,
			},
			{
				uint(1<<40 - 1),
				[]byte{version, typeId(uint(0)), 0b0110_0000, 255, 255, 255, 255, 255},
				nil,
			},
			{
				uint(1 << 40),
				[]byte{version, typeId(uint(0)), 0b0110_0000 | 1, 0, 0, 0, 0, 0},
				nil,
			},
			{
				uint(1<<48 - 1),
				[]byte{version, typeId(uint(0)), 0b0111_0000, 255, 255, 255, 255, 255, 255},
				nil,
			},
			{
				uint(1 << 48),
				[]byte{version, typeId(uint(0)), 0b0111_0000 | 1, 0, 0, 0, 0, 0, 0},
				nil,
			},
			{
				uint(1<<56 - 1),
				[]byte{version, typeId(uint(0)), 0b1000_0000, 255, 255, 255, 255, 255, 255, 255},
				nil,
			},
			{
				uint(1 << 56),
				[]byte{version, typeId(uint(0)), 0b1000_0000 | 1, 0, 0, 0, 0, 0, 0, 0},
				nil,
			},
			{
				uint(1<<64 - 1),
				[]byte{version, typeId(uint(0)), 0b1001_0000, 255, 255, 255, 255, 255, 255, 255, 255},
				nil,
			},
		}...)
	}
	runTests(items, reg, t)
}

func Test_Int(t *testing.T) {
	reg, typeId := registry()
	items := []testItem{
		{
			0,
			[]byte{version, typeId(0), 0b0001_0000},
			nil,
		},
		{
			1<<7 - 1,
			[]byte{version, typeId(0), 0b0010_0000, 254},
			nil,
		},
		{
			-1 << 7,
			[]byte{version, typeId(0), 0b0010_0000, 255},
			nil,
		},
		{
			1 << 7,
			[]byte{version, typeId(0), 0b0010_0000 | 1, 0},
			nil,
		},
		{
			-1<<7 - 1,
			[]byte{version, typeId(0), 0b0010_0000 | 1, 1},
			nil,
		},
		{
			1<<15 - 1,
			[]byte{version, typeId(0), 0b0011_0000, 255, 254},
			nil,
		},
		{
			-1 << 15,
			[]byte{version, typeId(0), 0b0011_0000, 255, 255},
			nil,
		},
		{
			1 << 15,
			[]byte{version, typeId(0), 0b0011_0000 | 1, 0, 0},
			nil,
		},
		{
			-1<<15 - 1,
			[]byte{version, typeId(0), 0b0011_0000 | 1, 0, 1},
			nil,
		},
		{
			1<<23 - 1,
			[]byte{version, typeId(0), 0b0100_0000, 255, 255, 254},
			nil,
		},
		{
			-1 << 23,
			[]byte{version, typeId(0), 0b0100_0000, 255, 255, 255},
			nil,
		},
		{
			1 << 23,
			[]byte{version, typeId(0), 0b0100_0000 | 1, 0, 0, 0},
			nil,
		},
		{
			-1<<23 - 1,
			[]byte{version, typeId(0), 0b0100_0000 | 1, 0, 0, 1},
			nil,
		},
		{
			testInt(12345),
			[]byte{version, typeId(testInt(0)), 0b0011_0000, 96, 114},
			nil,
		},
	}
	if math.MaxInt == math.MaxInt64 {
		items = append(items, []testItem{
			{
				1<<32 - 1,
				[]byte{version, typeId(0), 0b0101_0000 | 1, 255, 255, 255, 254},
				nil,
			},
			{
				-1 << 32,
				[]byte{version, typeId(0), 0b0101_0000 | 1, 255, 255, 255, 255},
				nil,
			},
			{
				1 << 32,
				[]byte{version, typeId(0), 0b0101_0000 | 2, 0, 0, 0, 0},
				nil,
			},
			{
				-1<<32 - 1,
				[]byte{version, typeId(0), 0b0101_0000 | 2, 0, 0, 0, 1},
				nil,
			},
			{
				1<<40 - 1,
				[]byte{version, typeId(0), 0b0110_0000 | 1, 255, 255, 255, 255, 254},
				nil,
			},
			{
				-1 << 40,
				[]byte{version, typeId(0), 0b0110_0000 | 1, 255, 255, 255, 255, 255},
				nil,
			},
			{
				1 << 40,
				[]byte{version, typeId(0), 0b0110_0000 | 2, 0, 0, 0, 0, 0},
				nil,
			},
			{
				-1<<40 - 1,
				[]byte{version, typeId(0), 0b0110_0000 | 2, 0, 0, 0, 0, 1},
				nil,
			},
			{
				1<<48 - 1,
				[]byte{version, typeId(0), 0b0111_0000 | 1, 255, 255, 255, 255, 255, 254},
				nil,
			},
			{
				-1 << 48,
				[]byte{version, typeId(0), 0b0111_0000 | 1, 255, 255, 255, 255, 255, 255},
				nil,
			},
			{
				1 << 48,
				[]byte{version, typeId(0), 0b0111_0000 | 2, 0, 0, 0, 0, 0, 0},
				nil,
			},
			{
				-1<<48 - 1,
				[]byte{version, typeId(0), 0b0111_0000 | 2, 0, 0, 0, 0, 0, 1},
				nil,
			},
			{
				1<<56 - 1,
				[]byte{version, typeId(0), 0b1000_0000 | 1, 255, 255, 255, 255, 255, 255, 254},
				nil,
			},
			{
				-1 << 56,
				[]byte{version, typeId(0), 0b1000_0000 | 1, 255, 255, 255, 255, 255, 255, 255},
				nil,
			},
			{
				1 << 56,
				[]byte{version, typeId(0), 0b1000_0000 | 2, 0, 0, 0, 0, 0, 0, 0},
				nil,
			},
			{
				-1<<56 - 1,
				[]byte{version, typeId(0), 0b1000_0000 | 2, 0, 0, 0, 0, 0, 0, 1},
				nil,
			},
			{
				math.MaxInt,
				[]byte{version, typeId(0), 0b1001_0000, 255, 255, 255, 255, 255, 255, 255, 254},
				nil,
			},
			{
				math.MinInt,
				[]byte{version, typeId(0), 0b1001_0000, 255, 255, 255, 255, 255, 255, 255, 255},
				nil,
			},
		}...)
	}
	runTests(items, reg, t)
}

func Test_Uintptr(t *testing.T) {
	reg, typeId := registry()
	items := []testItem{
		{
			uintptr(123456),
			[]byte{version, typeId(uintptr(0)), 0b0011_0000 | 1, 226, 64},
			nil,
		},
		{
			testUintptr(12345),
			[]byte{version, typeId(testUintptr(0)), 0b0011_0000, 48, 57},
			nil,
		},
	}
	runTests(items, reg, t)
}

func Test_Float32(t *testing.T) {
	reg, typeId := registry()
	items := []testItem{
		{
			float32(0),
			[]byte{version, typeId(float32(0)), 0b0010_0000},
			nil,
		},
		{
			float32(1),
			[]byte{version, typeId(float32(0)), 0b0110_0000, 128, 63},
			nil,
		},
		{
			float32(10),
			[]byte{version, typeId(float32(0)), 0b0110_0000, 32, 65},
			nil,
		},
		{
			float32(-1),
			[]byte{version, typeId(float32(0)), 0b0110_0000, 128, 191},
			nil,
		},
		{
			float32(-10),
			[]byte{version, typeId(float32(0)), 0b0110_0000, 32, 193},
			nil,
		},
		{
			float32(1.23),
			[]byte{version, typeId(float32(0)), 0b1010_0000, 164, 112, 157, 63},
			nil,
		},
		{
			float32(-1.23),
			[]byte{version, typeId(float32(0)), 0b1010_0000, 164, 112, 157, 191},
			nil,
		},
		{
			testFloat32(123),
			[]byte{version, typeId(testFloat32(0)), 0b0110_0000, 246, 66},
			nil,
		},
	}
	runTests(items, reg, t)
}

func Test_Float64(t *testing.T) {
	reg, typeId := registry()
	items := []testItem{
		{
			float64(0),
			[]byte{version, typeId(float64(0)), 0b0001_0000},
			nil,
		},
		{
			float64(1),
			[]byte{version, typeId(float64(0)), 0b0011_0000, 240, 63},
			nil,
		},
		{
			float64(10),
			[]byte{version, typeId(float64(0)), 0b0011_0000, 36, 64},
			nil,
		},
		{
			float64(-1),
			[]byte{version, typeId(float64(0)), 0b0011_0000, 240, 191},
			nil,
		},
		{
			float64(-10),
			[]byte{version, typeId(float64(0)), 0b0011_0000, 36, 192},
			nil,
		},
		{
			1.23,
			[]byte{version, typeId(float64(0)), 0b1001_0000, 174, 71, 225, 122, 20, 174, 243, 63},
			nil,
		},
		{
			-1.23,
			[]byte{version, typeId(float64(0)), 0b1001_0000, 174, 71, 225, 122, 20, 174, 243, 191},
			nil,
		},
		{
			testFloat64(123),
			[]byte{version, typeId(testFloat64(0)), 0b0100_0000, 192, 94, 64},
			nil,
		},
		{
			math.NaN(),
			[]byte{version, typeId(float64(0)), 129, 0, 0, 0, 0, 0, 248, 127},
			func(_ any, actual any) bool {
				return math.IsNaN(actual.(float64))
			},
		},
		{
			math.Inf(-1),
			[]byte{version, typeId(float64(0)), 48, 240, 255},
			func(_ any, actual any) bool {
				return math.IsInf(actual.(float64), -1)
			},
		},
		{
			math.Inf(1),
			[]byte{version, typeId(float64(0)), 48, 240, 127},
			func(_ any, actual any) bool {
				return math.IsInf(actual.(float64), 1)
			},
		},
	}
	runTests(items, reg, t)
}

func Test_Complex64(t *testing.T) {
	reg, typeId := registry()
	items := []testItem{
		{
			complex(float32(0), float32(0)),
			[]byte{version, typeId(complex64(0)), 0b0010_0000, 0b0010_0000},
			nil,
		},
		{
			complex(float32(1), float32(0)),
			[]byte{version, typeId(complex64(0)), 0b0110_0000, 128, 63, 0b0010_0000},
			nil,
		},
		{
			complex(float32(0), float32(1)),
			[]byte{version, typeId(complex64(0)), 0b0010_0000, 0b0110_0000, 128, 63},
			nil,
		},
		{
			complex(float32(1.23), float32(-1.23)),
			[]byte{version, typeId(complex64(0)), 0b1010_0000, 164, 112, 157, 63, 0b1010_0000, 164, 112, 157, 191},
			nil,
		},
		{
			testComplex64(1 + 2i),
			[]byte{version, typeId(testComplex64(0)), 0b0110_0000, 128, 63, 0b0100_0000, 64},
			nil,
		},
	}
	runTests(items, reg, t)
}

func Test_Complex128(t *testing.T) {
	reg, typeId := registry()
	items := []testItem{
		// #1
		{
			complex(float64(0), float64(0)),
			[]byte{version, typeId(complex128(0)), 0b0001_0000, 0b0001_0000},
			nil,
		},
		// #2
		{
			complex(float64(1), float64(0)),
			[]byte{version, typeId(complex128(0)), 0b0011_0000, 240, 63, 0b0001_0000},
			nil,
		},
		// #3
		{
			complex(float64(0), float64(1)),
			[]byte{version, typeId(complex128(0)), 0b0001_0000, 0b0011_0000, 240, 63},
			nil,
		},
		// #4
		{
			complex(1.23, -1.23),
			[]byte{
				version, typeId(complex128(0)),
				0b1001_0000, 174, 71, 225, 122, 20, 174, 243, 63,
				0b1001_0000, 174, 71, 225, 122, 20, 174, 243, 191,
			},
			nil,
		},
		// #5
		{
			testComplex128(1 + 2i),
			[]byte{version, typeId(testComplex128(0)), 0b0011_0000, 240, 63, 0b0010_0000, 64},
			nil,
		},
		// #6
		{
			complex(math.Inf(1), math.NaN()),
			nil,
			func(expected, actual any) bool {
				n := actual.(complex128)
				return math.IsInf(real(n), 1) && math.IsNaN(imag(n))
			},
		},
	}
	runTests(items, reg, t)
}

func Test_UnsafePointer(t *testing.T) {
	reg, typeId := registry()
	items := []testItem{
		{
			unsafe.Pointer(nil),
			[]byte{version, typeId(unsafe.Pointer(nil)), meta_nil},
			nil,
		},
		{
			unsafe.Pointer(uintptr(0)),
			[]byte{version, typeId(unsafe.Pointer(nil)), meta_nil},
			nil,
		},
		{
			unsafe.Pointer(uintptr(123456)),
			[]byte{version, typeId(unsafe.Pointer(nil)), 0b0011_0000 | 1, 226, 64},
			nil,
		},
		{
			testUnsafePointer(nil),
			[]byte{version, typeId(testUnsafePointer(nil)), meta_nil},
			nil,
		},
		{
			testUnsafePointer(uintptr(12345)),
			[]byte{version, typeId(testUnsafePointer(nil)), 0b0011_0000, 48, 57},
			nil,
		},
	}
	runTests(items, reg, t)
}

func Test_Chan(t *testing.T) {
	reg, typeId := registry()
	items := []testItem{
		// #1
		{
			(<-chan bool)(nil),
			[]byte{version, typeId(make(<-chan bool)), meta_nil},
			nil,
		},
		// #2
		{
			make(chan int),
			[]byte{version, typeId(make(chan int)), meta_nonil, c2b0(0)},
			nil,
		},
		// #3
		{
			make(chan<- bool, 1),
			[]byte{version, typeId(make(chan<- bool)), meta_nonil, c2b0(1), c2b0(0)},
			nil,
		},
		// #4
		{
			make(testChan, 10),
			[]byte{version, typeId(testChan(nil)), meta_nonil, c2b0(10), c2b0(0)},
			nil,
		},
		// #5
		{
			func() any {
				ch := make(chan int, 7)
				return channelHolder{A: ch, B: ch}
			}(),
			nil,
			func(expected, actual any) bool {
				s := actual.(channelHolder)
				return s.A == s.B && cap(s.A) == 7
			},
		},
		// #6
		{
			func() any {
				ch := make(chan bool, 5)
				ch <- true
				ch <- true
				ch <- false
				return ch
			}(),
			nil,
			func(_, actual any) bool {
				ch := actual.(chan bool)
				b1 := <-ch
				b2 := <-ch
				b3 := <-ch
				return b1 && b2 && !b3
			},
		},
	}
	runTests(items, reg, t)
}

func Test_Func(t *testing.T) {
	reg, typeId, funcId := registryWithFuncId()
	items := []testItem{
		// #1
		{
			(func(byte, bool) int8)(nil),
			[]byte{version, typeId((func(byte, bool) int8)(nil)), meta_nil},
			nil,
		},
		// #2
		{
			funcId,
			[]byte{version, typeId(funcId), meta_nonil, funcId(funcId)},
			nil,
		},
		// #3
		{
			math.Abs,
			[]byte{version, typeId(math.Abs), meta_nonil, funcId(math.Abs)},
			nil,
		},
		// #4
		{
			func() any {
				a := testFunc{F: 1}
				b := testFunc{F: 2}
				return [2]func() int{a.Print, b.Print}
			}(),
			nil,
			func(_, actual any) bool {
				a := actual.([2]func() int)
				return a[0]() == 1 && a[1]() == 2
			},
		},
	}
	runTests(items, reg, t)
}

func Test_PointerToSingleValue(t *testing.T) {
	reg, typeId := registry()
	items := []testItem{
		// #1
		{
			(*any)(nil),
			[]byte{version, typeId((*any)(nil)), meta_nil},
			nil,
		},
		// #2
		{
			(*byte)(nil),
			[]byte{version, typeId((*byte)(nil)), meta_nil},
			nil,
		},
		// #3
		{
			func() any {
				b := true
				return &b
			}(),
			nil,
			nil,
		},
		// #4
		{
			func() any {
				b := byte(255)
				return &b
			}(),
			nil,
			nil,
		},
		// #5
		{
			func() any {
				s := "123"
				return &s
			}(),
			nil,
			nil,
		},
		// #6
		{
			func() any {
				var x any = true
				return &x
			}(),
			nil,
			nil,
		},
		// #7
		{
			func() any {
				b := true
				return testBoolPtr(&b)
			}(),
			nil,
			nil,
		},
	}
	runTests(items, reg, t)
}

func Test_PointersToTheSameValue(t *testing.T) {
	reg, _ := registry()
	items := []testItem{
		// #1
		{
			func() any {
				b1 := byte(1)
				b2 := byte(1)
				s := &testS3{}
				s.F1 = &b1
				s.F2 = &b2
				s.F3 = &b1
				return s
			}(),
			nil,
			nil,
		},
		// #2
		{
			func() any {
				b := "abc"
				s := &testS3{}
				s.F2 = &b
				s.F3 = &b
				return s
			}(),
			nil,
			nil,
		},
		// #3
		{
			func() any {
				s := &testS3{}
				s.F1 = s
				s.F2 = s
				return s
			}(),
			nil,
			nil,
		},
		// #4
		{
			func() any {
				a, b := 7, 7
				return struct {
					A *int
					B *int
				}{&a, &b}
			}(),
			nil,
			func(expected, actual any) bool {
				if !defaultEq(expected, actual) {
					return false
				}
				s := actual.(struct {
					A *int
					B *int
				})
				return s.A != s.B
			},
		},
		// #5
		{
			func() any {
				leaf := &graphNode{Value: 99}
				return struct {
					L *graphNode
					R *graphNode
				}{
					L: &graphNode{Next: leaf},
					R: &graphNode{Next: leaf},
				}
			}(),
			nil,
			func(_, actual any) bool {
				a := actual.(struct {
					L *graphNode
					R *graphNode
				})
				return a.L != nil && a.R != nil &&
					a.L.Next == a.R.Next && a.L.Next.Value == 99
			},
		},
		// #6
		{
			func() any {
				return struct {
					A *struct{}
					B *struct{}
				}{&struct{}{}, &struct{}{}}
			}(),
			nil,
			func(_, actual any) bool {
				a := actual.(struct {
					A *struct{}
					B *struct{}
				})
				return a.A != nil && a.B != nil
			},
		},
	}
	runTests(items, reg, t)
}

func Test_PointerChain(t *testing.T) {
	reg, _ := registry()
	items := []testItem{
		// #1
		{
			func() any {
				var x1, x2 any
				y := byte(111)
				x1 = &x2
				x2 = &y
				return x1
			}(),
			nil,
			nil,
		},
		// #2
		{
			func() any {
				var x1, x2, x3 any
				y := byte(111)
				x1 = &x2
				x2 = &x3
				x3 = &y
				return x1
			}(),
			nil,
			nil,
		},
	}
	runTests(items, reg, t)
}

func Test_CyclicPointerChain(t *testing.T) {
	reg, _ := registry()
	items := []testItem{
		// #1
		{
			func() any {
				var x any
				x = &x
				return x
			}(),
			nil,
			func(expected, actual any) bool {
				v := actual.(*any)
				return v == *v
			},
		},
		// #2
		{
			func() any {
				var x1, x2 any
				x1 = &x2
				x2 = &x1
				return x1
			}(),
			nil,
			func(expected, actual any) bool {
				x1 := actual.(*any)
				x2 := (*x1).(*any)
				return *x2 == x1 && x2 != x1
			},
		},
		// #3
		{
			func() any {
				var x1, x2, x3 any
				x1 = &x2
				x2 = &x3
				x3 = &x1
				return x1
			}(),
			nil,
			func(_, actual any) bool {
				x1 := actual.(*any)
				x2 := (*x1).(*any)
				x3 := (*x2).(*any)
				return *x3 == x1 && x1 != x2 && x2 != x3
			},
		},
		// #4
		{
			func() any {
				var x testRecPtr
				x = &x
				return x
			}(),
			nil,
			func(_, actual any) bool {
				v := actual.(testRecPtr)
				return v == *v
			},
		},
		// #5
		{
			func() any {
				var x1, x2 testRecPtr
				x1 = &x2
				x2 = &x1
				return x1
			}(),
			nil,
			func(_, actual any) bool {
				x1 := actual.(testRecPtr)
				x2 := *x1
				return *x2 == x1 && x2 != x1
			},
		},
		// #6
		{
			func() any {
				var x1, x2, x3 testRecPtr
				x1 = &x2
				x2 = &x3
				x3 = &x1
				return x1
			}(),
			nil,
			func(_, actual any) bool {
				x1 := actual.(testRecPtr)
				x2 := *x1
				x3 := *x2
				return *x3 == x1 && x1 != x2 && x2 != x3
			},
		},
		// #7
		{
			func() any {
				var x1 any
				var x2 *any
				var x3 **any
				x3 = &x2
				x2 = &x1
				x1 = x3
				return x3
			}(),
			nil,
			func(_, actual any) bool {
				x3 := actual.(**any)
				x2 := *x3
				x1 := *x2
				return x1 == x3
			},
		},
		// #8
		{
			func() any {
				var x1 any
				var x2 *any
				var x3 **any
				x3 = &x2
				x2 = &x1
				x1 = &x3
				return x3
			}(),
			nil,
			func(expected, actual any) bool {
				x3 := actual.(**any)
				x2 := *x3
				x1 := *x2
				return *x1.(***any) == x3
			},
		},
	}
	runTests(items, reg, t)
}

func Test_Interface(t *testing.T) {
	reg, typeId := registry()
	items := []testItem{
		// #1
		{
			error(nil),
			[]byte{
				version, typeId(error(nil)), meta_nil,
			},
			nil,
		},
		// #2
		{
			[1]error{errors.New("test")},
			nil,
			nil,
		},
		// #3
		{
			testSerializableInt(123),
			[]byte{
				version, typeId(testSerializableInt(0)), meta_nonil, c2b0(3), 49, 50, 51,
			},
			nil,
		},
		// #4
		{
			testInterface(123),
			nil,
			nil,
		},
		// #5
		{
			func() any {
				b := testSerializableBool(true)
				return &b
			}(),
			nil,
			nil,
		},
		// #6
		{
			func() any {
				i := testSerializablePointerInt(123)
				return &i
			}(),
			nil,
			func(_, actual any) bool {
				i, ok := actual.(*testSerializablePointerInt)
				return ok && *i == 124
			},
		},
		// #7
		{
			func() any {
				var x testInterface
				x = testSerializableInt(111)
				return x
			}(),
			nil,
			nil,
		},
		// #8
		{
			func() any {
				var p *graphNode
				return p
			}(),
			nil,
			func(_, actual any) bool {
				p, ok := actual.(*graphNode)
				return ok && p == nil
			},
		},
		// #9
		{
			func() any {
				n := 5
				return testSerializableGraph{node: &n}
			}(),
			nil,
			nil,
		},
	}
	runTests(items, reg, t)
}

func Test_Map(t *testing.T) {
	reg, typeId := registry()
	items := []testItem{
		// #1
		{
			map[int]byte{},
			[]byte{
				version, typeId(map[int]byte{}), meta_nonil, c2b0(0),
			},
			nil,
		},
		// #2
		{
			map[int]bool{1: true, 2: true, 7: false, -123: false},
			nil,
			nil,
		},
		// #3
		{
			map[any]any{byte(1): true, "a": 1.0, struct{}{}: map[string]any{"z": -123}},
			nil,
			nil,
		},
		// #4
		{
			func() any {
				b := true
				return map[int]*bool{1: &b}
			}(),
			nil,
			func(expected, actual any) bool {
				return true
			},
		},
		// #5
		{
			func() any {
				i := 123
				return map[any]*int{1: &i}
			}(),
			nil,
			nil,
		},
		// #6
		{
			func() any {
				i := 123
				b := true
				return map[any]*int{&b: &i}
			}(),
			nil,
			func(expected, actual any) bool {
				m := actual.(map[any]*int)
				for k, v := range m {
					return *k.(*bool) == true && *v == 123
				}
				return false
			},
		},
		// #7
		{
			func() any {
				var x any
				x = &x
				return map[any]any{x: x}
			}(),
			nil,
			func(expected, actual any) bool {
				m := actual.(map[any]any)
				for k, v := range m {
					return k.(*any) == *k.(*any) && k == v
				}
				return true
			},
		},
		// #8
		{
			testMap{"abc": 1234},
			nil,
			nil,
		},
		// #9
		{
			testRecMap{},
			nil,
			nil,
		},
		// #10
		{
			testRecMap{8: testRecMap(nil)},
			nil,
			nil,
		},
		// #11
		{
			[1]any{map[int]bool{1: true}},
			nil,
			nil,
		},
		{
			struct {
				Nil   map[string]int
				Empty map[string]int
			}{nil, map[string]int{}},
			nil,
			func(_, actual any) bool {
				a := actual.(struct {
					Nil   map[string]int
					Empty map[string]int
				})
				return a.Nil == nil && a.Empty != nil
			},
		},
		{
			func() any {
				m := map[string]any{}
				m["self"] = m
				return m
			}(),
			nil,
			func(_, actual any) bool {
				m := actual.(map[string]any)
				self, ok := m["self"].(map[string]any)
				if !ok {
					return false
				}
				self["shared"] = true
				return m["shared"] == true
			},
		},
	}
	runTests(items, reg, t)
}

func Test_Array(t *testing.T) {
	reg, typeId := registry()
	items := []testItem{
		// #1
		{
			[0]int{},
			[]byte{
				version, typeId([0]int{}),
			},
			nil,
		},
		// #2
		{
			[3]byte{1, 2, 3},
			[]byte{
				version, typeId([3]byte{}), 1, 2, 3,
			},
			nil,
		},
		// #3
		{
			[2][1]bool{{true}, {true}},
			[]byte{
				version, typeId([2][1]bool{}), meta_tru, meta_tru,
			},
			nil,
		},
		// #4
		{
			testArray{-1, 1, 0},
			[]byte{
				version, typeId(testArray{}), 17, 18, 16,
			},
			nil,
		},
		// #5
		{
			func() any {
				b := true
				return [3]*bool{&b, &b, &b}
			}(),
			nil,
			nil,
		},
		{
			func() any {
				a := [3]int{1, 2, 3}
				return struct {
					A [3]int
					P *[3]int
				}{a, &a}
			}(),
			nil,
			func(_, actual any) bool {
				a := actual.(struct {
					A [3]int
					P *[3]int
				})
				a.A[0] = 99
				return a.P != nil && a.P[0] == 1
			},
		},
	}
	runTests(items, reg, t)
}

func Test_Struct(t *testing.T) {
	reg, typeId := registry()
	items := []testItem{
		// #1
		{
			struct{}{},
			[]byte{version, typeId(struct{}{}), meta_strc},
			nil,
		},
		// #2
		{
			testpkg.Get(),
			nil,
			func(_, actual any) bool {
				return testpkg.Check(actual)
			},
		},
		// #3
		{
			func() any {
				m := &sync.Mutex{}
				m.Lock()
				return m
			}(),
			nil,
			func(expected, actual any) bool {
				if !defaultEq(expected, actual) {
					return false
				}
				m := actual.(*sync.Mutex)
				return !m.TryLock()
			},
		},
		// #4
		{
			func() any {
				w := &sync.WaitGroup{}
				w.Add(5)
				return w
			}(),
			nil,
			func(expected, actual any) bool {
				if !defaultEq(expected, actual) {
					return false
				}
				w := actual.(*sync.WaitGroup)
				w.Add(-5)
				w.Wait()
				return true
			},
		},
		// #5
		{
			func() any {
				m := map[any]bool{1: true}
				s := &testS3{}
				s.F1 = m
				s.F2 = [2]int{1, 2}
				s.F3 = &m
				return s
			}(),
			nil,
			func(expected, actual any) bool {
				if !defaultEq(expected, actual) {
					return false
				}
				s := actual.(*testS3)
				s.F1 = false
				return (*s.F3.(*map[any]bool))[1]
			},
		},
		// #6
		{
			func() any {
				s := &testNestedS1{
					testS1{
						F1: 100,
					},
					testS1Ptr{},
					testS2{
						F1: 123,
						F2: true,
					},
				}
				return s
			}(),
			nil,
			nil,
		},
		// #7
		{
			testS1Tags{
				f1: 123,
				f2: true,
				F3: "abc",
				f5: "abc",
			},
			nil,
			nil,
		},
		// #8
		{
			taggedRecord{
				Ignored:    "not serialized",
				Child:      &taggedChild{Value: 123, Ignored: "also omitted"},
				Deprecated: true,
				Any:        taggedChild{Value: 456, Ignored: "omitted"},
			},
			nil,
			func(_, actual any) bool {
				value := actual.(taggedRecord)
				child, ok := value.Any.(taggedChild)
				return value.Ignored == "" &&
					value.Child != nil && value.Child.Value == 123 && value.Child.Ignored == "" &&
					!value.Deprecated && ok && child.Value == 456 && child.Ignored == ""
			},
		},
	}
	runTests(items, reg, t)
}

func Test_Slice(t *testing.T) {
	reg, typeId := registry()
	items := []testItem{
		// #1
		{
			([]int)(nil),
			[]byte{
				version, typeId([]int{}), meta_nil,
			},
			nil,
		},
		// #2
		{
			[]bool{true, false, true},
			[]byte{
				version, typeId([]bool{}), meta_nonil, c2b0(3), c2b0(3),
				meta_tru, meta_fls, meta_tru,
			},
			nil,
		},
		// #3
		{
			func() any {
				s := []byte{1, 2, 3, 4, 5, 6}
				return [2][]byte{s, s[0:3:5]}
			}(),
			nil,
			func(expected, actual any) bool {
				if !defaultEq(expected, actual) {
					return false
				}
				a := actual.([2][]byte)
				p := a[0]
				s := a[1]
				p[0] = 100
				p[1] = 101
				p[2] = 102
				return s[0] == 100 && s[1] == 101 && s[2] == 102 && cap(s) == 5
			},
		},
		// #4
		{
			func() any {
				s := []byte{1, 2, 3, 4, 5, 6}
				return [3][]byte{s, s[2:4], s[0:3:4]}
			}(),
			nil,
			func(expected, actual any) bool {
				if !defaultEq(expected, actual) {
					return false
				}
				a := actual.([3][]byte)
				p := a[0]
				s1 := a[1]
				s2 := a[2]
				p[0] = 100
				p[1] = 101
				p[2] = 102
				p[3] = 103
				return s2[0] == 100 && s2[1] == 101 && s2[2] == 102 && cap(s2) == 4 &&
					s1[0] == 102 && s1[1] == 103 && cap(s1) == 4
			},
		},
		// #5
		{
			func() any {
				s := []byte{1, 2, 3, 4, 5, 6}
				return [3]any{s, s[2:4], s[0:3:4]}
			}(),
			nil,
			func(expected, actual any) bool {
				if !defaultEq(expected, actual) {
					return false
				}
				a := actual.([3]any)
				p := a[0].([]byte)
				s1 := a[1].([]byte)
				s2 := a[2].([]byte)
				p[0] = 100
				p[1] = 101
				p[2] = 102
				p[3] = 103
				return s2[0] == 100 && s2[1] == 101 && s2[2] == 102 && cap(s2) == 4 &&
					s1[0] == 102 && s1[1] == 103 && cap(s1) == 4
			},
		},
		// #6
		{
			func() any {
				s := []byte{1, 2, 3, 4, 5, 6}
				return [3]any{s[2:4], s, s[0:3:4]}
			}(),
			nil,
			func(expected, actual any) bool {
				if !defaultEq(expected, actual) {
					return false
				}
				a := actual.([3]any)
				p := a[1].([]byte)
				s1 := a[0].([]byte)
				s2 := a[2].([]byte)
				p[0] = 100
				p[1] = 101
				p[2] = 102
				p[3] = 103
				return s2[0] == 100 && s2[1] == 101 && s2[2] == 102 && cap(s2) == 4 &&
					s1[0] == 102 && s1[1] == 103 && cap(s1) == 4
			},
		},
		// #7
		{
			func() any {
				a := &[3]any{}
				a[0] = a[0:2]
				a[1] = a[1:2]
				a[2] = a[1:3]
				return a
			}(),
			nil,
			func(expected, actual any) bool {
				if !defaultEq(expected, actual) {
					return false
				}
				a := actual.(*[3]any)
				a[1].([]any)[0] = 123
				return a[0].([]any)[1] == 123 && a[2].([]any)[0] == 123
			},
		},
		// #8
		{
			func() any {
				s := []byte{1, 2, 3, 4, 5, 6}
				return [3][]byte{s[4:6], s[1:3], s[0:2]}
			}(),
			nil,
			func(expected, actual any) bool {
				if !defaultEq(expected, actual) {
					return false
				}
				a := actual.([3][]byte)
				a[1][0] = 123
				if a[2][1] != 123 {
					return false
				}
				a[1] = append(a[1], 111, 222)
				return a[0][0] == 222
			},
		},
		// #9
		{
			struct {
				S []int
			}{make([]int, 0, 17)},
			nil,
			func(expected, actual any) bool {
				if !defaultEq(expected, actual) {
					return false
				}
				s := actual.(struct {
					S []int
				}).S
				return s != nil && len(s) == 0 && cap(s) == 17
			},
		},
		// #10
		{
			testSlice{"a", "b", "c"},
			nil,
			nil,
		},
		// #11
		{
			testRecSlice{testRecSlice{nil, testRecSlice{}}, nil, testRecSlice{}},
			nil,
			nil,
		},
		// #12
		{
			func() any {
				a := [3]int{1, 2, 3}
				return struct {
					A []int
					B [3]int
				}{
					A: a[:],
					B: a,
				}
			}(),
			nil,
			func(expected, actual any) bool {
				if !defaultEq(expected, actual) {
					return false
				}
				s := actual.(struct {
					A []int
					B [3]int
				})
				s.B[0] = 100
				return s.A[0] != 100
			},
		},
		// #13
		{
			func() any {
				v := struct {
					A [3]int
					S []int
				}{}
				v.A = [3]int{1, 2, 3}
				v.S = v.A[:]
				return &v
			}(),
			nil,
			func(expected, actual any) bool {
				if !defaultEq(expected, actual) {
					return false
				}
				v := actual.(*struct {
					A [3]int
					S []int
				})
				v.A[0] = 100
				return v.S[0] == 100
			},
		},
		// #14
		{
			func() any {
				base := make([]struct{}, 10)
				return [2]any{base, base[2:5]}
			}(),
			nil,
			func(_, actual any) bool {
				slices := actual.([2]any)
				first := slices[0].([]struct{})
				second := slices[1].([]struct{})
				return len(first) == 10 && cap(first) == 10 &&
					len(second) == 3 && cap(second) == 8
			},
		},
		// #15
		{
			func() any {
				words := []uint32{0x11223344, 0x55667788}
				bytes := unsafe.Slice((*byte)(unsafe.Pointer(unsafe.SliceData(words))), len(words)*4)
				return [2]any{words, bytes}
			}(),
			nil,
			func(expected, actual any) bool {
				if !defaultEq(expected, actual) {
					return false
				}
				values := actual.([2]any)
				words := values[0].([]uint32)
				bytes := values[1].([]byte)
				firstWord := words[0]
				bytes[0] ^= 0xff
				return words[0] == firstWord
			},
		},
		// #16
		{
			func() any {
				words := [2]uint32{0x11223344, 0x55667788}
				bytes := (*[8]byte)(unsafe.Pointer(&words[0]))
				return [2]any{&words, bytes}
			}(),
			nil,
			func(expected, actual any) bool {
				if !defaultEq(expected, actual) {
					return false
				}
				values := actual.([2]any)
				words := values[0].(*[2]uint32)
				bytes := values[1].(*[8]byte)
				firstByte := bytes[0]
				words[0] ^= 0xffffffff
				return bytes[0] == firstByte
			},
		},
	}
	runTests(items, reg, t)
}

func Test_ReferenceToTheSameValue(t *testing.T) {
	reg, _ := registry()
	items := []testItem{
		// #1
		{
			func() any {
				s := testS3{}
				s.F1 = "abc"
				s.F3 = "abc"
				return s
			}(),
			nil,
			nil,
		},
		// #2
		{
			func() any {
				s := strings.Clone("abc")
				return [3]string{s, s, s}
			}(),
			nil,
			nil,
		},
		// #3
		{
			func() any {
				s := "abc"
				return [4]any{&s, &s, s, s}
			}(),
			nil,
			func(expected, actual any) bool {
				if !defaultEq(expected, actual) {
					return false
				}
				a := actual.([4]any)
				*a[1].(*string) = "cba"
				return *a[0].(*string) == "cba"
			},
		},
		// #4
		{
			func() any {
				s := "abc"
				c := s
				return [4]any{&s, &c, s, s}
			}(),
			nil,
			func(expected, actual any) bool {
				if !defaultEq(expected, actual) {
					return false
				}
				a := actual.([4]any)
				*a[1].(*string) = "cba"
				return *a[0].(*string) == "abc"
			},
		},
		// #5
		{
			func() any {
				s := testS3{}
				s.F1 = registry
				s.F2 = registry
				return s
			}(),
			nil,
			func(expected, actual any) bool {
				e := expected.(testS3)
				a := actual.(testS3)
				return funcEqual(e.F1, a.F1) && funcEqual(e.F2, a.F2) && a.F3 == nil
			},
		},
		// #6
		{
			[4]func(int, int) int{testSum, testDiv, testSum, testDiv},
			nil,
			func(expected, actual any) bool {
				e := expected.([4]func(int, int) int)
				a := actual.([4]func(int, int) int)
				return funcEqual(e[0], a[0]) && funcEqual(e[1], a[1]) && funcEqual(e[2], a[2]) &&
					funcEqual(e[3], a[3]) && funcEqual(a[0], a[2]) && funcEqual(a[1], a[3])
			},
		},
		// #7
		{
			func() any {
				f := testSum
				return [4]any{&f, &f, f, f}
			}(),
			nil,
			func(expected, actual any) bool {
				e := expected.([4]any)
				a := actual.([4]any)
				*a[0].(*func(int, int) int) = testDiv
				return funcEqual(a[0], a[1]) && funcEqual(e[2], a[2]) && funcEqual(e[3], a[3])
			},
		},
		// #8
		{
			func() any {
				f := testSum
				c := f
				return [4]any{&f, &c, f, f}
			}(),
			nil,
			func(expected, actual any) bool {
				e := expected.([4]any)
				a := actual.([4]any)
				*a[0].(*func(int, int) int) = testDiv
				return funcEqual(e[1], a[1]) && funcEqual(e[2], a[2]) && funcEqual(e[3], a[3])
			},
		},
		// #9
		{
			func() any {
				ch := make(chan<- byte, 15)
				s := testS3{}
				s.F2 = ch
				s.F3 = ch
				return s
			}(),
			nil,
			func(expected, actual any) bool {
				e := expected.(testS3)
				a := actual.(testS3)
				return chanEqual(e.F2, a.F2) && chanEqual(e.F3, a.F3) && a.F1 == nil
			},
		},
		// #10
		{
			func() any {
				ch := make(chan<- byte, 15)
				return [4]any{&ch, &ch, ch, ch}
			}(),
			nil,
			func(expected, actual any) bool {
				e := expected.([4]any)
				a := actual.([4]any)
				*a[0].(*chan<- byte) = make(chan<- byte, 5)
				return chanEqual(*a[0].(*chan<- byte), *a[1].(*chan<- byte)) &&
					chanEqual(e[2], a[2]) && chanEqual(e[3], a[3])
			},
		},
		// #11
		{
			func() any {
				ch := make(chan<- byte, 15)
				c := ch
				return [4]any{&ch, &c, ch, ch}
			}(),
			nil,
			func(expected, actual any) bool {
				e := expected.([4]any)
				a := actual.([4]any)
				*a[0].(*chan<- byte) = make(chan<- byte, 5)
				return chanEqual(*e[1].(*chan<- byte), *a[1].(*chan<- byte)) &&
					chanEqual(e[2], a[2]) && chanEqual(e[3], a[3])
			},
		},
		// #12
		{
			func() any {
				m := map[byte]byte{1: 100}
				return [3]any{m, m, m}
			}(),
			nil,
			nil,
		},
		// #13
		{
			func() any {
				m := map[byte]byte{1: 100}
				return [4]any{&m, &m, m, m}
			}(),
			nil,
			func(expected, actual any) bool {
				if !defaultEq(expected, actual) {
					return false
				}
				a := actual.([4]any)
				a[2].(map[byte]byte)[2] = 200
				if (*a[0].(*map[byte]byte))[2] != 200 || (*a[1].(*map[byte]byte))[2] != 200 || a[3].(map[byte]byte)[2] != 200 {
					return false
				}
				*a[0].(*map[byte]byte) = map[byte]byte{1: 200}
				return reflect.DeepEqual(a[0], a[1])
			},
		},
		// #14
		{
			func() any {
				m := map[byte]byte{1: 1}
				c := m
				return [4]any{&m, &c, m, m}
			}(),
			nil,
			func(expected, actual any) bool {
				if !defaultEq(expected, actual) {
					return false
				}
				a := actual.([4]any)
				a[2].(map[byte]byte)[2] = 200
				if (*a[0].(*map[byte]byte))[2] != 200 || (*a[1].(*map[byte]byte))[2] != 200 || a[3].(map[byte]byte)[2] != 200 {
					return false
				}
				*a[1].(*map[byte]byte) = map[byte]byte{2: 2}
				return reflect.DeepEqual(*a[0].(*map[byte]byte), a[3])
			},
		},
		// #15
		{
			func() any {
				s := []byte{1, 2, 3, 4, 5, 6}
				return [4]any{s[2:4], s[2:4], s, s}
			}(),
			nil,
			nil,
		},
		// #16
		{
			func() any {
				s := []byte{1, 2, 3, 4, 5, 6}
				return [4]any{&s, &s, s, s}
			}(),
			nil,
			func(expected, actual any) bool {
				if !defaultEq(expected, actual) {
					return false
				}
				a := actual.([4]any)
				a[2].([]byte)[0] = 200
				if (*a[0].(*[]byte))[0] != 200 || (*a[1].(*[]byte))[0] != 200 || a[3].([]byte)[0] != 200 {
					return false
				}
				*a[1].(*[]byte) = []byte{0}
				return reflect.DeepEqual(a[0], a[1])
			},
		},
		// #17
		{
			func() any {
				s := []byte{1, 2, 3, 4, 5, 6}
				c := s
				return [4]any{&s, &c, s, s}
			}(),
			nil,
			func(expected, actual any) bool {
				if !defaultEq(expected, actual) {
					return false
				}
				a := actual.([4]any)
				a[2].([]byte)[0] = 200
				if (*a[0].(*[]byte))[0] != 200 || (*a[1].(*[]byte))[0] != 200 || a[3].([]byte)[0] != 200 {
					return false
				}
				*a[1].(*[]byte) = []byte{0}
				return reflect.DeepEqual(*a[0].(*[]byte), a[3])
			},
		},
		// #18
		{
			func() any {
				s := []byte{1, 2, 3, 4, 5, 6}
				s1 := s[2:4]
				s2 := s[2:4]
				return [4]any{&s1, &s2, s, s}
			}(),
			nil,
			func(expected, actual any) bool {
				if !defaultEq(expected, actual) {
					return false
				}
				a := actual.([4]any)
				a[2].([]byte)[3] = 200
				if (*a[0].(*[]byte))[1] != 200 || (*a[1].(*[]byte))[1] != 200 || a[3].([]byte)[3] != 200 {
					return false
				}
				*a[1].(*[]byte) = []byte{0}
				return reflect.DeepEqual(*a[0].(*[]byte), a[3].([]byte)[2:4])
			},
		},
		// #19
		{
			func() any {
				s := strings.Clone("ab")
				return [3]string{s[:1], s, s[1:]}
			}(),
			nil,
			nil,
		},
		// #20
		{
			[2]func(int, int) int{testSum, testSum},
			nil,
			func(_, actual any) bool {
				a := actual.([2]func(int, int) int)
				return defaultEq(a[0], testSum) && defaultEq(a[1], testSum)
			},
		},
	}
	runTests(items, reg, t)
}

func Test_BackwardPointerToContainer(t *testing.T) {
	reg, _ := registry()
	items := []testItem{
		// #1
		{
			func() any {
				s := &testS3{}
				s.F1 = true
				s.F2 = &s.F1
				s.F3 = &s.F1
				return s
			}(),
			nil,
			func(expected, actual any) bool {
				if !defaultEq(expected, actual) {
					return false
				}
				s := actual.(*testS3)
				s.F1 = false
				return *s.F2.(*any) == false && *s.F3.(*any) == false
			},
		},
		// #2
		{
			func() any {
				b := true
				s := &testS3Bool{}
				s.F1 = &b
				s.F2 = &s.F1
				s.F3 = &s.F1
				return s
			}(),
			nil,
			func(expected, actual any) bool {
				if !defaultEq(expected, actual) {
					return false
				}
				s := actual.(*testS3Bool)
				s.F1 = nil
				return *s.F2.(**bool) == nil && *s.F3.(**bool) == nil
			},
		},
		// #3
		{
			func() any {
				var x any = true
				s := &testS3Any{}
				s.F1 = &x
				s.F2 = &s.F1
				s.F3 = &s.F1
				return s
			}(),
			nil,
			func(expected, actual any) bool {
				if !defaultEq(expected, actual) {
					return false
				}
				s := actual.(*testS3Any)
				*s.F1 = byte(123)
				return **s.F2.(**any) == *s.F1 && **s.F3.(**any) == *s.F1
			},
		},
		// #4
		{
			func() any {
				s := &testS3{}
				s.F1 = s
				s.F2 = &s.F1
				s.F3 = &s.F1
				return s
			}(),
			nil,
			func(expected, actual any) bool {
				if !defaultEq(expected, actual) {
					return false
				}
				s := actual.(*testS3)
				s.F1 = byte(123)
				return *s.F2.(*any) == s.F1 && *s.F3.(*any) == s.F1
			},
		},
		// #5
		{
			func() any {
				s := &testS3{}
				s.F2 = &s.F2
				return s
			}(),
			nil,
			func(expected, actual any) bool {
				if !defaultEq(expected, actual) {
					return false
				}
				s := actual.(*testS3)
				return *s.F2.(*any) == s.F2 && s.F2 != s
			},
		},
		// #6
		{
			func() any {
				a := [4]any{}
				s := []any{1, 2, 3}
				a[0] = s[0:2]
				a[1] = s[1:3]
				a[2] = &s[1]
				a[3] = s
				return a
			}(),
			nil,
			func(expected, actual any) bool {
				if !defaultEq(expected, actual) {
					return false
				}
				a := actual.([4]any)
				a[3].([]any)[1] = true
				return a[0].([]any)[1].(bool) == true && a[1].([]any)[0].(bool) == true
			},
		},
		// #7
		{
			func() any {
				a := &graphArray{A: [4]int{10, 20, 30, 40}}
				a.P = &a.A[2]
				return a
			}(),
			nil,
			func(_, actual any) bool {
				a := actual.(*graphArray)
				return a.P != nil && *a.P == 30 && a.P == &a.A[2]
			},
		},
	}
	runTests(items, reg, t)
}

func Test_ForwardPointerToContainer(t *testing.T) {
	reg, _ := registry()
	items := []testItem{
		// #1
		{
			func() any {
				s := &testS1{}
				s.F1 = s
				x := &s.F1
				return x
			}(),
			nil,
			func(expected, actual any) bool {
				if !defaultEq(expected, actual) {
					return false
				}
				x := actual.(*any)
				s := (*x).(*testS1)
				return s.F1 == s && x == &s.F1
			},
		},
		// #2
		{
			func() any {
				a := &[1]any{}
				a[0] = a
				x := &a[0]
				return x
			}(),
			nil,
			func(expected, actual any) bool {
				if !defaultEq(expected, actual) {
					return false
				}
				x := actual.(*any)
				a := (*x).(*[1]any)
				return a[0] == a && x == &a[0]
			},
		},
		// #3
		{
			func() any {
				s := &testS1Ptr{}
				s.F1 = s
				x := &s.F1
				return x
			}(),
			nil,
			func(expected, actual any) bool {
				if !defaultEq(expected, actual) {
					return false
				}
				x := actual.(**testS1Ptr)
				s := *x
				return s.F1 == s && x == &s.F1
			},
		},
		// #4
		{
			func() any {
				s := &testS4{}
				s.F1 = &s.F3
				s.F2 = &s.F3
				s.F3 = true
				s.F4 = &s.F3
				return s
			}(),
			nil,
			func(expected, actual any) bool {
				if !defaultEq(expected, actual) {
					return false
				}
				s := (actual).(*testS4)
				return s.F1 == &s.F3 && s.F2 == &s.F3 && s.F4 == &s.F3
			},
		},
		// #5
		{
			func() any {
				s := &testS4{}
				s.F1 = &s.F3
				s.F2 = &s.F3
				s.F3 = &s.F1
				s.F4 = &s.F3
				return s
			}(),
			nil,
			func(expected, actual any) bool {
				if !defaultEq(expected, actual) {
					return false
				}
				s := (actual).(*testS4)
				return s.F1 == &s.F3 && s.F2 == &s.F3 && s.F4 == &s.F3 && s.F3 == &s.F1
			},
		},
		// #6
		{
			func() any {
				a := &[4]any{}
				a[0] = &a[2]
				a[1] = &a[2]
				a[2] = &a[0]
				a[3] = &a[2]
				return a
			}(),
			nil,
			func(expected, actual any) bool {
				if !defaultEq(expected, actual) {
					return false
				}
				a := (actual).(*[4]any)
				return a[0] == &a[2] && a[1] == &a[2] && a[3] == &a[2] && a[2] == &a[0]
			},
		},
		// #7
		{
			func() any {
				s := &testS2{}
				s.F1 = &s.F2
				s.F2 = &s.F1
				return s
			}(),
			nil,
			func(expected, actual any) bool {
				if !defaultEq(expected, actual) {
					return false
				}
				s := actual.(*testS2)
				return *s.F1.(*any) == s.F2 && *s.F2.(*any) == s.F1
			},
		},
		// #8
		{
			func() any {
				a := &[2]any{}
				a[0] = &a[1]
				a[1] = &a[0]
				return a
			}(),
			nil,
			func(expected, actual any) bool {
				if !defaultEq(expected, actual) {
					return false
				}
				a := actual.(*[2]any)
				return *a[0].(*any) == a[1] && *a[1].(*any) == a[0]
			},
		},
		// #9
		{
			func() any {
				s := &testS2{}
				x := &s.F2
				y := &s.F1
				s.F1 = &x
				s.F2 = &y
				return s
			}(),
			nil,
			func(expected, actual any) bool {
				if !defaultEq(expected, actual) {
					return false
				}
				s := actual.(*testS2)
				return **s.F1.(**any) == s.F2 && **s.F2.(**any) == s.F1
			},
		},
		// #10
		{
			func() any {
				a := &[2]any{}
				x := &a[1]
				y := &a[0]
				a[0] = &x
				a[1] = &y
				return a
			}(),
			nil,
			func(expected, actual any) bool {
				if !defaultEq(expected, actual) {
					return false
				}
				a := actual.(*[2]any)
				return **a[0].(**any) == a[1] && **a[1].(**any) == a[0]
			},
		},
		// #11
		{
			func() any {
				s := &testS3{}
				x := &s.F3
				s.F1 = &x
				s.F2 = &x
				return s
			}(),
			nil,
			func(expected, actual any) bool {
				if !defaultEq(expected, actual) {
					return false
				}
				s := actual.(*testS3)
				s.F3 = 123
				return **s.F1.(**any) == s.F3 && **s.F2.(**any) == s.F3 && s.F3 == 123
			},
		},
		// #12
		{
			func() any {
				s := &testS3{}
				x := &s.F2
				s.F1 = &x
				s.F3 = &x
				return s
			}(),
			nil,
			func(expected, actual any) bool {
				if !defaultEq(expected, actual) {
					return false
				}
				s := actual.(*testS3)
				s.F2 = 123
				return **s.F1.(**any) == s.F2 && **s.F3.(**any) == s.F2 && s.F2 == 123
			},
		},
		// #13
		{
			func() any {
				a := [4]any{}
				s := []any{1, 2, 3}
				a[0] = &s[1]
				a[1] = s[0:2]
				a[2] = s[1:3]
				a[3] = s
				return a
			}(),
			nil,
			func(expected, actual any) bool {
				if !defaultEq(expected, actual) {
					return false
				}
				a := actual.([4]any)
				a[3].([]any)[1] = true
				return a[1].([]any)[1].(bool) == true && a[2].([]any)[0].(bool) == true
			},
		},
	}
	runTests(items, reg, t)
}

func Test_MixedContainerPointers(t *testing.T) {
	reg, _ := registry()
	items := []testItem{
		// #1
		{
			func() any {
				s1 := &testS1{}
				s1.F1 = s1
				s2 := &testS2{}
				s2.F2 = &s1.F1
				x := &s2.F2
				s2.F1 = &x
				return s2
			}(),
			nil,
			func(expected, actual any) bool {
				if !defaultEq(expected, actual) {
					return false
				}
				s2 := actual.(*testS2)
				x := s2.F2.(*any)
				s1 := (*x).(*testS1)
				return s1.F1 == s1 && x == &s1.F1 && **s2.F1.(**any) == s2.F2 && s2.F2 == x
			},
		},
		// #2
		{
			func() any {
				s1 := &testS1{}
				s1.F1 = s1
				s2 := &testS3{}
				s2.F2 = &s1.F1
				x := &s2.F2
				s2.F1 = &x
				s2.F3 = s2
				return s2.F3
			}(),
			nil,
			func(expected, actual any) bool {
				if !defaultEq(expected, actual) {
					return false
				}
				s2 := actual.(*testS3)
				x := s2.F2.(*any)
				s1 := (*x).(*testS1)
				return s1.F1 == s1 && x == &s1.F1 && **s2.F1.(**any) == s2.F2 && s2.F2 == x && s2.F3 == s2
			},
		},
		// #3
		{
			func() any {
				s1 := &testS3{}
				s2 := &testS3{}
				s1.F1 = s1
				s1.F2 = &s1.F3
				s1.F3 = &s2.F3
				s2.F1 = &s2.F2
				s2.F2 = &s1.F2
				s2.F3 = &s1.F1
				return s2
			}(),
			nil,
			func(expected, actual any) bool {
				if !defaultEq(expected, actual) {
					return false
				}
				s2 := actual.(*testS3)
				s1 := (*s2.F3.(*any)).(*testS3)
				return s2.F1 == &s2.F2 && s2.F2 == &s1.F2 && s2.F3 == &s1.F1 && s1.F3 == &s2.F3 && s1.F2 == &s1.F3 && s1.F1 == s1
			},
		},
		// #4
		{
			newLst(),
			nil,
			func(expected, actual any) bool {
				if !defaultEq(expected, actual) {
					return false
				}
				s := actual.(*lst)
				return s.root.next == &s.root && s.root.prev == &s.root
			},
		},
		// #5
		{
			func() any {
				l := newLst()
				l.root.lst = l
				return l.root.next
			}(),
			nil,
			func(expected, actual any) bool {
				if !defaultEq(expected, actual) {
					return false
				}
				r := actual.(*testNode)
				s := r.lst
				return s.root.next == &s.root && s.root.prev == &s.root && s.root.lst == s
			},
		},
		// #6
		{
			func() any {
				l := newLst()
				l.push()
				return l
			}(),
			nil,
			func(expected, actual any) bool {
				if !defaultEq(expected, actual) {
					return false
				}
				s := actual.(*lst)
				return s == s.root.next.lst && s.root.next == s.root.prev && s.root.next != &s.root &&
					s.root.prev.prev == &s.root && s.root.next.next == &s.root
			},
		},
		// #7
		{
			func() any {
				l := newLst()
				l.push()
				l.push()
				return l
			}(),
			nil,
			func(expected, actual any) bool {
				if !defaultEq(expected, actual) {
					return false
				}
				s := actual.(*lst)
				return s == s.root.next.lst && s == s.root.next.next.lst &&
					s.root.next.next.next == &s.root && s.root.prev.prev.prev == &s.root
			},
		},
		// #8
		{
			func() any {
				a := &[2]any{}
				s := &testS2{}
				a[0] = &s.F1
				a[1] = s
				s.F1 = &a[1]
				s.F2 = a
				return &a[0]
			}(),
			nil,
			nil,
		},
		// #9
		{
			func() any {
				n1 := &graphNode{Value: 1}
				n2 := &graphNode{Value: 2}
				n3 := &graphNode{Value: 3}
				base := []any{n1, n2, n3}
				sub := base[1:3]
				m := map[string]any{
					"root":  n1,
					"base":  base,
					"sub":   sub,
					"n2":    n2,
					"again": n2,
				}
				n1.Next = n2
				n2.Next = n3
				n3.Next = n1
				n1.Any = m
				n2.Any = sub
				return graphMixed{
					Node:  n1,
					Items: base,
					Table: m,
					Box:   n2,
				}
			}(),
			nil,
			func(expected, actual any) bool {
				if !defaultEq(expected, actual) {
					return false
				}
				g := actual.(graphMixed)
				if g.Node != g.Items[0].(*graphNode) {
					return false
				}
				if g.Box.(*graphNode) != g.Items[1].(*graphNode) {
					return false
				}
				if g.Table["again"].(*graphNode) != g.Table["n2"].(*graphNode) {
					return false
				}
				if g.Node.Next.Next.Next != g.Node {
					return false
				}
				g.Items[1].(*graphNode).Value = 900
				if g.Table["n2"].(*graphNode).Value != 900 {
					return false
				}
				sub2 := g.Table["sub"].([]any)
				if sub2[0].(*graphNode) != g.Items[1].(*graphNode) {
					return false
				}
				return true
			},
		},
		// #10
		{
			func() any {
				m := map[byte]byte{1: 2}
				a := &[2]map[byte]byte{m, m}
				return &[4]any{*a, &a[1], &m, a}
			}(),
			nil,
			func(expected, actual any) bool {
				if !defaultEq(expected, actual) {
					return false
				}
				a := actual.(*[4]any)
				*a[1].(*map[byte]byte) = map[byte]byte{100: 200}
				return a[0].([2]map[byte]byte)[1][1] == 2 && a[3].(*[2]map[byte]byte)[1][100] == 200
			},
		},
		// #11
		{
			func() any {
				i1 := 1
				i2 := 2
				m := map[testS1][1]*int{
					{F1: &i1}: {&i2},
					{F1: &i2}: {&i1},
				}
				return &testS1{
					F1: m,
				}
			}(),
			nil,
			func(_, actual any) bool {
				m := actual.(*testS1).F1.(map[testS1][1]*int)
				for k, v := range m {
					if *k.F1.(*int) != 1 && *v[0] != 2 && *k.F1.(*int) != 2 && *v[0] != 1 {
						return false
					}
				}
				return true
			},
		},
		// #12
		{
			map[byte]string{1: "abc", 2: "abc", 3: "abc"},
			nil,
			func(_, actual any) bool {
				m := actual.(map[byte]string)
				return m[1] == "abc" && m[2] == "abc" && m[3] == "abc"
			},
		},
		// #13
		{
			func() any {
				m := map[chan any]any{}
				ch := make(chan any, 5)
				ch <- 1
				ch <- true
				ch <- m
				m[ch] = ch
				return m
			}(),
			nil,
			func(_, actual any) bool {
				m := actual.(map[chan any]any)
				for ch, v := range m {
					v1 := <-ch
					v2 := <-ch
					v3 := <-ch
					return chanEqual(ch, v) && v1 == 1 && v2 == true && chanEqual(ch, v3.(map[chan any]any)[ch])
				}
				return false
			},
		},
	}
	runTests(items, reg, t)
}

func TestTypeAutoOrdering(t *testing.T) {
	v := &testS1{}
	v.F1 = [1]any{testS1{F1: &v.F1}}

	typeRegistry := NewTypeRegistry(true)
	unserializer := NewUnserializer().WithTypeRegistry(typeRegistry)
	serializer := NewSerializer().
		WithTypeRegistry(typeRegistry).
		WithTypeAutoOrdering()

	r, err := unserializer.Decode(serializer.Encode(v))
	if err != nil {
		t.Errorf("Auto ordering failed: %s", err)
		return
	}
	if !defaultEq(v, r) {
		t.Errorf("Auto ordering failed: expected and actual values are not equal.")
	}
}
