// SPDX-License-Identifier: 0BSD
// SPDX-FileCopyrightText: 2026 Hajime Hoshi

//go:generate go run gen_tables.go

package regexp

import (
	"sync"
	"unicode"
)

// binaryAliases maps the names of the binary properties that property escapes accept,
// with their aliases, to their long names.
var binaryAliases = map[string]string{
	"ASCII":                        "ASCII",
	"ASCII_Hex_Digit":              "ASCII_Hex_Digit",
	"AHex":                         "ASCII_Hex_Digit",
	"Alphabetic":                   "Alphabetic",
	"Alpha":                        "Alphabetic",
	"Any":                          "Any",
	"Assigned":                     "Assigned",
	"Bidi_Control":                 "Bidi_Control",
	"Bidi_C":                       "Bidi_Control",
	"Bidi_Mirrored":                "Bidi_Mirrored",
	"Bidi_M":                       "Bidi_Mirrored",
	"Case_Ignorable":               "Case_Ignorable",
	"CI":                           "Case_Ignorable",
	"Cased":                        "Cased",
	"Changes_When_Casefolded":      "Changes_When_Casefolded",
	"CWCF":                         "Changes_When_Casefolded",
	"Changes_When_Casemapped":      "Changes_When_Casemapped",
	"CWCM":                         "Changes_When_Casemapped",
	"Changes_When_Lowercased":      "Changes_When_Lowercased",
	"CWL":                          "Changes_When_Lowercased",
	"Changes_When_NFKC_Casefolded": "Changes_When_NFKC_Casefolded",
	"CWKCF":                        "Changes_When_NFKC_Casefolded",
	"Changes_When_Titlecased":      "Changes_When_Titlecased",
	"CWT":                          "Changes_When_Titlecased",
	"Changes_When_Uppercased":      "Changes_When_Uppercased",
	"CWU":                          "Changes_When_Uppercased",
	"Dash":                         "Dash",
	"Default_Ignorable_Code_Point": "Default_Ignorable_Code_Point",
	"DI":                           "Default_Ignorable_Code_Point",
	"Deprecated":                   "Deprecated",
	"Dep":                          "Deprecated",
	"Diacritic":                    "Diacritic",
	"Dia":                          "Diacritic",
	"Emoji":                        "Emoji",
	"Emoji_Component":              "Emoji_Component",
	"EComp":                        "Emoji_Component",
	"Emoji_Modifier":               "Emoji_Modifier",
	"EMod":                         "Emoji_Modifier",
	"Emoji_Modifier_Base":          "Emoji_Modifier_Base",
	"EBase":                        "Emoji_Modifier_Base",
	"Emoji_Presentation":           "Emoji_Presentation",
	"EPres":                        "Emoji_Presentation",
	"Extended_Pictographic":        "Extended_Pictographic",
	"ExtPict":                      "Extended_Pictographic",
	"Extender":                     "Extender",
	"Ext":                          "Extender",
	"Grapheme_Base":                "Grapheme_Base",
	"Gr_Base":                      "Grapheme_Base",
	"Grapheme_Extend":              "Grapheme_Extend",
	"Gr_Ext":                       "Grapheme_Extend",
	"Hex_Digit":                    "Hex_Digit",
	"Hex":                          "Hex_Digit",
	"IDS_Binary_Operator":          "IDS_Binary_Operator",
	"IDSB":                         "IDS_Binary_Operator",
	"IDS_Trinary_Operator":         "IDS_Trinary_Operator",
	"IDST":                         "IDS_Trinary_Operator",
	"ID_Continue":                  "ID_Continue",
	"IDC":                          "ID_Continue",
	"ID_Start":                     "ID_Start",
	"IDS":                          "ID_Start",
	"Ideographic":                  "Ideographic",
	"Ideo":                         "Ideographic",
	"Join_Control":                 "Join_Control",
	"Join_C":                       "Join_Control",
	"Logical_Order_Exception":      "Logical_Order_Exception",
	"LOE":                          "Logical_Order_Exception",
	"Lowercase":                    "Lowercase",
	"Lower":                        "Lowercase",
	"Math":                         "Math",
	"Noncharacter_Code_Point":      "Noncharacter_Code_Point",
	"NChar":                        "Noncharacter_Code_Point",
	"Pattern_Syntax":               "Pattern_Syntax",
	"Pat_Syn":                      "Pattern_Syntax",
	"Pattern_White_Space":          "Pattern_White_Space",
	"Pat_WS":                       "Pattern_White_Space",
	"Quotation_Mark":               "Quotation_Mark",
	"QMark":                        "Quotation_Mark",
	"Radical":                      "Radical",
	"Regional_Indicator":           "Regional_Indicator",
	"RI":                           "Regional_Indicator",
	"Sentence_Terminal":            "Sentence_Terminal",
	"STerm":                        "Sentence_Terminal",
	"Soft_Dotted":                  "Soft_Dotted",
	"SD":                           "Soft_Dotted",
	"Terminal_Punctuation":         "Terminal_Punctuation",
	"Term":                         "Terminal_Punctuation",
	"Unified_Ideograph":            "Unified_Ideograph",
	"UIdeo":                        "Unified_Ideograph",
	"Uppercase":                    "Uppercase",
	"Upper":                        "Uppercase",
	"Variation_Selector":           "Variation_Selector",
	"VS":                           "Variation_Selector",
	"White_Space":                  "White_Space",
	"space":                        "White_Space",
	"XID_Continue":                 "XID_Continue",
	"XIDC":                         "XID_Continue",
	"XID_Start":                    "XID_Start",
	"XIDS":                         "XID_Start",
}

// stringPropertyNames are the properties of strings that property escapes accept with the v flag.
var stringPropertyNames = map[string]bool{
	"Basic_Emoji":                 true,
	"Emoji_Keycap_Sequence":       true,
	"RGI_Emoji_Modifier_Sequence": true,
	"RGI_Emoji_Flag_Sequence":     true,
	"RGI_Emoji_Tag_Sequence":      true,
	"RGI_Emoji_ZWJ_Sequence":      true,
	"RGI_Emoji":                   true,
}

var (
	propCache   = map[string]charSet{}
	propCacheMu sync.Mutex
)

// lookupProperty returns the characters of \p{name=value}, or of \p{name} when value is empty.
func lookupProperty(name, value string) (charSet, bool) {
	key := name + "=" + value
	propCacheMu.Lock()
	defer propCacheMu.Unlock()
	if s, ok := propCache[key]; ok {
		return s, true
	}
	s, ok := computeProperty(name, value)
	if ok {
		propCache[key] = s
	}
	return s, ok
}

func computeProperty(name, value string) (charSet, bool) {
	if value != "" {
		switch name {
		case "General_Category", "gc":
			return generalCategory(value)
		case "Script", "sc":
			return script(value, false)
		case "Script_Extensions", "scx":
			return script(value, true)
		}
		return nil, false
	}
	if s, ok := generalCategory(name); ok {
		return s, true
	}
	long, ok := binaryAliases[name]
	if !ok {
		return nil, false
	}
	switch long {
	case "ASCII":
		return charSet{0, 0x7F}, true
	case "Any":
		return charSet{0, maxRune}, true
	case "Assigned":
		return fromTable(unicode.Cn).complement(maxRune), true
	}
	if t, ok := unicode.Properties[long]; ok {
		return fromTable(t), true
	}
	if s, ok := derivedProperties[long]; ok {
		return s, true
	}
	return nil, false
}

func generalCategory(value string) (charSet, bool) {
	short, ok := gcAliases[value]
	if !ok {
		return nil, false
	}
	if short == "C" {
		var s charSet
		for _, c := range []string{"Cc", "Cf", "Cn", "Co", "Cs"} {
			s = s.union(fromTable(unicode.Categories[c]))
		}
		return s, true
	}
	t, ok := unicode.Categories[short]
	if !ok {
		return nil, false
	}
	return fromTable(t), true
}

func script(value string, extensions bool) (charSet, bool) {
	long, ok := scriptAliases[value]
	if !ok {
		return nil, false
	}
	var s charSet
	switch long {
	case "Unknown":
		var all charSet
		for _, t := range unicode.Scripts {
			all = append(all, fromTable(t)...)
		}
		s = all.normalize().complement(maxRune)
	case "Katakana_Or_Hiragana":
	default:
		t, ok := unicode.Scripts[long]
		if !ok {
			return nil, false
		}
		s = fromTable(t)
	}
	if extensions {
		s = s.subtract(scriptExtensionsListed).union(scriptExtensions[long])
	}
	return s, true
}

// lookupStringProperty returns the strings of the property of strings name.
func lookupStringProperty(name string) ([][]rune, bool) {
	if !stringPropertyNames[name] {
		return nil, false
	}
	var list []string
	if name == "RGI_Emoji" {
		for n := range stringPropertyNames {
			if n != "RGI_Emoji" {
				list = append(list, stringProperties[n]...)
			}
		}
	} else {
		list = stringProperties[name]
	}
	out := make([][]rune, 0, len(list))
	for _, s := range list {
		out = append(out, []rune(s))
	}
	return out, true
}

var idStart, idContinue = derivedProperties["ID_Start"], derivedProperties["ID_Continue"]

// isStringProperty reports whether name is a property of strings.
func isStringProperty(name string) bool {
	return stringPropertyNames[name]
}
