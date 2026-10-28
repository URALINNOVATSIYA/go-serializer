package serializer

type Option[T Serializer | Unserializer] func(*T)

func WithTypeRegistry[T Serializer | Unserializer](regitry *TypeRegistry) Option[T] {
	return func(obj *T) {
		switch v := any(obj).(type) {
		case *Serializer:
			v.WithTypeRegistry(regitry)
		case *Unserializer:
			v.WithTypeRegistry(regitry)
		}
	}
}

// It eliminates dependence on the order in which types are registered.
func WithTypeAutoOrdering() Option[Serializer] {
	return func(s *Serializer) {
		s.WithTypeAutoOrdering()
	}
}

func WithMaxSliceCapacity(maxCapacity int) Option[Unserializer] {
	return func(u *Unserializer) {
		u.WithMaxSliceCapacity(maxCapacity)
	}
}

func WithMaxChanCapacity(maxCapacity int) Option[Unserializer] {
	return func(u *Unserializer) {
		u.WithMaxChanCapacity(maxCapacity)
	}
}
