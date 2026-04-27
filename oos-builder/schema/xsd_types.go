package schema

// xsd_types.go — minimal XSD AST tailored to what the OOS DSL grammar
// actually uses. Only the constructs we encounter in dsl.xsd are
// modelled; anything else is unmarshalled into Anys and ignored.
//
// The structs are deliberately denormalised to keep encoding/xml happy
// without a wrapper layer. Pointers mark "may be absent"; slices mark
// "zero or more".

import "encoding/xml"

type xsdSchema struct {
	XMLName         xml.Name           `xml:"http://www.w3.org/2001/XMLSchema schema"`
	Elements        []xsdElement       `xml:"http://www.w3.org/2001/XMLSchema element"`
	ComplexTypes    []xsdComplexType   `xml:"http://www.w3.org/2001/XMLSchema complexType"`
	SimpleTypes    []xsdSimpleType    `xml:"http://www.w3.org/2001/XMLSchema simpleType"`
	AttributeGroups []xsdAttributeGroup `xml:"http://www.w3.org/2001/XMLSchema attributeGroup"`
	Groups          []xsdGroup          `xml:"http://www.w3.org/2001/XMLSchema group"`
}

type xsdElement struct {
	Name string `xml:"name,attr"`
	Type string `xml:"type,attr"`
	Ref  string `xml:"ref,attr"`
}

type xsdComplexType struct {
	Name string `xml:"name,attr"`

	Sequence *xsdSequence `xml:"http://www.w3.org/2001/XMLSchema sequence"`
	Choice   *xsdChoice   `xml:"http://www.w3.org/2001/XMLSchema choice"`
	Group    *xsdGroupRef `xml:"http://www.w3.org/2001/XMLSchema group"`

	Attributes      []xsdAttribute            `xml:"http://www.w3.org/2001/XMLSchema attribute"`
	AttributeGroups []xsdAttributeGroupRef    `xml:"http://www.w3.org/2001/XMLSchema attributeGroup"`
}

type xsdSequence struct {
	Elements []xsdElement `xml:"http://www.w3.org/2001/XMLSchema element"`
}

type xsdChoice struct {
	Elements []xsdElement `xml:"http://www.w3.org/2001/XMLSchema element"`
}

type xsdGroup struct {
	Name   string     `xml:"name,attr"`
	Choice *xsdChoice `xml:"http://www.w3.org/2001/XMLSchema choice"`
}

type xsdGroupRef struct {
	Ref string `xml:"ref,attr"`
}

type xsdAttribute struct {
	Name       string         `xml:"name,attr"`
	Type       string         `xml:"type,attr"`
	Use        string         `xml:"use,attr"`
	SimpleType *xsdSimpleType `xml:"http://www.w3.org/2001/XMLSchema simpleType"`
}

type xsdAttributeGroup struct {
	Name       string                 `xml:"name,attr"`
	Attributes []xsdAttribute         `xml:"http://www.w3.org/2001/XMLSchema attribute"`
}

type xsdAttributeGroupRef struct {
	Ref string `xml:"ref,attr"`
}

type xsdSimpleType struct {
	Name        string          `xml:"name,attr"`
	Restriction *xsdRestriction `xml:"http://www.w3.org/2001/XMLSchema restriction"`
}

type xsdRestriction struct {
	Base         string             `xml:"base,attr"`
	Enumerations []xsdEnumeration   `xml:"http://www.w3.org/2001/XMLSchema enumeration"`
}

type xsdEnumeration struct {
	Value string `xml:"value,attr"`
}
