package serializer

import (
	"encoding/binary"
	"fmt"
	"hash/fnv"
	"reflect"
	"sync"
	"unsafe"

	"github.com/URALINNOVATSIYA/reflex"
)

type typeKey struct {
	t   reflect.Type
	ptr unsafe.Pointer // only for functions
}

type typeId struct {
	hash uint64
	id   uint16
}

type TypeRegistry struct {
	typeAutoReg bool
	hashes      map[uint64]uint16        // keys are hashes of type names or function names, values are identifiers of types
	types       map[uint16]reflect.Type  // registered types
	funcs       map[uint16]reflect.Value // registered functions
	tags        map[uint16]TagMap        // codec struct tags
	ids         map[typeKey]typeId       // type and func full names and their ids
	mx          sync.RWMutex
}

func NewTypeRegistry(typeAutoReg bool) *TypeRegistry {
	return &TypeRegistry{
		typeAutoReg: typeAutoReg,
		hashes:      make(map[uint64]uint16),
		types:       make(map[uint16]reflect.Type),
		funcs:       make(map[uint16]reflect.Value),
		tags:        make(map[uint16]TagMap),
		ids:         make(map[typeKey]typeId),
	}
}

func (r *TypeRegistry) TurnOnTypeAutoRegistration() {
	r.typeAutoReg = true
}

func (r *TypeRegistry) TurnOffTypeAutoRegistration() {
	r.typeAutoReg = false
}

func (r *TypeRegistry) RegisteredTypeNames() []string {
	names := make([]string, len(r.ids)+1)
	for key, i := range r.ids {
		names[i.id] = reflex.NameOf(key.t)
	}
	return names
}

func (r *TypeRegistry) RegisterBaseTypes() {
	r.RegisterTypeOf(nil)
	r.RegisterTypeOf(false)
	r.RegisterTypeOf("")
	r.RegisterTypeOf(int8(0))
	r.RegisterTypeOf(uint8(0))
	r.RegisterTypeOf(int16(0))
	r.RegisterTypeOf(uint16(0))
	r.RegisterTypeOf(int32(0))
	r.RegisterTypeOf(uint32(0))
	r.RegisterTypeOf(int64(0))
	r.RegisterTypeOf(uint64(0))
	r.RegisterTypeOf(int(0))
	r.RegisterTypeOf(uint(0))
	r.RegisterTypeOf(float32(0))
	r.RegisterTypeOf(float64(0))
	r.RegisterTypeOf(complex64(0))
	r.RegisterTypeOf(complex128(0))
	r.RegisterTypeOf(uintptr(0))
	r.RegisterTypeOf(unsafe.Pointer(nil))
	r.RegisterType(reflect.TypeFor[any]()) // interface {}
}

func (r *TypeRegistry) RegisterTypeOf(v any) {
	t := reflect.TypeOf(v)
	r.RegisterTypeRec(t)
	if t != nil && t.Kind() == reflect.Func {
		r.RegisterFunc(v)
	}
}

func (r *TypeRegistry) RegisterTypeRec(t reflect.Type) {
	r.RegisterType(t)
	if t == nil {
		return
	}
	switch t.Kind() {
	case reflect.Map:
		r.RegisterTypeRec(t.Key())
		fallthrough
	case reflect.Array, reflect.Slice, reflect.Chan, reflect.Pointer:
		r.RegisterTypeRec(t.Elem())
	case reflect.Struct:
		for field := range t.Fields() {
			r.RegisterTypeRec(field.Type)
		}
	case reflect.Func:
		for in := range t.Ins() {
			r.RegisterTypeRec(in)
		}
		for out := range t.Outs() {
			r.RegisterTypeRec(out)
		}
	}
}

func (r *TypeRegistry) RegisterType(t reflect.Type) {
	key := typeKey{t: t}
	r.mx.RLock()
	_, exists := r.ids[key]
	r.mx.RUnlock()
	if exists {
		return
	}
	r.bindType(key)
}

func (r *TypeRegistry) RegisterFunc(f any) {
	v := reflect.ValueOf(f)
	if v.Kind() != reflect.Func {
		panic(fmt.Errorf("argument of RegisterFunc is not function"))
	}
	key := typeKey{
		t:   v.Type(),
		ptr: reflex.DataPtrOf(v),
	}
	r.mx.RLock()
	_, exists := r.ids[key]
	r.mx.RUnlock()
	if exists {
		return
	}
	r.bindFunc(key, v)
}

func (r *TypeRegistry) typeByHash(hash uint64) reflect.Type {
	r.mx.RLock()
	defer r.mx.RUnlock()
	if id, exists := r.hashes[hash]; exists {
		return r.types[id]
	}
	panic(fmt.Errorf("unrecognized type hash: %d", hash))
}

func (r *TypeRegistry) funcByHash(hash uint64) reflect.Value {
	r.mx.RLock()
	defer r.mx.RUnlock()
	if id, exists := r.hashes[hash]; exists {
		return r.funcs[id]
	}
	panic(fmt.Errorf("unrecognized type hash: %d", hash))
}

func (r *TypeRegistry) typeById(id uint16) reflect.Type {
	r.mx.RLock()
	t, exists := r.types[id]
	r.mx.RUnlock()
	if !exists {
		panic(fmt.Errorf("unrecognized type [id: %d]", id))
	}
	return t
}

func (r *TypeRegistry) funcById(id uint16) reflect.Value {
	r.mx.RLock()
	f, exists := r.funcs[id]
	r.mx.RUnlock()
	if !exists {
		panic(fmt.Errorf("unrecognized func [id: %d]", id))
	}
	return f
}

func (r *TypeRegistry) tagsByValue(v reflect.Value) TagMap {
	tid := r.typeIdByValue(v)
	r.mx.RLock()
	tags := r.tags[tid.id]
	r.mx.RUnlock()
	return tags
}

func (r *TypeRegistry) typeIdByType(t reflect.Type) typeId {
	key := typeKey{t: t}
	r.mx.RLock()
	id, exists := r.ids[key]
	r.mx.RUnlock()
	if exists {
		return id
	}
	if !r.typeAutoReg {
		panic(fmt.Errorf("unregistered type: %s", reflex.NameOf(t)))
	}
	return r.bindType(key)
}

func (r *TypeRegistry) typeIdByValue(v reflect.Value) typeId {
	var t reflect.Type
	if v.IsValid() {
		t = v.Type()
	}
	return r.typeIdByType(t)
}

func (r *TypeRegistry) funcIdByValue(v reflect.Value) typeId {
	if v.Kind() != reflect.Func {
		panic("argument must be a function")
	}
	key := typeKey{
		t:   v.Type(),
		ptr: reflex.DataPtrOf(v),
	}
	r.mx.RLock()
	id, exists := r.ids[key]
	r.mx.RUnlock()
	if exists {
		return id
	}
	if !r.typeAutoReg {
		panic(fmt.Errorf("unregistered func: %s", reflex.NameOf(key.t)))
	}
	r.RegisterType(key.t)
	return r.bindFunc(key, v)
}

func (r *TypeRegistry) bindType(key typeKey) typeId {
	hash := r.hash(key)
	r.mx.Lock()
	tid := r.assignTypeId(key, hash)
	r.types[tid.id] = key.t
	r.hashes[hash] = tid.id
	r.mx.Unlock()
	tags, err := ParseTags(key.t)
	if err != nil {
		panic(err)
	}
	if len(tags) > 0 {
		r.mx.Lock()
		r.tags[tid.id] = tags
		r.mx.Unlock()
	}
	return tid
}

func (r *TypeRegistry) bindFunc(key typeKey, v reflect.Value) typeId {
	hash := r.hash(key)
	r.mx.Lock()
	tid := r.assignTypeId(key, hash)
	r.funcs[tid.id] = v
	r.hashes[hash] = tid.id
	r.mx.Unlock()
	return tid
}

func (r *TypeRegistry) assignTypeId(key typeKey, hash uint64) typeId {
	id, exists := r.ids[key]
	if !exists {
		id = typeId{
			hash: hash,
			id:   uint16(len(r.ids) + 1),
		}
		r.ids[key] = id
	}
	return id
}

func (r *TypeRegistry) hash(key typeKey) uint64 {
	h := fnv.New64a()
	if key.ptr == nil {
		h.Write([]byte(reflex.NameOf(key.t)))
	} else {
		p := uint64(uintptr(key.ptr))
		buf := make([]byte, 8)
		binary.LittleEndian.PutUint64(buf, p)
		h.Write(buf)
	}
	return h.Sum64()
}

func GetDefaultTypeRegistry() *TypeRegistry {
	return defaultTypeReg
}

func GetRegisteredTypeNames() []string {
	return GetDefaultTypeRegistry().RegisteredTypeNames()
}

func RegisterTypeOf(value any) {
	GetDefaultTypeRegistry().RegisterTypeOf(value)
}

func RegisterType(t reflect.Type) {
	GetDefaultTypeRegistry().RegisterType(t)
}

func RegisterFunc(v any) {
	GetDefaultTypeRegistry().RegisterFunc(v)
}

func TurnOffTypeAutoRegistration() {
	GetDefaultTypeRegistry().TurnOffTypeAutoRegistration()
}

func TurnOnTypeAutoRegistration() {
	GetDefaultTypeRegistry().TurnOnTypeAutoRegistration()
}
