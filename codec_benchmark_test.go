package serializer

import "testing"

type benchmarkRecord struct {
	Name    string
	Count   int
	Enabled bool
	Payload []byte
	Values  []int
}

var (
	benchmarkEncoded []byte
	benchmarkDecoded any
)

func benchmarkGraphValue() any {
	node := &graphNode{Value: 42}
	node.Next = node
	node.Prev = node
	return graphMixed{
		Node:  node,
		Items: []any{node, []byte{1, 2, 3, 4}},
		Table: map[string]any{"node": node, "value": 99},
		Box:   node,
	}
}

func BenchmarCodec(b *testing.B) {
	payload := make([]byte, 64<<10)
	for i := range payload {
		payload[i] = byte(i)
	}
	largeRecord := benchmarkRecord{
		Name:    "large payload",
		Count:   len(payload),
		Enabled: true,
		Payload: payload,
		Values:  []int{1, 2, 3, 5, 8, 13, 21},
	}
	smallRecord := benchmarkRecord{
		Name:    "small payload",
		Count:   7,
		Enabled: true,
		Payload: []byte{1, 2, 3, 4, 5, 6, 7},
		Values:  []int{1, 2, 3, 5, 8, 13, 21},
	}
	items := []struct {
		name  string
		value any
	}{
		{"Scalar", int64(123456789)},
		{"Struct", smallRecord},
		{"ByteSlice64KiB", payload},
		{"Map", map[string][]int{
			"fibonacci": {1, 2, 3, 5, 8, 13, 21},
			"powers":    {1, 2, 4, 8, 16, 32},
		}},
		{"GraphWithAliases", benchmarkGraphValue()},
		{"LargeStruct", largeRecord},
	}

	b.Run("Encode", func(b *testing.B) {
		for _, item := range items {
			b.Run(item.name, func(b *testing.B) {
				serializer := NewSerializer()
				encoded := serializer.Encode(item.value)
				b.SetBytes(int64(len(encoded)))
				b.ReportAllocs()
				b.ResetTimer()
				for range b.N {
					benchmarkEncoded = serializer.Encode(item.value)
				}
			})
		}
	})

	b.Run("Decode", func(b *testing.B) {
		for _, item := range items {
			b.Run(item.name, func(b *testing.B) {
				data := Serialize(item.value)
				unserializer := NewUnserializer()
				b.SetBytes(int64(len(data)))
				b.ReportAllocs()
				b.ResetTimer()
				for range b.N {
					value, err := unserializer.Decode(data)
					if err != nil {
						b.Fatal(err)
					}
					benchmarkDecoded = value
				}
			})
		}
	})

	b.Run("RoundTrip", func(b *testing.B) {
		for _, item := range items {
			b.Run(item.name, func(b *testing.B) {
				serializer := NewSerializer()
				unserializer := NewUnserializer()
				b.ReportAllocs()
				b.ResetTimer()
				for range b.N {
					data := serializer.Encode(item.value)
					value, err := unserializer.Decode(data)
					if err != nil {
						b.Fatal(err)
					}
					benchmarkDecoded = value
				}
			})
		}
	})
}
