package serializer

var defaultTypeReg *TypeRegistry

func init() {
	defaultTypeReg = NewTypeRegistry(true)
	defaultTypeReg.RegisterBaseTypes()
}
