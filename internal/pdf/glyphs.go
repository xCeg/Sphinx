package pdf

import (
	"strconv"
	"strings"
)

// latin1Names are the glyph names of U+00A0..U+00FF in order.
var latin1Names = strings.Fields(`space exclamdown cent sterling currency yen brokenbar section
dieresis copyright ordfeminine guillemotleft logicalnot hyphen registered macron degree
plusminus twosuperior threesuperior acute mu paragraph periodcentered cedilla onesuperior
ordmasculine guillemotright onequarter onehalf threequarters questiondown Agrave Aacute
Acircumflex Atilde Adieresis Aring AE Ccedilla Egrave Eacute Ecircumflex Edieresis Igrave
Iacute Icircumflex Idieresis Eth Ntilde Ograve Oacute Ocircumflex Otilde Odieresis multiply
Oslash Ugrave Uacute Ucircumflex Udieresis Yacute Thorn germandbls agrave aacute acircumflex
atilde adieresis aring ae ccedilla egrave eacute ecircumflex edieresis igrave iacute
icircumflex idieresis eth ntilde ograve oacute ocircumflex otilde odieresis divide oslash
ugrave uacute ucircumflex udieresis yacute thorn ydieresis`)

// asciiNames are the glyph names of U+0020..U+007E in order.
var asciiNames = strings.Fields(`space exclam quotedbl numbersign dollar percent ampersand
quotesingle parenleft parenright asterisk plus comma hyphen period slash zero one two three
four five six seven eight nine colon semicolon less equal greater question at A B C D E F G H
I J K L M N O P Q R S T U V W X Y Z bracketleft backslash bracketright asciicircum underscore
grave a b c d e f g h i j k l m n o p q r s t u v w x y z braceleft bar braceright asciitilde`)

var glyphNames = map[string]string{
	"quoteleft": "‘", "quoteright": "’", "quotedblleft": "“", "quotedblright": "”",
	"quotesinglbase": "‚", "quotedblbase": "„", "endash": "–", "emdash": "—", "bullet": "•",
	"ellipsis": "…", "dagger": "†", "daggerdbl": "‡", "perthousand": "‰", "florin": "ƒ",
	"circumflex": "ˆ", "tilde": "˜", "dotlessi": "ı", "minus": "−", "nbspace": " ",
	"fi": "fi", "fl": "fl", "ff": "ff", "ffi": "ffi", "ffl": "ffl", "OE": "Œ", "oe": "œ",
	"Scaron": "Š", "scaron": "š", "Zcaron": "Ž", "zcaron": "ž", "Ydieresis": "Ÿ", "Euro": "€",
	"trademark": "™", "guilsinglleft": "‹", "guilsinglright": "›", "Lslash": "Ł", "lslash": "ł",
	"fraction": "⁄", "breve": "˘", "dotaccent": "˙", "ring": "˚", "ogonek": "˛", "caron": "ˇ",
	"hungarumlaut": "˝", "space.alt": " ", "uni00A0": " ",
}

func init() {
	for i, n := range asciiNames {
		glyphNames[n] = string(rune(0x20 + i))
	}
	for i, n := range latin1Names {
		if _, ok := glyphNames[n]; !ok {
			glyphNames[n] = string(rune(0xA0 + i))
		}
	}
	glyphNames["space"] = " "
	glyphNames["hyphen"] = "-"
}

// glyphText maps a glyph name to text, following the Adobe Glyph List
// conventions for uniXXXX, uXXXXX and suffixed names such as "a.sc".
func glyphText(name string) string {
	if t, ok := glyphNames[name]; ok {
		return t
	}
	if i := strings.IndexByte(name, '.'); i > 0 {
		return glyphText(name[:i])
	}
	if i := strings.IndexByte(name, '_'); i > 0 {
		var sb strings.Builder
		for _, part := range strings.Split(name, "_") {
			sb.WriteString(glyphText(part))
		}
		return sb.String()
	}
	if strings.HasPrefix(name, "uni") && len(name) >= 7 && (len(name)-3)%4 == 0 {
		var sb strings.Builder
		for i := 3; i+4 <= len(name); i += 4 {
			v, err := strconv.ParseUint(name[i:i+4], 16, 32)
			if err != nil {
				return ""
			}
			sb.WriteRune(rune(v))
		}
		return sb.String()
	}
	if strings.HasPrefix(name, "u") && len(name) >= 5 && len(name) <= 7 {
		if v, err := strconv.ParseUint(name[1:], 16, 32); err == nil {
			return string(rune(v))
		}
	}
	return ""
}

// winAnsiHigh maps the 0x80..0x9F block where Windows-1252 differs from Latin-1.
var winAnsiHigh = map[byte]rune{
	0x80: '€', 0x82: '‚', 0x83: 'ƒ', 0x84: '„', 0x85: '…', 0x86: '†', 0x87: '‡', 0x88: 'ˆ',
	0x89: '‰', 0x8A: 'Š', 0x8B: '‹', 0x8C: 'Œ', 0x8E: 'Ž', 0x91: '‘', 0x92: '’', 0x93: '“',
	0x94: '”', 0x95: '•', 0x96: '–', 0x97: '—', 0x98: '˜', 0x99: '™', 0x9A: 'š', 0x9B: '›',
	0x9C: 'œ', 0x9E: 'ž', 0x9F: 'Ÿ',
}

// baseEncoding returns a code -> text table. Standard and MacRoman
// encodings differ from WinAnsi mostly outside ASCII; WinAnsi is used as
// the approximation there, which only affects accented letters.
func baseEncoding(name Name) [256]string {
	var t [256]string
	for c := 0; c < 256; c++ {
		switch {
		case c < 0x20:
		case c < 0x80 || c >= 0xA0:
			t[c] = string(rune(c))
		default:
			if r, ok := winAnsiHigh[byte(c)]; ok {
				t[c] = string(r)
			}
		}
	}
	if name == "StandardEncoding" {
		t[0x27], t[0x60] = "’", "‘"
	}
	return t
}
