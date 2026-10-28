package serializer

import (
	"math"
	"math/bits"
	"reflect"
	"slices"

	"github.com/URALINNOVATSIYA/reflex"
)

const (
	version    byte = 1           // current serializer version
	meta_sref  byte = 0b0001_0000 // mark of references section
	meta_sptr  byte = 0b0010_0000 // mark of pointers section
	meta_sslc  byte = 0b0100_0000 // mark of slices section
	meta_typs  byte = 0b1000_0000 // mark of types sections
	meta_ref   byte = 0b0000_0000 // pseudo type for referenced values
	meta_fls   byte = 0b0000_0001 // boolean false
	meta_tru   byte = 0b0000_0011 // boolean true
	meta_nil   byte = 0b0001_0000 // determines whether underlying value is nil
	meta_nonil byte = 0b0010_0000 // determines whether underlying value is not nil
	meta_strc  byte = 0b0100_0000 // mark of structs
	meta_tags  byte = 0b0000_0001 // mark of structs that has codec tags
)

type addr struct {
	t   reflect.Type
	ptr uintptr
}

type Serializer struct {
	typeRegistry *TypeRegistry
	values       []reflect.Value
	slices       *reflex.SliceMap
	addrs        map[addr]int
	parents      map[int]int
	childs       map[int][]int
	ptrs         map[int][]int
	refs         map[int]int
	imap         map[int]int
	tids         map[uint64]uint16 // for type auto ordering
}

func NewSerializer(options ...Option[Serializer]) *Serializer {
	s := &Serializer{
		typeRegistry: GetDefaultTypeRegistry(),
		slices:       reflex.NewSliceMap(-2),
		addrs:        make(map[addr]int),
		parents:      make(map[int]int),
		childs:       make(map[int][]int),
		ptrs:         make(map[int][]int),
		refs:         make(map[int]int),
		imap:         make(map[int]int),
	}
	s.WithOptions(options)
	return s
}

func (s *Serializer) WithOptions(options []Option[Serializer]) *Serializer {
	for _, opt := range options {
		opt(s)
	}
	return s
}

func (s *Serializer) WithTypeRegistry(registry *TypeRegistry) *Serializer {
	s.typeRegistry = registry
	return s
}

func (s *Serializer) WithTypeAutoOrdering() *Serializer {
	s.tids = make(map[uint64]uint16)
	return s
}

func (s *Serializer) clear() {
	s.slices.Clear()
	clear(s.tids)
	clear(s.addrs)
	clear(s.parents)
	clear(s.childs)
	clear(s.ptrs)
	clear(s.refs)
	clear(s.imap)
	clear(s.values)
	s.values = s.values[:0]
}

func (s *Serializer) Encode(v any) []byte {
	defer s.clear()
	b := s.encode(reflect.ValueOf(v))
	return b
}

func (s *Serializer) encode(v reflect.Value) []byte {
	s.visit(v, -1, -1)
	s.visitSlices()
	s.renumberGraph(-1)
	b := make([]byte, 1, min(max(1, len(s.values)<<1), 4096))
	b[0] = version
	b = s.encodeRefs(b)
	b = s.encodePtrs(b)
	b = s.encodeSlices(b)
	b = s.encodeValues(b)
	b = s.encodeTypes(b)
	return b
}

func (s *Serializer) addNode(parentId int, v reflect.Value) int {
	id := len(s.values)
	s.values = append(s.values, v)
	s.parents[id] = parentId
	s.childs[parentId] = append(s.childs[parentId], id)
	return id
}

func (s *Serializer) addContainer(v reflect.Value, containerId int) bool {
	addr := addr{
		ptr: uintptr(reflex.PtrOf(v)),
		t:   v.Type(),
	}
	id, exists := s.addrs[addr]
	if id < 0 {
		s.refs[containerId] = -id
		return false
	}
	s.addrs[addr] = -containerId
	if !exists {
		return true
	}
	prevParentId := s.parents[id]
	s.childs[prevParentId] = slices.DeleteFunc(s.childs[prevParentId], func(i int) bool {
		return i == id
	})
	s.parents[id] = containerId
	s.childs[containerId] = []int{id}
	ptrs := s.ptrs[id]
	delete(s.ptrs, id)
	for _, pid := range ptrs {
		p := s.ptrs[pid]
		for i, pid := range p {
			if pid == id {
				p[i] = containerId
			}
		}
		s.ptrs[pid] = p
	}
	s.ptrs[containerId] = ptrs
	return false
}

func (s *Serializer) addReference(v reflect.Value, id int) bool {
	addr := addr{
		ptr: uintptr(reflex.DataPtrOf(v)),
		t:   v.Type(),
	}
	ref, exists := s.addrs[addr]
	if exists {
		s.refs[id] = ref
		return true
	}
	s.addrs[addr] = id
	return false
}

func (s *Serializer) addSlice(v reflect.Value, id int) (*reflex.Slice, bool) {
	slice, new := s.slices.Add(v, id)
	if new {
		return slice, true
	}
	if id == slice.Id {
		return slice, true
	}
	s.refs[id] = slice.Id
	return slice, false
}

func (s *Serializer) visit(v reflect.Value, parentId, parentContainerId int) {
	id := s.addNode(parentId, v)
	if isSerializableValue(v) {
		s.visitSerializable(v, id)
		return
	}
	switch v.Kind() {
	case reflect.Func:
		s.addReference(v, id)
	case reflect.Chan:
		s.visitChan(v, id)
	case reflect.String:
		s.addSlice(v, id)
	case reflect.Map:
		s.visitMap(v, id)
	case reflect.Interface:
		s.visitInterface(v, id, parentContainerId)
	case reflect.Pointer:
		s.visitPointer(v, id, parentContainerId)
	case reflect.Struct:
		s.visitStruct(v, id)
	case reflect.Array, reflect.Slice:
		s.visitList(v, id)
	}
}

func (s *Serializer) visitSerializable(v reflect.Value, id int) {
	if v.Kind() != reflect.Pointer || v.IsNil() {
		return
	}
	addr := addr{
		ptr: v.Pointer(),
		t:   v.Type(),
	}
	if ref, exists := s.addrs[addr]; exists {
		s.refs[id] = ref
	} else {
		s.addrs[addr] = id
	}
}

func (s *Serializer) visitChan(v reflect.Value, id int) {
	if s.addReference(v, id) {
		return
	}
	for elem := range reflex.ChanElemIter(v) {
		s.visit(elem, id, -1)
	}
}

func (s *Serializer) visitList(v reflect.Value, id int) {
	if v.Kind() == reflect.Slice && (v.IsNil() || v.Cap() == 0) {
		return
	}
	slice, new := s.addSlice(v, id)
	if !new {
		return
	}
	if slice.ElemType.Size() == 0 {
		for i := range v.Len() {
			elem := v.Index(i)
			containerId := s.addNode(id, elem)
			s.visit(elem, containerId, containerId)
		}
		return
	}
	for i := range v.Len() {
		elem := v.Index(i)
		containerId := s.addNode(id, elem)
		if proceed := s.addContainer(elem, containerId); proceed {
			s.visit(elem, containerId, containerId)
		}
	}
}

func (s *Serializer) visitMap(v reflect.Value, id int) {
	if s.addReference(v, id) {
		return
	}
	iter := v.MapRange()
	for iter.Next() {
		s.visit(iter.Key(), id, -1)
		s.visit(iter.Value(), id, -1)
	}
}

func (s *Serializer) visitInterface(v reflect.Value, id, parentContainerId int) {
	if parentContainerId < 0 {
		parentContainerId = id
	}
	s.visit(v.Elem(), id, parentContainerId)
}

func (s *Serializer) visitStruct(v reflect.Value, id int) {
	tags := s.typeRegistry.tagsByValue(v)
	if len(tags) == 0 {
		for _, field := range v.Fields() {
			containerId := s.addNode(id, field)
			if proceed := s.addContainer(field, containerId); proceed {
				s.visit(field, containerId, containerId)
			}
		}
		return
	}
	for sf, field := range v.Fields() {
		if tag, exists := tags[sf.Index[0]]; !exists || tag.Deprecated {
			continue
		}
		containerId := s.addNode(id, field)
		if proceed := s.addContainer(field, containerId); proceed {
			s.visit(field, containerId, containerId)
		}
	}
}

func (s *Serializer) visitPointer(v reflect.Value, id, parentContainerId int) {
	if v.IsNil() {
		return
	}
	elem := v.Elem()
	addr := addr{
		ptr: uintptr(reflex.PtrOf(elem)),
		t:   elem.Type(),
	}
	if parentContainerId > 0 {
		id = parentContainerId
	}
	ref, exists := s.addrs[addr]
	if exists {
		if ref < 0 {
			ref = -ref
		}
		s.ptrs[ref] = append(s.ptrs[ref], id)
		return
	}
	nextId := len(s.values)
	s.ptrs[nextId] = append(s.ptrs[nextId], id)
	s.addrs[addr] = nextId
	s.visit(elem, -1, -1)
}

func (s *Serializer) visitSlices() {
	for _, p := range s.slices.Parents() {
		if p.Id < 0 {
			s.visit(p.V, -1, -1)
		}
		if p.V.Kind() == reflect.String {
			continue
		}
		if p.ElemSize() == 0 {
			continue
		}
		pchilds := s.childs[p.Id]
		elemSize := p.ElemType.Size()
		for _, child := range p.Childs {
			i := int(child.Ptr-p.Ptr) / int(elemSize)
			for j, containerId := range s.childs[child.Id] {
				delete(s.parents, containerId)
				if _, exists := s.refs[containerId]; exists {
					delete(s.refs, containerId)
					continue
				}
				pid := pchilds[i+j]
				delete(s.refs, pid)
				delete(s.parents, pid)
				pchilds[i+j] = containerId
				s.parents[containerId] = p.Id
			}
			delete(s.childs, child.Id)
		}
	}
}

func (s *Serializer) renumberGraph(parentId int) {
	for _, id := range s.childs[parentId] {
		s.imap[id] = len(s.imap)
		s.renumberGraph(id)
	}
}

func (s *Serializer) encodeValues(b []byte) []byte {
	for _, id := range s.childs[-1] {
		b = s.encodeNode(b, id)
	}
	return b
}

func (s *Serializer) encodeType(tid typeId, b []byte) []byte {
	if s.tids == nil {
		return t2b(b, tid.id)
	}
	id, exists := s.tids[tid.hash]
	if !exists {
		id = uint16(len(s.tids))
		s.tids[tid.hash] = id
	}
	return t2b(b, id)
}

func (s *Serializer) encodeNode(b []byte, id int) []byte {
	tid := s.typeRegistry.typeIdByValue(s.values[id])
	return s.encodeValue(s.encodeType(tid, b), id)
}

func (s *Serializer) encodeValue(b []byte, id int) []byte {
	v := s.values[id]
	if isSerializableValue(v) {
		return s.encodeSerializable(b, v, id)
	}
	switch v.Kind() {
	case reflect.Invalid:
		return s.encodeNil(b)
	case reflect.Bool:
		return s.encodeBool(b, v)
	case reflect.Uint8:
		return s.encodeUint8(b, v)
	case reflect.Int8:
		return s.encodeInt8(b, v)
	case reflect.Uint16:
		return s.encodeUint16(b, v)
	case reflect.Int16:
		return s.encodeInt16(b, v)
	case reflect.Uint32:
		return s.encodeUint32(b, v)
	case reflect.Int32:
		return s.encodeInt32(b, v)
	case reflect.Uint64:
		return s.encodeUint64(b, v)
	case reflect.Int64:
		return s.encodeInt64(b, v)
	case reflect.Uint:
		return s.encodeUint(b, v)
	case reflect.Int:
		return s.encodeInt(b, v)
	case reflect.Float32:
		return s.encodeFloat32(b, v)
	case reflect.Float64:
		return s.encodeFloat64(b, v)
	case reflect.Complex64:
		return s.encodeComplex64(b, v)
	case reflect.Complex128:
		return s.encodeComplex128(b, v)
	case reflect.Uintptr:
		return s.encodeUintptr(b, v)
	case reflect.UnsafePointer:
		return s.encodeUnsafePointer(b, v)
	case reflect.Chan:
		return s.encodeChan(b, v, id)
	case reflect.Func:
		return s.encodeFunc(b, v, id)
	case reflect.String:
		return s.encodeString(b, v, id)
	case reflect.Array:
		return s.encodeArray(b, id)
	case reflect.Slice:
		return s.encodeSlice(b, v, id)
	case reflect.Map:
		return s.encodeMap(b, v, id)
	case reflect.Struct:
		return s.encodeStruct(b, v, id)
	case reflect.Interface:
		return s.encodeInterface(b, id)
	case reflect.Pointer:
		return s.encodePointer(b, v)
	}
	panic("unrecognized value kind")
}

func (s *Serializer) encodeSerializable(b []byte, v reflect.Value, id int) []byte {
	switch v.Kind() {
	case reflect.Interface,
		reflect.Map, reflect.Slice,
		reflect.Pointer, reflect.UnsafePointer,
		reflect.Chan, reflect.Func:
		if v.IsNil() {
			return s.encodeNil(b)
		}
		if _, exists := s.refs[id]; exists {
			return s.encodeReference(b)
		}
	}
	body := v.MethodByName("Serialize").Call(nil)[0].Interface().([]byte)
	b = append(b, meta_nonil)
	b = c2b(b, len(body))
	return append(b, body...)
}

func (s *Serializer) encodeNil(b []byte) []byte {
	return append(b, meta_nil)
}

func (s *Serializer) encodeBool(b []byte, v reflect.Value) []byte {
	if v.Bool() {
		return append(b, meta_tru)
	}
	return append(b, meta_fls)
}

func (s *Serializer) encodeUint8(b []byte, v reflect.Value) []byte {
	return append(b, uint8(v.Uint()))
}

func (s *Serializer) encodeInt8(b []byte, v reflect.Value) []byte {
	return append(b, uint8(i2u(v.Int())))
}

func (s *Serializer) encodeUint16(b []byte, v reflect.Value) []byte {
	return u2bs(b, v.Uint(), 2)
}

func (s *Serializer) encodeInt16(b []byte, v reflect.Value) []byte {
	return u2bs(b, i2u(v.Int()), 2)
}

func (s *Serializer) encodeUint32(b []byte, v reflect.Value) []byte {
	return u2bs(b, v.Uint(), 3)
}

func (s *Serializer) encodeInt32(b []byte, v reflect.Value) []byte {
	return u2bs(b, i2u(v.Int()), 3)
}

func (s *Serializer) encodeUint64(b []byte, v reflect.Value) []byte {
	return u2bs(b, v.Uint(), 4)
}

func (s *Serializer) encodeInt64(b []byte, v reflect.Value) []byte {
	return u2bs(b, i2u(v.Int()), 4)
}

func (s *Serializer) encodeUint(b []byte, v reflect.Value) []byte {
	return s.encodeUint64(b, v)
}

func (s *Serializer) encodeInt(b []byte, v reflect.Value) []byte {
	return s.encodeInt64(b, v)
}

func (s *Serializer) encodeFloat32(b []byte, v reflect.Value) []byte {
	return u2bs(b, uint64(bits.ReverseBytes32(math.Float32bits(float32(v.Float())))), 3)
}

func (s *Serializer) encodeFloat64(b []byte, v reflect.Value) []byte {
	return u2bs(b, bits.ReverseBytes64(math.Float64bits(v.Float())), 4)
}

func (s *Serializer) encodeComplex64(b []byte, v reflect.Value) []byte {
	c := v.Complex()
	b = s.encodeFloat32(b, reflect.ValueOf(float32(real(c))))
	return s.encodeFloat32(b, reflect.ValueOf(float32(imag(c))))
}

func (s *Serializer) encodeComplex128(b []byte, v reflect.Value) []byte {
	c := v.Complex()
	b = s.encodeFloat64(b, reflect.ValueOf(real(c)))
	return s.encodeFloat64(b, reflect.ValueOf(imag(c)))
}

func (s *Serializer) encodeUintptr(b []byte, v reflect.Value) []byte {
	return s.encodeUint64(b, v)
}

func (s *Serializer) encodeUnsafePointer(b []byte, v reflect.Value) []byte {
	return u2bs(b, uint64(v.Pointer()), 4)
}

func (s *Serializer) encodeChan(b []byte, v reflect.Value, id int) []byte {
	if v.IsNil() {
		return s.encodeNil(b)
	}
	if _, exists := s.refs[id]; exists {
		return s.encodeReference(b)
	}
	b = append(b, meta_nonil)
	capacity, length := v.Cap(), v.Len()
	b = c2b(b, capacity)
	if capacity == 0 {
		return b
	}
	b = c2b(b, length)
	for _, childId := range s.childs[id] {
		b = s.encodeValue(b, childId)
	}
	return b
}

func (s *Serializer) encodeFunc(b []byte, v reflect.Value, id int) []byte {
	if v.IsNil() {
		return s.encodeNil(b)
	}
	if _, exists := s.refs[id]; exists {
		return s.encodeReference(b)
	}
	b = append(b, meta_nonil)
	fid := s.typeRegistry.funcIdByValue(v)
	return s.encodeType(fid, b)
}

func (s *Serializer) encodeString(b []byte, v reflect.Value, id int) []byte {
	if ref, exists := s.refs[id]; exists {
		if s.slices.Get(ref).Parent == nil {
			return s.encodeReference(b)
		}
		return append(b, meta_ref)
	}
	if s.slices.Get(id).Parent == nil {
		return append(c2b(b, v.Len()), v.String()...)
	}
	return append(b, meta_ref)
}

func (s *Serializer) encodeSlice(b []byte, v reflect.Value, id int) []byte {
	if v.IsNil() {
		return append(b, meta_nil)
	}
	if v.Cap() == 0 {
		b = append(b, meta_nonil)
		b = c2b(b, v.Len())
		return c2b(b, v.Cap())
	}
	if ref, exists := s.refs[id]; exists {
		if s.slices.Get(ref).Parent == nil {
			return s.encodeReference(b)
		}
		return append(b, meta_ref)
	}
	slice := s.slices.Get(id)
	if slice.Parent != nil {
		return append(b, meta_ref)
	}
	b = append(b, meta_nonil)
	b = c2b(b, v.Len())
	b = c2b(b, v.Cap())
	for _, containerId := range s.childs[id] {
		b = s.encodeValue(b, s.childs[containerId][0])
	}
	return b
}

func (s *Serializer) encodeMap(b []byte, v reflect.Value, id int) []byte {
	if v.IsNil() {
		return s.encodeNil(b)
	}
	if _, exists := s.refs[id]; exists {
		return s.encodeReference(b)
	}
	b = append(b, meta_nonil)
	b = c2b(b, v.Len())
	for _, childId := range s.childs[id] {
		b = s.encodeValue(b, childId)
	}
	return b
}

func (s *Serializer) encodeArray(b []byte, id int) []byte {
	for _, containerId := range s.childs[id] {
		b = s.encodeValue(b, s.childs[containerId][0])
	}
	return b
}

func (s *Serializer) encodeStruct(b []byte, v reflect.Value, id int) []byte {
	b = append(b, meta_strc)
	tags := s.typeRegistry.tagsByValue(v)
	if len(tags) == 0 {
		for _, containerId := range s.childs[id] {
			b = s.encodeValue(b, s.childs[containerId][0])
		}
		return b
	}
	b[len(b)-1] |= meta_tags
	b = c2b(b, len(s.childs[id]))
	childIndex := 0
	for fieldIndex := range v.NumField() {
		tag, exists := tags[fieldIndex]
		if !exists || tag.Deprecated {
			continue
		}
		containerId := s.childs[id][childIndex]
		b = c2b(b, tag.Id)
		b = s.encodeValue(b, s.childs[containerId][0])
		childIndex++
	}
	return b
}

func (s *Serializer) encodeInterface(b []byte, id int) []byte {
	return s.encodeNode(b, s.childs[id][0])
}

func (s *Serializer) encodePointer(b []byte, v reflect.Value) []byte {
	if v.IsNil() {
		return s.encodeNil(b)
	}
	return append(b, meta_nonil)
}

func (s *Serializer) encodeReference(b []byte) []byte {
	return append(b, meta_ref)
}

func (s *Serializer) encodeRefs(b []byte) []byte {
	if len(s.refs) == 0 {
		return b
	}
	refs := make(map[int][]int)
	for id, ref := range s.refs {
		refs[ref] = append(refs[ref], id)
	}
	b[0] |= meta_sref
	b = c2b(b, len(refs))
	for referencedValueId, refIds := range refs {
		b = c2b(b, s.imap[referencedValueId])
		b = c2b(b, len(refIds))
		for _, refId := range refIds {
			b = c2b(b, s.imap[s.settableParentId(refId)])
		}
	}
	return b
}

func (s *Serializer) encodePtrs(b []byte) []byte {
	if len(s.ptrs) == 0 {
		return b
	}
	b[0] |= meta_sptr
	b = c2b(b, len(s.ptrs))
	for pointedValueId, ptrIds := range s.ptrs {
		b = c2b(b, s.imap[pointedValueId])
		b = c2b(b, len(ptrIds))
		for _, ptrId := range ptrIds {
			b = c2b(b, s.imap[ptrId])
		}
	}
	return b
}

func (s *Serializer) encodeSlices(b []byte) []byte {
	parentWithChilds := 0
	for _, parent := range s.slices.Parents() {
		if len(parent.Childs) > 0 {
			parentWithChilds++
		}
	}
	if parentWithChilds == 0 {
		return b
	}
	b[0] |= meta_sslc
	b = c2b(b, parentWithChilds)
	for _, parent := range s.slices.Parents() {
		if len(parent.Childs) == 0 {
			continue
		}
		b = c2b(b, s.imap[parent.Id])
		b = c2b(b, len(parent.Childs))
		for _, child := range parent.Childs {
			b = c2b(b, s.imap[s.settableParentId(child.Id)])
			i, j, k := parent.SliceOf(child)
			b = c2b(b, i)
			b = c2b(b, j)
			b = c2b(b, k)
		}
	}
	return b
}

func (s *Serializer) settableParentId(childId int) int {
	pid := s.parents[childId]
	if pid < 0 {
		return childId
	}
	if s.values[pid].Kind() == reflect.Interface {
		return pid
	}
	return childId
}

func (s *Serializer) encodeTypes(b []byte) []byte {
	size := len(s.tids)
	if size == 0 {
		return b
	}
	hashes := make([]uint64, size)
	for hash, i := range s.tids {
		hashes[i] = hash
	}
	b[0] |= meta_typs
	c := c2b([]byte{b[0]}, size)
	for _, hash := range hashes {
		c = u2bs(c, hash, 4)
	}
	return append(c, b[1:]...)
}

func Serialize(value any, options ...Option[Serializer]) []byte {
	return NewSerializer().
		WithOptions(options).
		Encode(value)
}
