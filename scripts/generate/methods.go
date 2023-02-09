package main

import (
	"fmt"
	"strings"
	"text/template"
)

var (
	inputFileBranchTmpl             = template.Must(template.New("inputFileBranch").Parse(inputFileBranch))
	inputMediaParamsBranchTmpl      = template.Must(template.New("inputMediaParamsBranch").Parse(inputMediaParamsBranch))
	inputMediaArrayParamsBranchTmpl = template.Must(template.New("inputMediaArrayParamsBranch").Parse(inputMediaArrayParamsBranch))
)

func generateMethods(d APIDescription) error {
	file := strings.Builder{}
	file.WriteString(`
// THIS FILE IS AUTOGENERATED. DO NOT EDIT.
// Regen by running 'go generate' in the repo root.

package gotgbot

import (
	"encoding/json"
	"fmt"
	"strconv"
)
`)

	for _, tgMethodName := range orderedMethods(d) {
		tgMethod := d.Methods[tgMethodName]

		method, err := generateMethodDef(d, tgMethod)
		if err != nil {
			return fmt.Errorf("failed to generate method definition of %s: %w", tgMethodName, err)
		}

		file.WriteString(method)
	}

	return writeGenToFile(file, "gen_methods.go")
}

func generateMethodDef(d APIDescription, tgMethod MethodDescription) (string, error) {
	method := strings.Builder{}

	methodSignature, retTypes, optionalsStruct, err := generateMethodSignature(d, tgMethod)
	if err != nil {
		return "", err
	}

	if optionalsStruct != "" {
		method.WriteString("\n" + optionalsStruct)
	}

	// Generate method description
	desc, err := tgMethod.description()
	if err != nil {
		return "", fmt.Errorf("failed to generate method description for %s: %w", tgMethod.Name, err)
	}

	// Generate list of default return values (for error handling).
	defaultRetVals := strings.Join(getDefaultReturnVals(d, retTypes), ", ")

	// Generate method contents, setting up values in expected format
	valueGen, hasData, err := tgMethod.argsToValues(d, tgMethod.Name, defaultRetVals)
	if err != nil {
		return "", fmt.Errorf("failed to generate url values for method %s: %w", tgMethod.Name, err)
	}

	// Generate return statements
	returnGen, err := returnValues(d, retTypes)
	if err != nil {
		return "", fmt.Errorf("failed to generate return values: %w", err)
	}

	method.WriteString(desc)
	method.WriteString("\nfunc (bot *Bot) " + methodSignature + " {")
	method.WriteString("\n	v := map[string]string{}")
	method.WriteString(valueGen)
	method.WriteString("\n")

	method.WriteString("\nvar reqOpts *RequestOpts")
	method.WriteString("\nif opts != nil {")
	method.WriteString("\n	reqOpts = opts.RequestOpts")
	method.WriteString("\n}")
	method.WriteString("\n")

	// If sending data, we need to do it over POST
	if hasData {
		method.WriteString("\nr, err := bot.Request(\"" + tgMethod.Name + "\", v, data, reqOpts)")
	} else {
		method.WriteString("\nr, err := bot.Request(\"" + tgMethod.Name + "\", v, nil, reqOpts)")
	}

	method.WriteString("\n	if err != nil {")
	method.WriteString("\n		return " + defaultRetVals + ", err")
	method.WriteString("\n	}")
	method.WriteString("\n")

	method.WriteString(returnGen)
	method.WriteString("\n}")

	return method.String(), nil
}

func generateMethodSignature(d APIDescription, tgMethod MethodDescription) (string, []string, string, error) {
	retTypes, err := tgMethod.GetReturnTypes(d)
	if err != nil {
		return "", nil, "", fmt.Errorf("failed to get return for %s: %w", tgMethod.Name, err)
	}

	args, optionalsStruct, err := tgMethod.getArgs()
	if err != nil {
		return "", nil, "", fmt.Errorf("failed to get args for method %s: %w", tgMethod.Name, err)
	}

	methodSignature := strings.Title(tgMethod.Name) + "(" + args + ") (" + strings.Join(retTypes, ", ") + ", error)"
	return methodSignature, retTypes, optionalsStruct, nil
}

func returnValues(d APIDescription, retTypes []string) (string, error) {
	retType := retTypes[0]
	retVarType := retType
	retVarName := getRetVarName(retVarType)
	addr := ""
	if isPointer(retVarType) {
		retVarType = strings.TrimLeft(retVarType, "*")
		addr = "&"
	}

	if len(retTypes) == 2 && retTypes[1] == "bool" {
		// Manual bit of code injected for dual returns of TYPE+bool.
		// If the unmarshal into TYPE is a success, we return TYPE + true. This populates an expected bool value.
		// if TYPE unmarshal fails, we see if it unmarshals to bool; if success, return nil + bool
		// if bool unmarshal fails, this is an unknown type; fail completely.
		defaultRetVal := getDefaultTypeVal(d, retVarType)
		return fmt.Sprintf(`
var %s %s
if err := json.Unmarshal(r, &%s); err != nil {
	var b bool
	if err := json.Unmarshal(r, &b); err != nil {
		return %s, false, err
	}
	return %s, b, nil
}
return %s, true, nil
`, retVarName, retVarType, retVarName, defaultRetVal, defaultRetVal, addr+retVarName), nil
	} else if len(retTypes) >= 2 {
		return "", fmt.Errorf("no existing support for multiple return types of %v", retTypes)
	}

	returnString := strings.Builder{}

	if rawType := strings.TrimPrefix(retType, "[]"); isArray(retType) && len(d.Types[rawType].Subtypes) != 0 {
		// Handle interface array returns such as []ChatMember from GetChatAdministrators
		returnString.WriteString(fmt.Sprintf("\nreturn unmarshal%sArray(r)", rawType))
	} else if len(d.Types[retType].Subtypes) != 0 {
		// Handle interface returns such as ChatMember from GetChatMember
		returnString.WriteString(fmt.Sprintf("\nreturn unmarshal%s(r)", retType))
	} else {
		returnString.WriteString("\nvar " + retVarName + " " + retVarType)
		returnString.WriteString("\nreturn " + addr + retVarName + ", json.Unmarshal(r, &" + retVarName + ")")
	}

	return returnString.String(), nil
}

func (m MethodDescription) description() (string, error) {
	description := strings.Builder{}

	for idx, d := range m.Description {
		text := d
		if idx == 0 {
			text = strings.Title(m.Name) + " " + d
		}

		description.WriteString("\n// " + text)
	}

	for _, f := range m.Fields {
		if !f.Required {
			continue
		}

		prefType, err := f.getPreferredType()
		if err != nil {
			return "", err
		}

		description.WriteString("\n// - " + snakeToCamel(f.Name) + " (type " + prefType + "): " + f.Description)
	}

	// All methods have the optional `RequestOpts`
	description.WriteString("\n// - opts (type " + m.optsName() + "): All optional parameters.")

	description.WriteString("\n// " + m.Href)

	return description.String(), nil
}

func (m MethodDescription) argsToValues(d APIDescription, methodName string, defaultRetVal string) (string, bool, error) {
	hasData := false
	bd := strings.Builder{}

	var optionals []Field
	for _, f := range m.Fields {
		goParam := snakeToCamel(f.Name)
		if !f.Required {
			optionals = append(optionals, f)
			continue
		}

		contents, data, err := generateValue(d, methodName, f, goParam, defaultRetVal)
		if err != nil {
			return "", false, err
		}
		bd.WriteString(contents)
		hasData = hasData || data
	}

	if len(optionals) > 0 {
		bd.WriteString("\nif opts != nil {")
		for _, f := range optionals {
			goParam := "opts." + snakeToTitle(f.Name)
			contents, data, err := generateValue(d, methodName, f, goParam, defaultRetVal)
			if err != nil {
				return "", false, err
			}

			bd.WriteString(contents)
			hasData = hasData || data
		}
		bd.WriteString("\n}")
	}

	if hasData {
		return "\ndata := map[string]NamedReader{}" + bd.String(), true, nil
	}

	return bd.String(), false, nil
}

func generateValue(d APIDescription, methodName string, f Field, goParam string, defaultRetVal string) (string, bool, error) {
	fieldType, err := f.getPreferredType()
	if err != nil {
		return "", false, fmt.Errorf("failed to get preferred type: %w", err)
	}

	stringer := goTypeStringer(fieldType)
	if stringer != "" {
		if !f.Required {
			// Ints and Floats should generally not be sent if they're 0.
			if fieldType == "int64" || fieldType == "float64" {
				// Editing an inline query requires the inline_message_id. However, if we send the empty chat_id with it,
				// it'll fail with a "chat not found" error, since it believes were trying to access the chat with ID 0.
				// To avoid this, we want to make sure not to add default integers or floats to requests.
				// This is a good rule of thumb, however... it doesn't ALWAYS work.
				// So, any exceptions should go here:
				if methodName == "sendPoll" && f.Name == "correct_option_id" {
					// correct_option_id (in sendPoll) is dependent on the "type" field being "quiz".
					// It isn't used for "regular" polls. It still needs to be sent when the value is "0".
					return fmt.Sprintf(`
if opts.Type == "quiz" {
	// correct_option_id should always be set when the type is "quiz" - it doesnt need to be set for type "regular".
	%s
}`, addURLParam(f, stringer, goParam)), false, nil
				}

				// TODO: Simplify this to avoid ANY unrequired default values instead of just int/float?
				return fmt.Sprintf(`
if %s != %s {
	%s
}`, goParam, getDefaultTypeVal(d, fieldType), addURLParam(f, stringer, goParam)), false, nil
			}

			if methodName == "editForumTopic" && f.Name == "icon_custom_emoji_id" {
				return fmt.Sprintf(`
// icon_custom_emoji_id has different behaviour if it's empty, or if it's unspecified; so we need to handle that.
if %s != nil {
	v["%s"] = %s
}`, goParam, f.Name, fmt.Sprintf(goTypeStringer("*"+fieldType), goParam)), false, nil
			}
		}

		return "\n" + addURLParam(f, stringer, goParam), false, nil
	}

	bd := strings.Builder{}
	hasData := false
	switch fieldType {
	case tgTypeInputFile:
		hasData = true
		err = inputFileBranchTmpl.Execute(&bd, readerBranchesData{
			GoParam:       goParam,
			DefaultReturn: defaultRetVal,
			Name:          f.Name,
			AllowString:   len(f.Types) > 1, // Either "InputFile", or "InputFile or String"; so if two types, strings are supported.
		})
		if err != nil {
			return "", false, fmt.Errorf("failed to execute branch reader template: %w", err)
		}

	case tgTypeInputMedia:
		hasData = true

		err = inputMediaParamsBranchTmpl.Execute(&bd, readerBranchesData{
			GoParam:       goParam,
			DefaultReturn: defaultRetVal,
			Name:          f.Name,
		})
		if err != nil {
			return "", false, fmt.Errorf("failed to execute inputmedia branch template: %w", err)
		}

	case "[]" + tgTypeInputMedia:
		hasData = true

		err = inputMediaArrayParamsBranchTmpl.Execute(&bd, readerBranchesData{
			GoParam:       goParam,
			DefaultReturn: defaultRetVal,
			Name:          f.Name,
		})
		if err != nil {
			return "", false, fmt.Errorf("failed to execute inputmedia array branch template: %w", err)
		}

	default:
		if isArray(fieldType) || fieldType == tgTypeReplyMarkup {
			bd.WriteString("\nif " + goParam + " != nil {")
		}

		bd.WriteString("\n	bs, err := json.Marshal(" + goParam + ")")
		bd.WriteString("\n	if err != nil {")
		bd.WriteString("\n		return " + defaultRetVal + ", fmt.Errorf(\"failed to marshal field " + f.Name + ": %w\", err)")
		bd.WriteString("\n	}")
		bd.WriteString("\n	v[\"" + f.Name + "\"] = string(bs)")

		if isArray(fieldType) || fieldType == tgTypeReplyMarkup {
			bd.WriteString("\n}")
		}
	}
	return bd.String(), hasData, nil
}

func addURLParam(f Field, stringer string, goParam string) string {
	return fmt.Sprintf(`v["%s"] = %s`, f.Name, fmt.Sprintf(stringer, goParam))
}

func getRetVarName(retType string) string {
	for isPointer(retType) {
		retType = strings.TrimPrefix(retType, "*")
	}

	for isArray(retType) {
		retType = strings.TrimPrefix(retType, "[]")
	}

	return strings.ToLower(retType[:1])
}

func (m MethodDescription) getArgs() (string, string, error) {
	var requiredArgs []string
	optionals := strings.Builder{}

	for _, f := range m.Fields {
		fieldType, err := f.getPreferredType()
		if err != nil {
			return "", "", fmt.Errorf("failed to get preferred type: %w", err)
		}

		if f.Required {
			requiredArgs = append(requiredArgs, fmt.Sprintf("%s %s", snakeToCamel(f.Name), fieldType))
			continue
		}

		optionals.WriteString("\n// " + f.Description)
		if m.Name == "editForumTopic" && f.Name == "icon_custom_emoji_id" {
			// Special case for the editForumTopic method's icon_custom_emoji_id param, which has different behaviour
			// between empty and unset values.
			optionals.WriteString("\n" + fmt.Sprintf("%s %s", snakeToTitle(f.Name), "*"+fieldType))
			continue
		}
		optionals.WriteString("\n" + fmt.Sprintf("%s %s", snakeToTitle(f.Name), fieldType))
	}

	optionalsName := m.optsName()
	optionalsStructBuilder := strings.Builder{}
	optionalsStructBuilder.WriteString(fmt.Sprintf("\n// %s is the set of optional fields for Bot.%s.", optionalsName, strings.Title(m.Name)))
	optionalsStructBuilder.WriteString("\ntype " + optionalsName + " struct {")
	optionalsStructBuilder.WriteString(optionals.String())
	optionalsStructBuilder.WriteString("\n// RequestOpts are an additional optional field to configure timeouts for individual requests")
	optionalsStructBuilder.WriteString("\nRequestOpts *RequestOpts")
	optionalsStructBuilder.WriteString("\n}")

	requiredArgs = append(requiredArgs, fmt.Sprintf("opts *%s", optionalsName))

	return strings.Join(requiredArgs, ", "), optionalsStructBuilder.String(), nil
}

type readerBranchesData struct {
	GoParam       string
	DefaultReturn string
	Name          string
	AllowString   bool
}

const inputFileBranch = `
if {{.GoParam}} != nil {
	err := attachFile("{{.Name}}", {{.GoParam}}, v, data, {{.AllowString}})
	if err != nil {
		return {{.DefaultReturn}}, fmt.Errorf("failed to attach file: %w", err)
	}
}`

const inputMediaParamsBranch = `
inputMediaBs, err := {{.GoParam}}.InputMediaParams("{{.Name}}" , data)
if err != nil {
	return {{.DefaultReturn}}, fmt.Errorf("failed to marshal field {{.Name}}: %w", err)
}
v["{{.Name}}"] = string(inputMediaBs)`

const inputMediaArrayParamsBranch = `
if {{.GoParam}} != nil {
	var rawList []json.RawMessage
	for idx, im := range {{.GoParam}} {
		inputMediaBs, err := im.InputMediaParams("{{.Name}}" + strconv.Itoa(idx), data)
		if err != nil {
			return {{.DefaultReturn}}, fmt.Errorf("failed to marshal InputMedia list item %d for field {{.Name}}: %w", idx, err)
		}
		rawList = append(rawList, inputMediaBs)
	}
	bs, err := json.Marshal(rawList)
	if err != nil {
		return {{.DefaultReturn}}, fmt.Errorf("failed to marshal raw json list of InputMedia for field: {{.Name}} %w", err)
	}
	v["{{.Name}}"] = string(bs)
}`
