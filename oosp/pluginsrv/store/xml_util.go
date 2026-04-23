package store

import "encoding/xml"

func parseXMLString(xmlStr string, v any) error {
	return xml.Unmarshal([]byte(xmlStr), v)
}
