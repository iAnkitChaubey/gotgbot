package main

import (
	"sort"
	"strings"
)

func generateTypes(d APIDescription) error {
	file := strings.Builder{}
	file.WriteString(`
// THIS FILE IS AUTOGENERATED. DO NOT EDIT.
// Regen by running 'go generate' in the repo root.

package gen

import "encoding/json"

`)

	// TODO: Obtain ordered map to retain tg ordering
	var types []string
	for k := range d.Types {
		types = append(types, k)
	}
	sort.Strings(types)

	for _, tgTypeName := range types {
		file.WriteString(generateTypeDef(d, tgTypeName))
	}

	return writeGenToFile(file, "gen/gen_types.go")
}

func generateTypeDef(d APIDescription, tgTypeName string) string {
	typeDef := strings.Builder{}
	tgType := d.Types[tgTypeName]

	for _, d := range tgType.Description {
		typeDef.WriteString("\n// " + d)
	}
	typeDef.WriteString("\n// " + tgType.Href)
	if len(tgType.Fields) == 0 {
		// todo: Generate interface methods for child functions
		typeDef.WriteString("\ntype " + tgTypeName + " interface{}")
		return typeDef.String()
	}

	var genCustomMarshalFields []TypeFields
	typeDef.WriteString("\ntype " + tgTypeName + " struct {")
	for _, fields := range tgType.Fields {
		goType := toGoTypes(fields.Types[0]) // TODO: NOT just default to first type

		// we don't write the type field since it isnt something that should be customised. This is set in the custom marshaller.
		if isSubtypeOf(tgType.SubtypeOf, "InputMedia") && fields.Field == "type" {
			continue
		}

		if isTgType(d.Types, goType) && strings.HasPrefix(fields.Description, "Optional.") {
			goType = "*" + goType
		}

		if isTgArray(fields.Types[0]) { // TODO: NOT just default to first type
			genCustomMarshalFields = append(genCustomMarshalFields, fields)
		}

		typeDef.WriteString("\n// " + fields.Description)
		typeDef.WriteString("\n" + snakeToTitle(fields.Field) + " " + goType + " `json:\"" + fields.Field + "\"`")
	}

	typeDef.WriteString("\n}")

	if len(genCustomMarshalFields) > 0 {
		typeDef.WriteString(genCustomMarshal(tgTypeName, genCustomMarshalFields))
	}

	return typeDef.String()
}

func genCustomMarshal(name string, fields []TypeFields) string {
	marshalDef := strings.Builder{}

	marshalDef.WriteString("\n")
	marshalDef.WriteString("\nfunc (v " + name + ") MarshalJSON() ([]byte, error) {")
	marshalDef.WriteString("\n	type alias " + name)
	marshalDef.WriteString("\n	a := struct{")
	if strings.HasPrefix(name, "InputMedia") {
		marshalDef.WriteString("\n		Type string `json:\"type\"`")
	}
	marshalDef.WriteString("\n		alias")
	marshalDef.WriteString("\n	}{")

	if strings.HasPrefix(name, "InputMedia") {
		marshalDef.WriteString("\n		Type: \"" + strings.ToLower(strings.TrimPrefix(name, "InputMedia")) + "\",")
	}
	marshalDef.WriteString("\n		alias: (alias)(v),")
	marshalDef.WriteString("\n	}")
	for _, f := range fields {
		marshalDef.WriteString("\n	if a." + snakeToTitle(f.Field) + " == nil {")
		marshalDef.WriteString("\n		a." + snakeToTitle(f.Field) + " = make(" + toGoTypes(f.Types[0]) + ", 0)")
		marshalDef.WriteString("\n	}")
	}
	marshalDef.WriteString("\nreturn json.Marshal(a)")
	marshalDef.WriteString("\n}")

	return marshalDef.String()
}

func isSubtypeOf(types []string, parentType string) bool {
	for _, t := range types {
		if t == parentType {
			return true
		}
	}
	return false
}
