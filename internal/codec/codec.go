// Package codec implements the JCS subset used by the protocol: numbers are
// forbidden (all application integers are decimal strings).
package codec

import (
	"bytes"
	"crypto/sha256"
	"encoding/base32"
	"encoding/base64"
	"encoding/hex"
	"encoding/json"
	"fmt"
	"io"
	"sort"
	"strconv"
	"strings"
	"sync"
	"unicode/utf16"
	"unicode/utf8"

	"civora/spec"
	"github.com/santhosh-tekuri/jsonschema/v6"
	"golang.org/x/text/unicode/norm"
)

func Parse(raw []byte) (any, error) {
	if !utf8.Valid(raw) {
		return nil, fmt.Errorf("invalid UTF-8")
	}
	d := json.NewDecoder(bytes.NewReader(raw))
	d.UseNumber()
	var v any
	if err := d.Decode(&v); err != nil {
		return nil, err
	}
	if _, err := d.Token(); err != io.EOF {
		return nil, fmt.Errorf("trailing JSON")
	}
	canonical, err := Canonical(v)
	if err != nil {
		return nil, err
	}
	// Duplicate names, whitespace, nonminimal escapes and lone surrogates
	// necessarily differ from the unique canonical re-encoding.
	if !bytes.Equal(raw, canonical) {
		return nil, fmt.Errorf("noncanonical JSON")
	}
	return v, nil
}

func Canonical(value any) ([]byte, error) {
	// Structs go through JSON first; generic values retain their original types.
	switch value.(type) {
	case nil, bool, string, []any, map[string]any:
	default:
		b, err := json.Marshal(value)
		if err != nil {
			return nil, err
		}
		d := json.NewDecoder(bytes.NewReader(b))
		d.UseNumber()
		if err = d.Decode(&value); err != nil {
			return nil, err
		}
	}
	var out bytes.Buffer
	var encode func(any) error
	encode = func(v any) error {
		switch x := v.(type) {
		case nil:
			out.WriteString("null")
		case bool:
			out.WriteString(strconv.FormatBool(x))
		case string:
			if !utf8.ValidString(x) || !norm.NFC.IsNormalString(x) || strings.ContainsRune(x, '\r') {
				return fmt.Errorf("text must be Unicode NFC with LF endings")
			}
			out.WriteByte('"')
			for _, r := range x {
				switch r {
				case '"', '\\':
					out.WriteByte('\\')
					out.WriteRune(r)
				case '\b':
					out.WriteString(`\b`)
				case '\t':
					out.WriteString(`\t`)
				case '\n':
					out.WriteString(`\n`)
				case '\f':
					out.WriteString(`\f`)
				default:
					if r < 32 {
						fmt.Fprintf(&out, `\u%04x`, r)
					} else {
						out.WriteRune(r)
					}
				}
			}
			out.WriteByte('"')
		case []any:
			out.WriteByte('[')
			for i, e := range x {
				if i > 0 {
					out.WriteByte(',')
				}
				if err := encode(e); err != nil {
					return err
				}
			}
			out.WriteByte(']')
		case map[string]any:
			keys := make([]string, 0, len(x))
			for k := range x {
				keys = append(keys, k)
			}
			sort.Slice(keys, func(i, j int) bool {
				a, b := utf16.Encode([]rune(keys[i])), utf16.Encode([]rune(keys[j]))
				for k := 0; k < len(a) && k < len(b); k++ {
					if a[k] != b[k] {
						return a[k] < b[k]
					}
				}
				return len(a) < len(b)
			})
			out.WriteByte('{')
			for i, k := range keys {
				if i > 0 {
					out.WriteByte(',')
				}
				if err := encode(k); err != nil {
					return err
				}
				out.WriteByte(':')
				if err := encode(x[k]); err != nil {
					return err
				}
			}
			out.WriteByte('}')
		default:
			return fmt.Errorf("unsupported JSON type %T (integers must be strings)", v)
		}
		return nil
	}
	if err := encode(value); err != nil {
		return nil, err
	}
	return out.Bytes(), nil
}

func Must(value any) []byte {
	b, e := Canonical(value)
	if e != nil {
		panic(e)
	}
	return b
}
func Hash(tag string, value any) string {
	h := sha256.New()
	h.Write([]byte("CIVIC/1/" + tag + "\n"))
	h.Write(Must(value))
	return hex.EncodeToString(h.Sum(nil))
}
func ChainID(network string) (string, error) {
	b, e := hex.DecodeString(network)
	if e != nil || len(b) != 32 || network != strings.ToLower(network) {
		return "", fmt.Errorf("invalid network")
	}
	return "civ1-" + strings.ToLower(base32.StdEncoding.WithPadding(base32.NoPadding).EncodeToString(b[:20])), nil
}
func Uint(s string) (uint64, error) {
	v, e := strconv.ParseUint(s, 10, 64)
	if e != nil || strconv.FormatUint(v, 10) != s {
		return 0, fmt.Errorf("invalid uint64 %q", s)
	}
	return v, nil
}
func Blob(s string) ([]byte, error) {
	b, e := base64.RawURLEncoding.Strict().DecodeString(s)
	if e != nil || base64.RawURLEncoding.EncodeToString(b) != s {
		return nil, fmt.Errorf("invalid base64url")
	}
	return b, nil
}

var schemas sync.Map
var compileMu sync.Mutex

func Validate(name string, value any) error {
	s, ok := schemas.Load(name)
	if !ok {
		compileMu.Lock()
		defer compileMu.Unlock()
		c := jsonschema.NewCompiler()
		files, e := spec.Schemas.ReadDir("schemas")
		if e != nil {
			return e
		}
		for _, f := range files {
			b, e := spec.Schemas.ReadFile("schemas/" + f.Name())
			if e != nil {
				return e
			}
			var doc any
			if e = json.Unmarshal(b, &doc); e != nil {
				return e
			}
			if e = c.AddResource("https://civora.invalid/v0.1/"+strings.TrimSuffix(f.Name(), ".schema.json"), doc); e != nil {
				return e
			}
		}
		compiled, e := c.Compile("https://civora.invalid/v0.1/" + name)
		if e != nil {
			return e
		}
		s = compiled
		schemas.Store(name, s)
	}
	return s.(*jsonschema.Schema).Validate(value)
}

func Decode(raw []byte, name string, target any) error {
	v, e := Parse(raw)
	if e != nil {
		return e
	}
	if e = Validate(name, v); e != nil {
		return e
	}
	return json.Unmarshal(raw, target)
}
