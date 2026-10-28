package serializer

import (
	"errors"
	"fmt"
	"reflect"
	"strconv"
	"strings"
)

type TagMap map[int]Tag // Keys are field indexes

func (m TagMap) IndexById(id int) int {
	for i, t := range m {
		if t.Id == id {
			return i
		}
	}
	return -1
}

type Tag struct {
	Id         int  // Field identifier
	Deprecated bool // Field is no longer used
}

func ParseTags(t reflect.Type) (TagMap, error) {
	if t == nil || t.Kind() != reflect.Struct {
		return nil, nil
	}
	m := make(TagMap)
	ids := make(map[int]string)
	for field := range t.Fields() {
		v, exists := field.Tag.Lookup("codec")
		if !exists {
			continue
		}
		id, deprecated, err := parseTagValue(v)
		if err != nil {
			return nil, err
		}
		if id < 0 {
			continue
		}
		if previous, exists := ids[id]; exists {
			return nil, fmt.Errorf("invalid codec tag format: identifier %d is used by both %q and %q", id, previous, field.Name)
		}
		ids[id] = field.Name
		m[field.Index[0]] = Tag{
			Id:         id,
			Deprecated: deprecated,
		}
	}
	return m, nil
}

func parseTagValue(tag string) (id int, deprecated bool, err error) {
	for v := range strings.SplitSeq(tag, ",") {
		v = strings.TrimSpace(v)
		if v == "deprecated" {
			deprecated = true
			continue
		}
		parts := strings.SplitN(v, "=", 2)
		if len(parts) < 2 {
			err = errors.New("invalid codec tag format: identifier is not set")
			return
		}
		if strings.TrimSpace(parts[0]) != "id" {
			continue
		}
		id, err = strconv.Atoi(strings.TrimSpace(parts[1]))
		if err != nil {
			err = fmt.Errorf("invalid codec tag format: %s", err)
			return
		}
		if id < 0 {
			err = errors.New("invalid codec tag format: field identifier must not be negative")
			return
		}
	}
	return
}
