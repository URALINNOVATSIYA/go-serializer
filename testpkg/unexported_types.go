package testpkg

type unexportedFuncType func() int

type testStruct struct {
	p1 string
	p2 unexportedFuncType
}

var v testStruct

func init() {
	v = testStruct{
		p1: "test",
		p2: unexportedFunc,
	}
}

func unexportedFunc() int {
	return 42
}

func Get() any {
	return v
}

func Check(s any) bool {
	t, ok := s.(testStruct)
	if !ok {
		return false
	}
	return t.p1 == v.p1 && t.p2() == v.p2()
}
