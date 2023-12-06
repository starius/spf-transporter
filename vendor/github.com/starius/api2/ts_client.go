package api2

import (
	"encoding/json"
	"io"
	"os"
	"path/filepath"
	"reflect"
	"text/template"

	"github.com/starius/api2/typegen"
)

type funcer interface {
	Func() interface{}
}

const tsClient = `/* eslint-disable */
// prettier-disable
// prettier-ignore
// Code generated by api2. DO NOT EDIT.
import {route} from "./utils"

export const api = {
{{- range $key, $services := .}}
{{$key}}: {
	{{- range $service, $methods := $services }}
	{{$service}}: {
		{{- range $info := $methods}}
			{{$info.FnInfo.Method}}: route<{{.ReqType}}, {{.ResType}}>(
				"{{.Method}}", "{{.Path}}",
				{{.TypeInfoReq}},
				{{.TypeInfoRes}}),{{end}}
	},{{end}}
},{{- end}}
}
`

const templateHeaderDefault = `/* eslint-disable */
// prettier-disable
// prettier-ignore
// Code generated by api2. DO NOT EDIT.
import axios from "axios"
export function cancelable<T>(p: T, source): T & { cancel: () => void } {
	let promiseAny = p as any;
	promiseAny.cancel = () => source.cancel("Request was canceled");
	let resolve = promiseAny.then.bind(p);
	promiseAny.then = (res, rej) => cancelable(resolve(res, rej), source);
	return promiseAny;
}
type RequestMapping = Record<string, string[]>
type ResponseMapping = Record<string, string[]>

export function route<Req, Res>(method:string, url:string, requestMapping:RequestMapping, responseMapping:ResponseMapping) {
	let headersReqSet = new Set(requestMapping.headers)
	let queryReqSet = new Set(requestMapping.query)
	let shouldProcess = headersReqSet.size || queryReqSet.size;
	return Object.assign((data: Req)=>{
		const c = axios.CancelToken.source()
		data = {...data};
		let headers = {} as any
		let query = {} as any
		if (data && shouldProcess) {
			for(let k in data) {
					if(headersReqSet.has(k)) {
						headers[k] = data[k]
						delete data[k];
					} else if(queryReqSet.has(k)) {
						query[k] = data[k]
						delete data[k];
					}
				}
		}
		let queryAsString = new URLSearchParams(Object.values(query)).toString()
		return cancelable(axios.request<Res>({ method, url: url + (queryAsString? '?' + queryAsString : '') , data, cancelToken: c.token, headers  }).then(el=>{
			let res = el.data;
			for(let k of responseMapping.header) {
				if(el.headers[k]) {
					res[k] = el.headers[k]
				}
			}
			return res;
		}), c)
	}, {method, url})
}
`

var tsClientTemplate = template.Must(template.New("ts_static_client").Parse(tsClient))
var tsClientUtilsTemplate = template.Must(template.New("ts_static_client_utils").Parse(templateHeaderDefault))

type TypesGenConfig struct {
	OutDir          string
	ClientTemplate  *template.Template
	Routes          []interface{}
	Types           []interface{}
	Blacklist       []BlacklistItem
	EnumsWithPrefix bool
}

func panicIf(err error) {
	if err != nil {
		panic(err)
	}
}

var jsonRawMessageType = reflect.TypeOf((*json.RawMessage)(nil)).Elem()

func CustomParse(t reflect.Type) (typegen.IType, bool) {
	if t == jsonRawMessageType {
		return nil, true
	}
	return nil, false
}

func SerializeCustom(t reflect.Type) string {
	if t == jsonRawMessageType {
		return "any"
	}
	return ""
}

func GenerateTSClient(options *TypesGenConfig) {
	if options.ClientTemplate == nil {
		options.ClientTemplate = tsClientTemplate
	}
	_ = os.RemoveAll(filepath.Join(options.OutDir, "gen.ts"))
	err := os.MkdirAll(options.OutDir, os.ModePerm)
	panicIf(err)
	typesFile, err := os.OpenFile(filepath.Join(options.OutDir, "gen.ts"), os.O_WRONLY|os.O_CREATE, 0755)
	panicIf(err)
	parser := typegen.NewParser()
	parser.CustomParse = CustomParse
	allRoutes := []Route{}
	for _, getRoutes := range options.Routes {
		genValue := reflect.ValueOf(getRoutes)
		serviceArg := reflect.New(genValue.Type().In(0)).Elem()
		routesValues := genValue.Call([]reflect.Value{serviceArg})
		routes := routesValues[0].Interface().([]Route)
		allRoutes = append(allRoutes, routes...)
	}
	genRoutes(typesFile, allRoutes, parser, options)
	if _, err := os.Stat(filepath.Join(options.OutDir, "utils.ts")); os.IsNotExist(err) {
		utilsFile, err := os.OpenFile(filepath.Join(options.OutDir, "utils.ts"), os.O_WRONLY|os.O_CREATE, 0755)
		panicIf(err)
		err = tsClientUtilsTemplate.Execute(utilsFile, nil)
		panicIf(err)
	}
	parser.ParseRaw(options.Types...)
	typegen.PrintTsTypes(parser, typesFile, SerializeCustom, typegen.EnumsWithPrefix(options.EnumsWithPrefix))
	panicIf(err)

}

func serializeTypeInfo(t *preparedType) ([]byte, error) {
	type resStruct struct {
		Query  []string `json:"query,omitempty"`
		Header []string `json:"header,omitempty"`
		Json   []string `json:"json,omitempty"`
	}
	res := resStruct{}
	for _, v := range t.HeaderMapping {
		res.Header = append(res.Header, v.Key)
	}
	for _, v := range t.QueryMapping {
		res.Query = append(res.Query, v.Key)
	}
	if t.TypeForJson != nil {
		for i := 0; i < t.TypeForJson.NumField(); i++ {
			ft := t.TypeForJson.Field(i)
			tag, err := typegen.ParseStructTag(ft.Tag)
			if err != nil {
				return nil, err
			}
			if tag.State == typegen.Ignored || tag.State == typegen.NoInfo {
				continue
			}
			name := ft.Name
			if tag.FieldName != "" {
				name = tag.FieldName
			}
			res.Json = append(res.Json, name)
		}
	}
	return json.Marshal(res)
}

type BlacklistItem struct {
	Package string
	Service string
	Handler string
}

func Matches(this *BlacklistItem, Package, Service, Handler string) bool {
	res := true
	if this.Package != "" {
		res = res && Package == this.Package
	}
	if this.Service != "" {
		res = res && Service == this.Service
	}
	if this.Handler != "" {
		res = res && Handler == this.Handler
	}
	return res
}

func genRoutes(w io.Writer, routes []Route, p *typegen.Parser, options *TypesGenConfig) {
	type routeDef struct {
		Method      string
		Path        string
		ReqType     string
		ResType     string
		Handler     interface{}
		FnInfo      FnInfo
		TypeInfoReq string
		TypeInfoRes string
	}
	m := map[string]map[string][]routeDef{}
OUTER:
	for _, route := range routes {
		handler := route.Handler
		if f, ok := handler.(funcer); ok {
			handler = f.Func()
		}

		handlerVal := reflect.ValueOf(handler)
		handlerType := handlerVal.Type()
		req := reflect.TypeOf(reflect.New(handlerType.In(1)).Elem().Interface()).Elem()
		response := reflect.TypeOf(reflect.New(handlerType.Out(0)).Elem().Interface()).Elem()
		fnInfo := GetFnInfo(route.Handler)
		for _, v := range options.Blacklist {
			if Matches(&v, fnInfo.PkgName, fnInfo.StructName, fnInfo.Method) {
				continue OUTER
			}
		}
		p.Parse(req, response)
		TypeInfoReq, err := serializeTypeInfo(prepare(req))
		panicIf(err)
		TypeInfoRes, err := serializeTypeInfo(prepare(response))
		panicIf(err)
		r := routeDef{
			ReqType:     req.String(),
			ResType:     response.String(),
			Method:      route.Method,
			Path:        route.Path,
			Handler:     route.Handler,
			FnInfo:      fnInfo,
			TypeInfoReq: string(TypeInfoReq),
			TypeInfoRes: string(TypeInfoRes),
		}

		if _, ok := m[fnInfo.PkgName]; !ok {
			m[fnInfo.PkgName] = make(map[string][]routeDef)
		}
		m[fnInfo.PkgName][fnInfo.StructName] = append(m[fnInfo.PkgName][fnInfo.StructName], r)
	}

	err := tsClientTemplate.Execute(w, m)
	if err != nil {
		panic(err)
	}
}