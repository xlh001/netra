package main

import (
	"encoding/binary"
	"encoding/json"
	"errors"
	"fmt"
	"math"
	"strconv"
	"strings"
)

var errBSONMalformed = errors.New("malformed bson document")

const (
	bsonMaxDepth    = 32
	bsonMaxElements = 4096
)

type bsonElement struct {
	Name  string
	Value any
}

type bsonDoc []bsonElement

func (d bsonDoc) firstKey() string {
	if len(d) == 0 {
		return ""
	}
	return d[0].Name
}

func decodeBSONDocument(b []byte) (bsonDoc, int, error) {
	return decodeBSONDocumentDepth(b, 0)
}

func decodeBSONDocumentDepth(b []byte, depth int) (bsonDoc, int, error) {
	if depth > bsonMaxDepth {
		return nil, 0, errBSONMalformed
	}
	if len(b) < 5 {
		return nil, 0, errBSONMalformed
	}
	total := int(int32(binary.LittleEndian.Uint32(b[0:4])))
	if total < 5 || total > len(b) {
		return nil, 0, errBSONMalformed
	}
	if b[total-1] != 0x00 {
		return nil, 0, errBSONMalformed
	}
	body := b[4 : total-1]

	var doc bsonDoc
	pos := 0
	for pos < len(body) {
		elemType := body[pos]
		pos++
		if pos >= len(body) {
			return nil, 0, errBSONMalformed
		}
		nameEnd := indexZeroByte(body[pos:])
		if nameEnd < 0 {
			return nil, 0, errBSONMalformed
		}
		name := string(body[pos : pos+nameEnd])
		pos += nameEnd + 1

		val, n, err := decodeBSONValue(elemType, body[pos:], depth+1)
		if err != nil {
			return nil, 0, err
		}
		pos += n

		doc = append(doc, bsonElement{Name: name, Value: val})
		if len(doc) > bsonMaxElements {
			return nil, 0, errBSONMalformed
		}
	}
	return doc, total, nil
}

func decodeBSONValue(elemType byte, b []byte, depth int) (any, int, error) {
	switch elemType {
	case 0x01:
		if len(b) < 8 {
			return nil, 0, errBSONMalformed
		}
		return math.Float64frombits(binary.LittleEndian.Uint64(b[:8])), 8, nil
	case 0x02, 0x0D, 0x0E:
		if len(b) < 4 {
			return nil, 0, errBSONMalformed
		}
		l := int(int32(binary.LittleEndian.Uint32(b[:4])))
		if l < 1 || 4+l > len(b) {
			return nil, 0, errBSONMalformed
		}
		return string(b[4 : 4+l-1]), 4 + l, nil
	case 0x03:
		doc, n, err := decodeBSONDocumentDepth(b, depth)
		if err != nil {
			return nil, 0, err
		}
		return doc, n, nil
	case 0x04:
		doc, n, err := decodeBSONDocumentDepth(b, depth)
		if err != nil {
			return nil, 0, err
		}
		arr := make([]any, len(doc))
		for i, e := range doc {
			arr[i] = e.Value
		}
		return arr, n, nil
	case 0x05:
		if len(b) < 5 {
			return nil, 0, errBSONMalformed
		}
		l := int(int32(binary.LittleEndian.Uint32(b[:4])))
		if l < 0 || 5+l > len(b) {
			return nil, 0, errBSONMalformed
		}
		return fmt.Sprintf("<binary:%d bytes>", l), 5 + l, nil
	case 0x06:
		return nil, 0, nil
	case 0x07:
		if len(b) < 12 {
			return nil, 0, errBSONMalformed
		}
		return fmt.Sprintf("ObjectId(%x)", b[:12]), 12, nil
	case 0x08:
		if len(b) < 1 {
			return nil, 0, errBSONMalformed
		}
		return b[0] != 0, 1, nil
	case 0x09:
		if len(b) < 8 {
			return nil, 0, errBSONMalformed
		}
		return int64(binary.LittleEndian.Uint64(b[:8])), 8, nil
	case 0x0A:
		return nil, 0, nil
	case 0x0B:
		patEnd := indexZeroByte(b)
		if patEnd < 0 {
			return nil, 0, errBSONMalformed
		}
		optStart := patEnd + 1
		optEnd := indexZeroByte(b[optStart:])
		if optEnd < 0 {
			return nil, 0, errBSONMalformed
		}
		return fmt.Sprintf("/%s/%s", string(b[:patEnd]), string(b[optStart:optStart+optEnd])), optStart + optEnd + 1, nil
	case 0x10:
		if len(b) < 4 {
			return nil, 0, errBSONMalformed
		}
		return int32(binary.LittleEndian.Uint32(b[:4])), 4, nil
	case 0x11:
		if len(b) < 8 {
			return nil, 0, errBSONMalformed
		}
		return binary.LittleEndian.Uint64(b[:8]), 8, nil
	case 0x12:
		if len(b) < 8 {
			return nil, 0, errBSONMalformed
		}
		return int64(binary.LittleEndian.Uint64(b[:8])), 8, nil
	case 0x13:
		if len(b) < 16 {
			return nil, 0, errBSONMalformed
		}
		return "<decimal128>", 16, nil
	case 0xFF:
		return "MinKey", 0, nil
	case 0x7F:
		return "MaxKey", 0, nil
	default:
		return nil, 0, fmt.Errorf("bson: unsupported element type 0x%02x", elemType)
	}
}

func indexZeroByte(b []byte) int {
	for i, c := range b {
		if c == 0 {
			return i
		}
	}
	return -1
}

func bsonDocToJSON(doc bsonDoc) string {
	var b strings.Builder
	writeBSONDoc(&b, doc)
	return b.String()
}

func writeBSONDoc(b *strings.Builder, doc bsonDoc) {
	b.WriteByte('{')
	for i, e := range doc {
		if i > 0 {
			b.WriteByte(',')
		}
		writeJSONString(b, e.Name)
		b.WriteByte(':')
		writeBSONValue(b, e.Value)
	}
	b.WriteByte('}')
}

func writeBSONValue(b *strings.Builder, v any) {
	switch t := v.(type) {
	case nil:
		b.WriteString("null")
	case bsonDoc:
		writeBSONDoc(b, t)
	case []any:
		b.WriteByte('[')
		for i, e := range t {
			if i > 0 {
				b.WriteByte(',')
			}
			writeBSONValue(b, e)
		}
		b.WriteByte(']')
	case string:
		writeJSONString(b, t)
	case bool:
		if t {
			b.WriteString("true")
		} else {
			b.WriteString("false")
		}
	case float64:
		b.WriteString(strconv.FormatFloat(t, 'g', -1, 64))
	case int32:
		b.WriteString(strconv.FormatInt(int64(t), 10))
	case int64:
		b.WriteString(strconv.FormatInt(t, 10))
	case uint64:
		b.WriteString(strconv.FormatUint(t, 10))
	default:
		writeJSONString(b, fmt.Sprintf("%v", t))
	}
}

func writeJSONString(b *strings.Builder, s string) {
	data, err := json.Marshal(s)
	if err != nil {
		b.WriteString(`""`)
		return
	}
	b.Write(data)
}
