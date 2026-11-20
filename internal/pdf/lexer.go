package pdf

import (
	"bytes"
	"errors"
	"strconv"
)

var errSyntax = errors.New("pdf: syntax error")

type lexer struct {
	b   []byte
	pos int
	// depth bounds nesting, so a hostile file cannot exhaust the stack.
	depth int
}

func isSpace(c byte) bool {
	return c == ' ' || c == '\n' || c == '\r' || c == '\t' || c == '\f' || c == 0
}

func isDelim(c byte) bool {
	return bytes.IndexByte([]byte("()<>[]{}/%"), c) >= 0
}

func (l *lexer) skip() {
	for l.pos < len(l.b) {
		c := l.b[l.pos]
		if isSpace(c) {
			l.pos++
			continue
		}
		if c == '%' {
			for l.pos < len(l.b) && l.b[l.pos] != '\n' && l.b[l.pos] != '\r' {
				l.pos++
			}
			continue
		}
		return
	}
}

// token reads one token: a value, or a Keyword for delimiters and operators
// ("[", "]", "<<", ">>", "obj", "R", "Tj"...).
func (l *lexer) token() (Object, error) {
	l.skip()
	if l.pos >= len(l.b) {
		return nil, errEOF
	}
	c := l.b[l.pos]
	switch {
	case c == '/':
		return l.name(), nil
	case c == '(':
		return l.literal()
	case c == '<':
		if l.pos+1 < len(l.b) && l.b[l.pos+1] == '<' {
			l.pos += 2
			return Keyword("<<"), nil
		}
		return l.hex()
	case c == '>':
		if l.pos+1 < len(l.b) && l.b[l.pos+1] == '>' {
			l.pos += 2
			return Keyword(">>"), nil
		}
		l.pos++
		return nil, errSyntax
	case c == '[' || c == ']' || c == '{' || c == '}':
		l.pos++
		return Keyword(string(c)), nil
	case c == ')':
		l.pos++
		return nil, errSyntax
	}
	start := l.pos
	for l.pos < len(l.b) && !isSpace(l.b[l.pos]) && !isDelim(l.b[l.pos]) {
		l.pos++
	}
	w := string(l.b[start:l.pos])
	if w == "" {
		l.pos++
		return nil, errSyntax
	}
	if (w[0] >= '0' && w[0] <= '9') || w[0] == '-' || w[0] == '+' || w[0] == '.' {
		if i, err := strconv.Atoi(w); err == nil {
			return i, nil
		}
		if f, err := strconv.ParseFloat(w, 64); err == nil {
			return f, nil
		}
		return 0, nil
	}
	switch w {
	case "true":
		return true, nil
	case "false":
		return false, nil
	case "null":
		return nil, nil
	}
	return Keyword(w), nil
}

var errEOF = errors.New("pdf: unexpected end of data")

func (l *lexer) name() Name {
	l.pos++
	var out []byte
	for l.pos < len(l.b) && !isSpace(l.b[l.pos]) && !isDelim(l.b[l.pos]) {
		c := l.b[l.pos]
		if c == '#' && l.pos+2 < len(l.b) {
			if v, err := strconv.ParseUint(string(l.b[l.pos+1:l.pos+3]), 16, 8); err == nil {
				out = append(out, byte(v))
				l.pos += 3
				continue
			}
		}
		out = append(out, c)
		l.pos++
	}
	return Name(out)
}

func (l *lexer) literal() (Object, error) {
	l.pos++
	var out []byte
	depth := 1
	for l.pos < len(l.b) {
		c := l.b[l.pos]
		l.pos++
		switch c {
		case '(':
			depth++
		case ')':
			depth--
			if depth == 0 {
				return String(out), nil
			}
		case '\\':
			if l.pos >= len(l.b) {
				return String(out), nil
			}
			e := l.b[l.pos]
			l.pos++
			switch e {
			case 'n':
				c = '\n'
			case 'r':
				c = '\r'
			case 't':
				c = '\t'
			case 'b':
				c = '\b'
			case 'f':
				c = '\f'
			case '\r':
				if l.pos < len(l.b) && l.b[l.pos] == '\n' {
					l.pos++
				}
				continue
			case '\n':
				continue
			default:
				if e >= '0' && e <= '7' {
					v := int(e - '0')
					for k := 0; k < 2 && l.pos < len(l.b) && l.b[l.pos] >= '0' && l.b[l.pos] <= '7'; k++ {
						v = v*8 + int(l.b[l.pos]-'0')
						l.pos++
					}
					c = byte(v)
				} else {
					c = e
				}
			}
		}
		out = append(out, c)
	}
	return String(out), nil
}

func unhex(c byte) (byte, bool) {
	switch {
	case c >= '0' && c <= '9':
		return c - '0', true
	case c >= 'a' && c <= 'f':
		return c - 'a' + 10, true
	case c >= 'A' && c <= 'F':
		return c - 'A' + 10, true
	}
	return 0, false
}

func (l *lexer) hex() (Object, error) {
	l.pos++
	var out []byte
	var hi byte
	half := false
	for l.pos < len(l.b) {
		c := l.b[l.pos]
		l.pos++
		if c == '>' {
			break
		}
		v, ok := unhex(c)
		if !ok {
			continue
		}
		if half {
			out = append(out, hi<<4|v)
		} else {
			hi = v
		}
		half = !half
	}
	if half {
		out = append(out, hi<<4)
	}
	return String(out), nil
}

// object parses a complete value, resolving "n g R" into Ref.
func (l *lexer) object() (Object, error) {
	t, err := l.token()
	if err != nil {
		return nil, err
	}
	return l.complete(t)
}

func (l *lexer) complete(t Object) (Object, error) {
	switch v := t.(type) {
	case Keyword:
		switch v {
		case "[":
			return l.array()
		case "<<":
			return l.dict()
		}
		return v, nil
	case int:
		save := l.pos
		t2, err := l.token()
		if g, ok := t2.(int); err == nil && ok {
			t3, err := l.token()
			if k, ok := t3.(Keyword); err == nil && ok && k == "R" {
				return Ref{v, g}, nil
			}
		}
		l.pos = save
		return v, nil
	}
	return t, nil
}

const maxDepth = 64

func (l *lexer) array() (Object, error) {
	if l.depth++; l.depth > maxDepth {
		return nil, errSyntax
	}
	defer func() { l.depth-- }()
	var out Array
	for {
		t, err := l.token()
		if err != nil {
			return out, err
		}
		if k, ok := t.(Keyword); ok && k == "]" {
			return out, nil
		}
		v, err := l.complete(t)
		if err != nil {
			return out, err
		}
		out = append(out, v)
	}
}

func (l *lexer) dict() (Object, error) {
	if l.depth++; l.depth > maxDepth {
		return nil, errSyntax
	}
	defer func() { l.depth-- }()
	out := Dict{}
	for {
		t, err := l.token()
		if err != nil {
			return out, err
		}
		if k, ok := t.(Keyword); ok && k == ">>" {
			return out, nil
		}
		key, ok := t.(Name)
		if !ok {
			continue
		}
		v, err := l.object()
		if err != nil {
			return out, err
		}
		if k, ok := v.(Keyword); ok && k == ">>" {
			out[key] = nil
			return out, nil
		}
		out[key] = v
	}
}
