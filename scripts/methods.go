package main

import (
	"bufio"
	"bytes"
	"fmt"
	"sort"
	"strings"
	"text/template"
)

func generateMethods(d APIDescription) error {
	file := strings.Builder{}
	file.WriteString(`
// THIS FILE IS AUTOGENERATED. DO NOT EDIT.
// Regen by running 'go generate' in the repo root.

package gen
import (
	urlLib "net/url" // renamed to avoid clashes with url vars
	"encoding/json"
	"strconv"
	"fmt"
	"io"
)
`)

	// TODO: Obtain ordered map to retain tg ordering
	var methods []string
	for k := range d.Methods {
		methods = append(methods, k)
	}
	sort.Strings(methods)

	for _, tgMethodName := range methods {
		tgMethod := d.Methods[tgMethodName]
		file.WriteString(generateMethodDef(d, tgMethod, tgMethodName))
	}

	return writeGenToFile(file, "gen/gen_methods.go")
}

func generateMethodDef(d APIDescription, tgMethod MethodDescription, tgMethodName string) string {
	method := strings.Builder{}

	// defaulting to [0] is ok because its either message or bool
	retType := toGoType(tgMethod.Returns[0])
	if isTgType(d.Types, retType) {
		retType = "*" + retType
	}
	defaultRetVal := getDefaultReturnVal(retType)

	args, optionalsStruct := getArgs(tgMethodName, tgMethod)
	if optionalsStruct != "" {
		method.WriteString("\n" + optionalsStruct)
	}

	for _, d := range tgMethod.Description {
		method.WriteString("\n// " + d)
	}
	method.WriteString("\n// " + tgMethod.Href)

	method.WriteString("\nfunc (bot Bot) " + strings.Title(tgMethodName) + "(" + args + ") (" + retType + ", error) {")

	valueGen, hasData := methodArgsToValues(tgMethod, defaultRetVal)
	method.WriteString("\n	v := urlLib.Values{}")
	if hasData {
		method.WriteString("\n	data := map[string]NamedReader{}")
	}

	method.WriteString(valueGen)
	method.WriteString("\n")

	if hasData {
		method.WriteString("\nr, err := bot.Post(\"" + tgMethodName + "\", v, data)")
	} else {
		method.WriteString("\nr, err := bot.Get(\"" + tgMethodName + "\", v)")
	}
	method.WriteString("\n	if err != nil {")
	method.WriteString("\n		return " + defaultRetVal + ", err")
	method.WriteString("\n	}")
	method.WriteString("\n")

	retVarType := retType
	retVarName := getRetVarName(retVarType)
	isPointer := strings.HasPrefix(retVarType, "*")
	addr := ""
	if isPointer {
		retVarType = strings.TrimLeft(retVarType, "*")
		addr = "&"
	}
	method.WriteString("\nvar " + retVarName + " " + retVarType)
	method.WriteString("\nreturn " + addr + retVarName + ", json.Unmarshal(r, &" + retVarName + ")")
	method.WriteString("\n}")

	return method.String()
}

func methodArgsToValues(method MethodDescription, defaultRetVal string) (string, bool) {
	hasData := false
	bd := strings.Builder{}
	for _, f := range method.Fields {
		goParam := snakeToCamel(f.Name)
		if !f.Required {
			goParam = "opts." + snakeToTitle(f.Name)
		}

		fieldType := getPreferredType(f)
		stringer := goTypeStringer(toGoType(fieldType))
		if stringer != "" {
			bd.WriteString("\nv.Add(\"" + f.Name + "\", " + fmt.Sprintf(stringer, goParam) + ")")
			continue
		}

		if fieldType == "InputFile" {
			// TODO: support case where its just inputfile and not string

			hasData = true
			t, err := template.New("readers").Parse(readerBranches)
			if err != nil {
				panic("failed to parse template: " + err.Error())
			}
			buf := bytes.Buffer{}
			w := bufio.NewWriter(&buf)
			err = t.Execute(w, readerBranchesStruct{
				GoParam:       goParam,
				DefaultReturn: defaultRetVal,
				Parameter:     f.Name,
			})
			if err != nil {
				panic("failed to execute template: " + err.Error())
			}
			if err := w.Flush(); err != nil {
				panic("failed to flush template: " + err.Error())
			}
			bd.WriteString(buf.String())
			continue

		} else if strings.Contains(fieldType, "InputMedia") {
			offset := ""
			if isTgArray(fieldType) {
				bd.WriteString("\nfor idx, im := range " + goParam + " {")
				goParam = "im"
				offset = " + strconv.Itoa(idx)"
			}
			hasData = true

			// dont use goParam since that contains the `opts.` section
			bytesVarName := "inputMediaBs"
			bd.WriteString("\n	" + bytesVarName + ", err := " + goParam + ".InputMediaParams(\"" + f.Name + "\"" + offset + " , data)")
			bd.WriteString("\n	if err != nil {")
			bd.WriteString("\n		return " + defaultRetVal + ", fmt.Errorf(\"failed to marshal " + f.Name + ": %w\", err)")
			bd.WriteString("\n	}")
			bd.WriteString("\n	v.Add(\"" + f.Name + "\", string(" + bytesVarName + "))")

			if isTgArray(fieldType) {
				bd.WriteString("\n}")
			}

			continue
		}

		if isTgArray(fieldType) {
			bd.WriteString("\nif " + goParam + " != nil {")
		}

		// dont use goParam since that contains the `opts.` section
		bytesVarName := snakeToCamel(f.Name) + "Bs"

		bd.WriteString("\n	" + bytesVarName + ", err := json.Marshal(" + goParam + ")")
		bd.WriteString("\n	if err != nil {")
		bd.WriteString("\n		return " + defaultRetVal + ", fmt.Errorf(\"failed to marshal " + f.Name + ": %w\", err)")
		bd.WriteString("\n	}")
		bd.WriteString("\n	v.Add(\"" + f.Name + "\", string(" + bytesVarName + "))")

		if isTgArray(fieldType) {
			bd.WriteString("\n}")
		}

	}

	return bd.String(), hasData
}

func getRetVarName(retType string) string {
	for strings.HasPrefix(retType, "*") {
		retType = strings.TrimPrefix(retType, "*")
	}
	for strings.HasPrefix(retType, "[]") {
		retType = strings.TrimPrefix(retType, "[]")
	}
	return strings.ToLower(retType[:1])
}

func getArgs(name string, method MethodDescription) (string, string) {
	var requiredArgs []string
	optionals := strings.Builder{}
	for _, f := range method.Fields {
		fieldType := getPreferredType(f)
		goType := toGoType(fieldType)
		if f.Required {
			requiredArgs = append(requiredArgs, fmt.Sprintf("%s %s", snakeToCamel(f.Name), goType))
			continue
		}

		optionals.WriteString("\n// " + f.Description)
		optionals.WriteString("\n" + fmt.Sprintf("%s %s", snakeToTitle(f.Name), goType))

	}
	optionalsStruct := ""

	if optionals.Len() > 0 {
		optionalsName := snakeToTitle(name) + "Opts"
		bd := strings.Builder{}
		bd.WriteString("\ntype " + optionalsName + " struct {")
		bd.WriteString(optionals.String())
		bd.WriteString("\n}")
		optionalsStruct = bd.String()

		requiredArgs = append(requiredArgs, fmt.Sprintf("opts %s", optionalsName))
	}

	return strings.Join(requiredArgs, ", "), optionalsStruct
}

type readerBranchesStruct struct {
	GoParam       string
	DefaultReturn string
	Parameter     string
}

const readerBranches = `
if {{.GoParam}} != nil {
	if s, ok := {{.GoParam}}.(string); ok {
		v.Add("{{.Parameter}}", s)
	} else if r, ok := {{.GoParam}}.(io.Reader); ok {
		v.Add("{{.Parameter}}", "attach://{{.Parameter}}")
		data["{{.Parameter}}"] = NamedReader{File: r}
	} else if nf, ok := {{.GoParam}}.(NamedReader); ok {
		v.Add("{{.Parameter}}", "attach://{{.Parameter}}")
		data["{{.Parameter}}"] = nf
	} else {
		return {{.DefaultReturn}}, fmt.Errorf("unknown type for InputFile: %T",{{.GoParam}})
	}
}
`