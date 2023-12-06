package typegen

import (
	"bytes"
	"fmt"
	"io"
	"path"
	"reflect"
	"strings"
	"text/template"
)

const GlobalTemplate = `{{- range $namespace, $types := . -}}
{{- range $type := $types}}{{ if eq (printf "%T" $type) "*typegen.EnumDef" }}{{$type|SerializeEnum}}{{end}}{{end}}{{end}}
{{- range $namespace, $types := .}}
export declare namespace {{$namespace}} {
{{- range $type := $types}}
{{if $type.Doc| ne ""}}// {{$type.Doc -}}{{end}}
{{$type|Serialize}}{{end}}
}{{end}}
`

const RecordTemplate = `{{ range $field := .Embedded}} {{$field | RefName}} & {{end}}{
{{- range $field := .Fields}}
	{{- if hasPrefix $field.Doc "@deprecated" }}
	/** {{ $field.Doc }} */
	{{$field | Row}}
	{{- else }}
	{{$field | Row}}{{if $field.Doc| ne ""}} // {{$field.Doc}}{{end}}
	{{- end }}
{{- end}}
}
`

type TypeToString = func(t reflect.Type) string

func typeToString(t reflect.Type, getTypeName TypeToString, stringifyType TypeToString) string {
	k := t.Kind()
	customT := stringifyType(t)
	if customT != "" {
		return customT
	}
	switch {
	case k == reflect.Ptr:
		t = indirect(t)
		return fmt.Sprintf("%s | null", typeToString(t, getTypeName, stringifyType))
	case k == reflect.Struct:
		if isDate(t) {
			return "string"
		}
		return getTypeName(t)
	case isNumber(k) && isEnum(t):
		return getTypeName(t)
	case isNumber(k):
		return "number"
	case k == reflect.String && isEnum(t):
		return getTypeName(t)
	case k == reflect.String:
		return "string"
	case k == reflect.Bool:
		return "boolean"
	case k == reflect.Slice || k == reflect.Array:
		return fmt.Sprintf("Array<%s> | null", typeToString(t.Elem(), getTypeName, stringifyType))
	case k == reflect.Interface:
		return "any"
	case k == reflect.Map:
		KeyType, ValType := typeToString(t.Key(), getTypeName, stringifyType), typeToString(t.Elem(), getTypeName, stringifyType)
		return fmt.Sprintf("Record<%s, %s> |  null", KeyType, ValType)
	}
	return "any"
}

func stringifyCustom(t reflect.Type) string {
	return ""
}

type Stringifier = func(t reflect.Type) string

type TsTypesConfig struct {
	enumsWithPrefix bool
}

type TsTypesOption func(*TsTypesConfig)

func EnumsWithPrefix(value bool) TsTypesOption {
	return func(c *TsTypesConfig) {
		c.enumsWithPrefix = value
	}
}

func PrintTsTypes(parser *Parser, w io.Writer, stringify Stringifier, opts ...TsTypesOption) {
	var config TsTypesConfig
	for _, opt := range opts {
		opt(&config)
	}

	enumName := func(t *EnumDef) string {
		if config.enumsWithPrefix {
			pkg := path.Base(t.GetPackage())
			return fmt.Sprintf("%s_%sEnum", pkg, t.GetName())
		} else {
			return fmt.Sprintf("%sEnum", t.GetName())
		}
	}

	if stringify == nil {
		stringify = stringifyCustom
	}
	output := make(map[string][]IType)

	for _, m := range parser.visitOrder {
		pkg := parser.seen[m].GetPackage()
		output[path.Base(pkg)] = append(output[path.Base(pkg)], parser.seen[m])
	}

	recordToString := func(r *RecordDef) string {
		tmpl, err := template.New("content").Funcs(template.FuncMap{
			"RefName": func(t reflect.Type) string {
				return parser.GetVisited(t).RefName()
			},
			"Row": func(t RecordField) string {
				keyName := t.Key
				// fmt.Println("[printer]", t.Tag, t.Type, t)
				fieldType := t.Tag.FieldType
				if t.Tag.FieldName != "" {
					keyName = t.Tag.FieldName
				}
				// fmt.Println("[printer]", t.Tag, t.Type, t)

				if t.Type != nil {
					visited := parser.GetVisited(t.Type)
					if visited != nil {
						fieldType = visited.RefName()
					} else {
						// fmt.Println("[printer]", t.Tag, t.Type, visited)
						fieldType = typeToString(t.Type, func(t reflect.Type) string {
							// fmt.Println("[printer-tostring]", t, parser.GetVisited(t))
							return parser.GetVisited(t).RefName()
						}, stringify)
					}
				}
				optionalText := ""
				if t.Tag.State == Optional {
					optionalText = "?"
				}
				nullText := ""
				if t.Tag.State == Null || t.IsRef {
					nullText = " | null"
				}

				return fmt.Sprintf("%s%s: %s%s", keyName, optionalText, fieldType, nullText)
			},
			"hasPrefix": strings.HasPrefix,
		}).Parse(RecordTemplate)
		panicIf(err)
		w := &bytes.Buffer{}
		err = tmpl.Execute(w, r)
		panicIf(err)
		return w.String()
	}

	tmpl, err := template.New("types template").Funcs(template.FuncMap{
		"SerializeEnum": func(t IType) string {
			switch v := t.(type) {
			case *EnumDef:
				res := fmt.Sprintf("export const %s = {\n", enumName(v))
				for _, v := range v.Values {
					res += fmt.Sprintf("    \"%s\": %s,\n", v.name, v.Stringify())
				}
				res += "} as const\n"
				return res
			}
			return ""
		},
		"Serialize": func(t IType) string {
			switch v := t.(type) {
			case *RecordDef:
				return fmt.Sprintf("export type %s = %s", v.Name, recordToString(v))
			case *EnumDef:
				res := ""
				res += fmt.Sprintf("export type %s = typeof %s[keyof typeof %s]", t.GetName(), enumName(v), enumName(v))
				return res
			case *TypeDef:
				typeStr := typeToString(v.T, func(t reflect.Type) string {
					return parser.GetVisited(t).RefName()
				}, stringify)
				return fmt.Sprintf("export type %s = %s", v.GetName(), typeStr)
			}
			return "1"
		},
	}).Parse(GlobalTemplate)
	panicIf(err)

	err = tmpl.Execute(w, output)
	panicIf(err)
}
