package dsl

import "encoding/xml"

func xmlUnmarshal(data []byte, v any) error {
	return xml.Unmarshal(data, v)
}
