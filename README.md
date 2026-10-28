# Serializer

`go-serializer` is a Go serializer that converts values to a binary representation and
restores them at runtime preserving their internal states. It supports primitive values and composite Go values,
including pointers, interfaces, arrays, slices, maps, structs, channels, functions and shared references between values.

## Contents

- [Installation](#installation)
- [Quick start](#quick-start)
- [Options](#options)
- [Type registration](#type-registration)
- [Custom serialization](#custom-serialization)
- [Capabilities](#capabilities)
- [Limitations](#limitations)

## Installation

```bash
go get github.com/URALINNOVATSIYA/go-serializer
```

## Quick start

For a single operation, use the package-level functions:

```go
package main

import (
	"fmt"

	"github.com/URALINNOVATSIYA/go-serializer"
)

func main() {
	original := map[string]any{
		"name":  "Ada",
		"active": true,
		"scores": []int{10, 20, 30},
	}

	data := serializer.Serialize(original)
	value, err := serializer.Unserialize(data)
	if err != nil {
		panic(err)
	}

	fmt.Printf("%#v\n", value)
}
```

When a serializer is reused, or when a custom type registry is needed, use the
stateful types directly:

```go
s := serializer.NewSerializer()
data := s.Encode(value)

u := serializer.NewUnserializer()
decoded, err := u.Decode(data)
if err != nil {
	panic(err)
}
```

`Serialize` and `Unserialize` also accept options. A `*TypeRegistry` passed to
both operations makes the type mapping explicit and consistent:

```go
type MyMessage struct {
	Text string
}

registry := serializer.NewTypeRegistry(false)
registry.RegisterBaseTypes()
registry.RegisterTypeOf(MyMessage{})

data := serializer.Serialize(MyMessage{Text: "hello"}, serializer.WithTypeRegistry(registry))
value, err := serializer.Unserialize(data, serializer.WithTypeRegistry(registry))
```

## Options

Options can be passed to the package-level `Serialize` and `Unserialize`
functions or to `NewSerializer` and `NewUnserializer`. For example,
`WithTypeAutoOrdering` makes the serialized data carry a table of hashes of
type names and registered function names, and uses each hash's position as the
identifier:

```go
data := serializer.Serialize(value, serializer.WithTypeAutoOrdering())
decoded, err := serializer.Unserialize(data, serializer.WithTypeRegistry(registry))
if err != nil {
	panic(err)
}
```

The decoder detects this table from the data header, so the serializer and
decoder do not need types registered in the same order. The decoder still needs
the corresponding Go types and registered function values.

Deserializer options can also limit allocations when decoding slices and
channels. A positive limit is enforced; zero or a negative limit disables that
limit:

```go
decoded, err := serializer.Unserialize(
	data,
	serializer.WithMaxSliceCapacity(1_000_000),
	serializer.WithMaxChanCapacity(10_000),
)
```

## Type registration

The serializer writes type identifiers to the binary data. By default, the
unserializer resolves them through the registry, so the serializer and
unserializer need compatible type mappings. With `WithTypeAutoOrdering`, the
data also carries a table of name hashes, allowing the registry to resolve
identifiers regardless of registration order. By default, the package registry
registers encountered types automatically. This is convenient, but the
resulting registration order can vary between processes or between different
serialization paths.

Without automatic type ordering, the serializer and unserializer must use
compatible type registries. For a stable registry, register all application
types before serialization and unserialization:

```go
package main

import (
	"reflect"

	"github.com/URALINNOVATSIYA/go-serializer"
)

type User struct {
	Name  string
	Active bool
}

func registerTypes() {
	// Register a type by providing a zero value.
	serializer.RegisterTypeOf(User{})
	serializer.RegisterTypeOf(map[string][]bool{})

	// Or register a reflect.Type directly.
	serializer.RegisterType(reflect.TypeOf([]User{}))
}
```

For strict control, create a registry with automatic registration disabled:

```go
registry := serializer.NewTypeRegistry(false)
registry.RegisterBaseTypes()
registry.RegisterTypeOf(User{})

serializer := serializer.NewSerializer().WithTypeRegistry(registry)
unserializer := serializer.NewUnserializer().WithTypeRegistry(registry)
```

`RegisterTypeOf` is the preferred way to register a function: it registers
both the function type and the concrete function value. Registering only a
function type cannot uniquely identify a function value because the type does
not contain the function name. Use `RegisterFunc` when registering a function
value directly.

The package-level `TurnOffTypeAutoRegistration` and
`TurnOnTypeAutoRegistration` functions control automatic registration in the
default registry. A custom `TypeRegistry` exposes the same controls as methods.

## Custom serialization

A type can control its own representation by implementing `Serializable`.
Both methods must be defined on the value type, not only on a pointer type:

```go
import "fmt"

type Flag bool

func (flag Flag) Serialize() []byte {
	if flag {
		return []byte{1}
	}
	return []byte{0}
}

func (flag Flag) Unserialize(data []byte) (any, error) {
	if len(data) != 1 {
		return nil, fmt.Errorf("invalid Flag data length: %d", len(data))
	}
	return Flag(data[0] == 1), nil
}
```

The type must be registered in the same registry used for decoding. The
`Unserialize` method is responsible for validating its input and returning a
value of the custom type.

## Capabilities

- Binary serialization and deserialization of Go values at runtime.
- Primitive values, strings, arrays, slices, maps, structs and interfaces.
- Pointers, repeated references and cyclic object graphs.
- Channels, including their buffered values.
- Function values when the function is registered correctly.
- Automatic or explicit type registration through `TypeRegistry`.
- Custom representations through `Serializable`.

## Limitations

Version 1 has the following limitations:

- `unsafe.Pointer` cannot be restored correctly after deserialization.
- `uintptr` cannot be restored correctly after deserialization.
- A string and a slice, or two slices, that share backing storage but have
  different element types (for example, when created with `unsafe.Pointer`)
  are not supported.

There are no other known type or data-structure limitations beyond the normal
requirement that the decoder must have access to the types and registered
function values referenced by the serialized data.
