package serializer

import (
	"reflect"
	"testing"
)

func TestParseTags_IdDuplication(t *testing.T) {
	type duplicateIds struct {
		First  int `codec:"id=7"`
		Second int `codec:"id=7"`
	}
	tags, err := ParseTags(reflect.TypeFor[duplicateIds]())
	if err == nil {
		t.Fatal("ParseTags accepted duplicate field identifiers")
	}
	if tags != nil {
		t.Fatalf("ParseTags returned tags %v along with an error", tags)
	}
}
