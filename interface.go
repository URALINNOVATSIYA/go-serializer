package serializer

import "reflect"

type Serializable interface {
	Serialize() []byte
	Unserialize([]byte) (any, error)
}

var serializableInterfaceType = reflect.TypeFor[Serializable]()

func isSerializableValue(v reflect.Value) bool {
	if v.IsValid() {
		return v.Type().Implements(serializableInterfaceType)
	}
	return false
}

func isSerializableType(t reflect.Type) bool {
	return t != nil && t.Implements(serializableInterfaceType)
}
