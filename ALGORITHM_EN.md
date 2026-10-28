# Serialization and Deserialization Algorithm

This document describes how the `codec` package traverses a Go value graph,
writes it to the binary format, and reconstructs it when decoding. The byte
layout is specified in [FORMAT.md](./FORMAT.md).

## Serialization

The serializer uses Go reflection and a type registry. A single encode call
performs these steps:

1. Traverse the root value and build a graph of nodes.
2. Find views of shared slice and string storage.
3. Renumber the nodes in the order in which they will be written.
4. Write the header, optional type-hash table, metadata sections, and node
   stream.

### Building the value graph

Each visited value becomes a graph node. The serializer records each node's
parent and ordered children. The traversal recursively expands arrays, slices,
maps, structs, interfaces, pointers, and buffered channel values:

- Array and slice elements are visited by index.
- Each map entry contributes its key and value. Go does not guarantee map
  iteration order.
- Struct fields are visited in declaration order. In tagged mode, only fields
  with `codec:"id=N"` tags that are not marked `deprecated` are included.
- An interface visits its dynamic value.
- A non-nil pointer visits its target as a separate value; the pointer-to-target
  relationship is recorded in the graph rather than written inline.
- A channel visits its buffered elements in receive order. Reading the runtime
  buffer does not drain the channel, but the channel must not be used
  concurrently while its buffer is read.
- A value implementing `Serializable` is treated as atomic; its internals are
  not recursively traversed.

During traversal, the serializer tracks value and container identity. If a
value has already been encountered, it records a relationship to the existing
node instead of traversing it again. This represents reused values and cycles.
The repeated value's payload is marked with `meta_ref`; its target and
destinations are listed in the references section.

### Merging shared slice storage

Slices and strings may share the same backing memory. The serializer groups
related slices and strings, determines the parent and bounds of each derived
slice expression, and restructures the node tree so the shared storage is
written only once. In the node stream, a derived value is marked with
`meta_ref`; its parent and bounds are recorded in the shared-slices section.

A slice with its own backing storage is written with its elements and retains
its length and capacity. Nil and zero-capacity slices are handled separately
according to their nil state and capacity.

### Renumbering and writing

After the graph is prepared, the serializer assigns sequential IDs to nodes in
the traversal order that will be used for writing. The metadata sections use
these same IDs, so renumbering happens before metadata is encoded.

The binary output is assembled in this order:

1. The versioned header byte.
2. The type-hash table, if `WithTypeAutoOrdering` is enabled.
3. The references section, if repeated values were found.
4. The pointers section, if pointer-to-target relationships were found.
5. The shared-slices section, if shared backing storage was found.
6. The node stream.

The header flags indicate which sections are present. With type auto ordering,
the type-hash table stores hashes of type and registered function names; each
hash's zero-based position is used as its ID in this payload. This makes the
format independent of type registration order. Otherwise, IDs come directly
from the registry. Each top-level node in the stream begins with a type ID;
nested values generally get their type from the containing value and do not
need a separate ID. An interface is an exception: its dynamic value is written
with a type ID and payload.

Values are encoded according to their Go type: primitives directly, arrays and
structs sequentially, maps as key/value pairs, and nil values with a dedicated
marker. In tagged mode, a struct stores the field count, each field ID, and its
value. For `Serializable`, the encoder calls `Serialize()` and writes the
returned bytes as an opaque payload. A non-nil channel stores its capacity and,
when the capacity is nonzero, its current buffer length followed by the
buffered element values in receive order. A zero-capacity channel has no buffer
length or element payload.

## Deserialization

The decoder first checks that data is present and that the format version is
supported. It then performs these steps:

1. Read the type-hash table, if present, then the metadata sections indicated
   by the header flags.
2. Decode the node stream, creating Go values and tracking them by ID.
3. Restore shared slices and strings.
4. Restore pointers.
5. Restore references to decoded values.
6. Finish populating maps whose entries required deferred linking.
7. Send decoded buffered elements into their channels.

### Reading nodes and registering values

For each node, the decoder resolves its type ID through the registry, using the
type-hash table when present, and creates a zero value of that type. Composite
values are decoded recursively, and their children receive sequential IDs in
the same order used by the serializer. If an ID appears in a metadata section,
the decoder records the corresponding reflected value in its node table.

An interface is decoded as a separate node: its dynamic type ID determines the
value type to create. An untagged struct is filled in field order. A tagged
struct maps each field ID to a field of the current type; an unknown ID causes
an error. Fields absent from the stream retain their zero values.

For maps whose keys and values are neither pointers nor interfaces, entries can
be inserted immediately. If a key or value is a pointer or interface, the
decoder stores the IDs of the decoded keys and values and postpones insertion
until references have been restored.

For `Serializable`, the decoder passes the byte payload to
`Unserialize([]byte)`. The returned value must be compatible with the expected
type; errors returned by the method are propagated to the caller.

### Restoring the graph

Metadata sections contain IDs rather than ready-to-use values. The decoder
therefore creates the primary values first and then reconnects the nodes:

- **Shared slices and strings.** Slice expressions are created from each
  recorded parent using the stored bounds and assigned to destination nodes.
  Operations that share storage are used when converting between a string and
  a byte slice.
- **Pointers.** An addressable value is created for each target node, and all
  pointer nodes listed for that target are assigned pointers to it.
- **References.** Each alias node is assigned the value of its target node.
- **Maps.** Deferred key/value pairs are inserted after the values they refer
  to have been restored.
- **Channels.** Buffered values that required deferred linking are sent after
  references and maps have been restored. Scalar buffered values can be sent
  while the channel is decoded.

This ordering allows cycles and interconnected values to be decoded without
requiring the complete graph to exist before the node stream is read.

## Compatibility

Type and function IDs are interpreted through a `TypeRegistry`. Type auto
ordering removes the requirement for identical registration order, but decoding
still requires a registry containing the corresponding types and functions. An
unregistered function cannot be restored from the stream. A non-nil channel is
recreated with the same direction and capacity, with its buffered values
restored in receive order.
