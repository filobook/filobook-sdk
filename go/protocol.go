package filobook

// The parts of the protocol every SDK must compute byte-for-byte the same: price math, canonical JSON,
// the ledger hashes and the webhook signature. See spec/PROTOCOL.md; spec/vectors holds the test cases.

import (
	"crypto/hmac"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"fmt"
	"reflect"
	"regexp"
	"sort"
	"strconv"
	"strings"
	"unicode/utf16"
	"unicode/utf8"
)

const (
	// Unit is the number of micro-USDG in 1 USDG.
	Unit int64 = 1_000_000
	// MaxPriceMicro is the largest price the API accepts, in micro-USDG (1,000,000 USDG).
	MaxPriceMicro int64 = 1_000_000 * Unit
	// GenesisHash is the prev_hash of the first ledger entry.
	GenesisHash = "0000000000000000000000000000000000000000000000000000000000000000"
	// maxSafeInteger is JavaScript's Number.MAX_SAFE_INTEGER; the reference rejects anything larger.
	maxSafeInteger int64 = 1<<53 - 1
)

var priceRe = regexp.MustCompile(`^([0-9]{1,7})(?:\.([0-9]{1,6}))?$`)

// isJSWhitespace matches the characters JavaScript's String.prototype.trim removes: WhiteSpace
// (TAB, VT, FF, SP, NBSP, ZWNBSP and the Zs category) and LineTerminator (LF, CR, LS, PS).
func isJSWhitespace(r rune) bool {
	switch r {
	case '\t', '\n', '\v', '\f', '\r', ' ', 0x00A0, 0x1680, 0x2028, 0x2029, 0x202F, 0x205F, 0x3000, 0xFEFF:
		return true
	}
	return r >= 0x2000 && r <= 0x200A
}

// ParsePrice turns a decimal USDG string into micro-USDG: "0.0042" → 4200, true. It trims surrounding
// whitespace and reports false unless the rest is a positive decimal with at most 7 integer digits and
// 6 decimals, no larger than 1,000,000 USDG.
func ParsePrice(text string) (int64, bool) {
	m := priceRe.FindStringSubmatch(strings.TrimFunc(text, isJSWhitespace))
	if m == nil {
		return 0, false
	}
	whole, err := strconv.ParseInt(m[1], 10, 64)
	if err != nil {
		return 0, false
	}
	frac := int64(0)
	if m[2] != "" {
		frac, err = strconv.ParseInt(m[2]+strings.Repeat("0", 6-len(m[2])), 10, 64)
		if err != nil {
			return 0, false
		}
	}
	micro := whole*Unit + frac
	if micro <= 0 || micro > MaxPriceMicro {
		return 0, false
	}
	return micro, true
}

// FormatMicro turns micro-USDG into the shortest exact decimal string: 4200 → "0.0042", -1000000 → "-1".
func FormatMicro(micro int64) string {
	sign := ""
	abs := uint64(micro)
	if micro < 0 {
		sign = "-"
		abs = uint64(-(micro + 1)) + 1 // safe for math.MinInt64
	}
	whole := abs / uint64(Unit)
	frac := strings.TrimRight(fmt.Sprintf("%06d", abs%uint64(Unit)), "0")
	if frac == "" {
		return sign + strconv.FormatUint(whole, 10)
	}
	return sign + strconv.FormatUint(whole, 10) + "." + frac
}

var (
	jsonNumberType = reflect.TypeOf(json.Number(""))
	integerRe      = regexp.MustCompile(`^-?(?:0|[1-9][0-9]*)$`)
)

// CanonicalJSON encodes v with object keys sorted by UTF-16 code units at every level and no whitespace.
// Strings are escaped exactly like JavaScript's JSON.stringify (encoding/json is not used for them).
//
// Accepted values: nil, bool, string (and named string types), json.Number holding a plain integer, Go
// integer types, maps with string keys, slices and arrays, and pointers or interfaces holding these. A nil
// map, slice or pointer encodes as null. Floats, structs and integers beyond ±(2^53−1) are rejected, as in
// the reference implementation; decode input with json.Decoder.UseNumber so large integers stay exact.
// Strings must be valid UTF-8.
func CanonicalJSON(v any) (string, error) {
	var b strings.Builder
	if err := writeCanonical(&b, reflect.ValueOf(v)); err != nil {
		return "", err
	}
	return b.String(), nil
}

func writeCanonical(b *strings.Builder, v reflect.Value) error {
	if !v.IsValid() {
		b.WriteString("null")
		return nil
	}
	if v.Type() == jsonNumberType {
		s := v.String()
		if !integerRe.MatchString(s) {
			return fmt.Errorf("filobook: canonical JSON: number %q is not a plain integer", s)
		}
		n, err := strconv.ParseInt(s, 10, 64)
		if err != nil || n > maxSafeInteger || n < -maxSafeInteger {
			return fmt.Errorf("filobook: canonical JSON: number %s is not a safe integer", s)
		}
		b.WriteString(strconv.FormatInt(n, 10)) // normalises "-0" to "0", as JavaScript does
		return nil
	}
	switch v.Kind() {
	case reflect.Interface, reflect.Pointer:
		if v.IsNil() {
			b.WriteString("null")
			return nil
		}
		return writeCanonical(b, v.Elem())
	case reflect.Bool:
		if v.Bool() {
			b.WriteString("true")
		} else {
			b.WriteString("false")
		}
		return nil
	case reflect.String:
		return writeJSString(b, v.String())
	case reflect.Int, reflect.Int8, reflect.Int16, reflect.Int32, reflect.Int64:
		n := v.Int()
		if n > maxSafeInteger || n < -maxSafeInteger {
			return fmt.Errorf("filobook: canonical JSON: number %d is not a safe integer", n)
		}
		b.WriteString(strconv.FormatInt(n, 10))
		return nil
	case reflect.Uint, reflect.Uint8, reflect.Uint16, reflect.Uint32, reflect.Uint64, reflect.Uintptr:
		n := v.Uint()
		if n > uint64(maxSafeInteger) {
			return fmt.Errorf("filobook: canonical JSON: number %d is not a safe integer", n)
		}
		b.WriteString(strconv.FormatUint(n, 10))
		return nil
	case reflect.Float32, reflect.Float64:
		return fmt.Errorf("filobook: canonical JSON: floats are not allowed (got %v)", v.Float())
	case reflect.Slice, reflect.Array:
		if v.Kind() == reflect.Slice && v.IsNil() {
			b.WriteString("null")
			return nil
		}
		b.WriteByte('[')
		for i := 0; i < v.Len(); i++ {
			if i > 0 {
				b.WriteByte(',')
			}
			if err := writeCanonical(b, v.Index(i)); err != nil {
				return err
			}
		}
		b.WriteByte(']')
		return nil
	case reflect.Map:
		if v.Type().Key().Kind() != reflect.String {
			return fmt.Errorf("filobook: canonical JSON: map keys must be strings (got %s)", v.Type().Key())
		}
		if v.IsNil() {
			b.WriteString("null")
			return nil
		}
		keys := make([]string, 0, v.Len())
		vals := make(map[string]reflect.Value, v.Len())
		iter := v.MapRange()
		for iter.Next() {
			k := iter.Key().String()
			keys = append(keys, k)
			vals[k] = iter.Value()
		}
		sort.Slice(keys, func(i, j int) bool { return compareUTF16(keys[i], keys[j]) < 0 })
		b.WriteByte('{')
		for i, k := range keys {
			if i > 0 {
				b.WriteByte(',')
			}
			if err := writeJSString(b, k); err != nil {
				return err
			}
			b.WriteByte(':')
			if err := writeCanonical(b, vals[k]); err != nil {
				return err
			}
		}
		b.WriteByte('}')
		return nil
	}
	return fmt.Errorf("filobook: canonical JSON: unsupported type %s", v.Type())
}

// compareUTF16 orders strings the way JavaScript's default sort does: by UTF-16 code units.
func compareUTF16(a, b string) int {
	ua, ub := utf16.Encode([]rune(a)), utf16.Encode([]rune(b))
	for i := 0; i < len(ua) && i < len(ub); i++ {
		if ua[i] != ub[i] {
			if ua[i] < ub[i] {
				return -1
			}
			return 1
		}
	}
	switch {
	case len(ua) < len(ub):
		return -1
	case len(ua) > len(ub):
		return 1
	}
	return 0
}

const hexDigits = "0123456789abcdef"

// writeJSString writes s quoted and escaped exactly like JavaScript's JSON.stringify: \" \\ \b \f \n \r \t,
// other characters below U+0020 as \u00xx (lowercase hex), everything else as is.
func writeJSString(b *strings.Builder, s string) error {
	if !utf8.ValidString(s) {
		return fmt.Errorf("filobook: canonical JSON: string is not valid UTF-8: %q", s)
	}
	b.WriteByte('"')
	for i := 0; i < len(s); i++ {
		c := s[i]
		switch c {
		case '"':
			b.WriteString(`\"`)
		case '\\':
			b.WriteString(`\\`)
		case '\b':
			b.WriteString(`\b`)
		case '\f':
			b.WriteString(`\f`)
		case '\n':
			b.WriteString(`\n`)
		case '\r':
			b.WriteString(`\r`)
		case '\t':
			b.WriteString(`\t`)
		default:
			if c < 0x20 {
				b.WriteString(`\u00`)
				b.WriteByte(hexDigits[c>>4])
				b.WriteByte(hexDigits[c&0xF])
			} else {
				b.WriteByte(c) // multi-byte UTF-8 sequences are copied byte by byte, unchanged
			}
		}
	}
	b.WriteByte('"')
	return nil
}

// SHA256Hex returns the lowercase hex SHA-256 of data.
func SHA256Hex(data []byte) string {
	sum := sha256.Sum256(data)
	return hex.EncodeToString(sum[:])
}

// HMACSHA256Hex returns the lowercase hex HMAC-SHA256 of data, keyed with secret as UTF-8.
func HMACSHA256Hex(secret string, data []byte) string {
	mac := hmac.New(sha256.New, []byte(secret))
	mac.Write(data)
	return hex.EncodeToString(mac.Sum(nil))
}

// FillPayload returns the fields of a fill the ledger commits to: every field except ledger_height and
// ledger_hash.
func FillPayload(f *Fill) map[string]any {
	return map[string]any{
		"id":            f.ID,
		"order_id":      f.OrderID,
		"agent":         f.Agent,
		"provider_id":   f.ProviderID,
		"provider_name": f.ProviderName,
		"max_price":     f.MaxPrice,
		"price":         f.Price,
		"saved":         f.Saved,
		"created_at":    f.CreatedAt,
	}
}

// FillPayloadHash returns sha256(canonical_json(fill payload)) as lowercase hex. It fails only when a field
// is not valid UTF-8.
func FillPayloadHash(f *Fill) (string, error) {
	c, err := CanonicalJSON(FillPayload(f))
	if err != nil {
		return "", err
	}
	return SHA256Hex([]byte(c)), nil
}

// ChainHash returns sha256(prevHash + payloadHash): the two lowercase hex strings concatenated, then hashed.
func ChainHash(prevHash, payloadHash string) string {
	return SHA256Hex([]byte(prevHash + payloadHash))
}
