package filobook

import (
	"encoding/json"
	"errors"
	"io"
	"math"
	"net/http/httptest"
	"strconv"
	"strings"
	"testing"
	"time"
)

func TestParsePriceTrimsLikeJavaScript(t *testing.T) {
	for _, s := range []string{"\uFEFF1.5", "\u00A01.5\u2028", "\t1.5\n", "\u30001.5"} {
		if got, ok := ParsePrice(s); !ok || got != 1_500_000 {
			t.Errorf("ParsePrice(%q) = %d, %v", s, got, ok)
		}
	}
	// U+0085 is whitespace to Go's unicode.IsSpace but not to JavaScript's trim.
	if _, ok := ParsePrice("\u00851.5"); ok {
		t.Error("U+0085 should not be trimmed")
	}
	// Non-ASCII digits are not \d in JavaScript.
	if _, ok := ParsePrice("\u0661"); ok {
		t.Error("Arabic-Indic digit accepted")
	}
}

func TestFormatMicroExtremes(t *testing.T) {
	if got := FormatMicro(math.MinInt64); got != "-9223372036854.775808" {
		t.Errorf("FormatMicro(MinInt64) = %s", got)
	}
	if got := FormatMicro(math.MaxInt64); got != "9223372036854.775807" {
		t.Errorf("FormatMicro(MaxInt64) = %s", got)
	}
}

func TestCanonicalJSONTypes(t *testing.T) {
	type kind string
	got, err := CanonicalJSON(map[string]any{
		"i":   int(-1),
		"u8":  uint8(255),
		"n":   json.Number("-0"),
		"k":   kind("v"),
		"p":   (*string)(nil),
		"arr": [2]int64{1, 2},
		"s":   []string{"<&>", "\u2028\u2029", "\u007f", "/"},
		"nil": []int(nil),
	})
	if err != nil {
		t.Fatal(err)
	}
	want := "{\"arr\":[1,2],\"i\":-1,\"k\":\"v\",\"n\":0,\"nil\":null,\"p\":null,\"s\":[\"<&>\",\"\u2028\u2029\",\"\u007f\",\"/\"],\"u8\":255}"
	if got != want {
		t.Errorf("got  %s\nwant %s", got, want)
	}
}

func TestCanonicalJSONRejects(t *testing.T) {
	bad := map[string]any{
		"float64":          1.0,
		"float32":          float32(2),
		"json float":       json.Number("1.5"),
		"json exponent":    json.Number("1e3"),
		"above 2^53-1":     int64(1 << 53),
		"below -(2^53-1)":  int64(-(1 << 53)),
		"json above 2^53":  json.Number("9007199254740992"),
		"uint above 2^53":  uint64(math.MaxUint64),
		"invalid utf-8":    "\xff",
		"invalid utf8 key": map[string]int{"\xfe": 1},
		"struct":           struct{ A int }{1},
		"int keys":         map[int]string{1: "a"},
		"func":             func() {},
	}
	for name, v := range bad {
		if out, err := CanonicalJSON(v); err == nil {
			t.Errorf("%s: accepted, got %s", name, out)
		}
	}
	if out, err := CanonicalJSON(json.Number("9007199254740991")); err != nil || out != "9007199254740991" {
		t.Errorf("max safe integer: %s, %v", out, err)
	}
}

func TestCompareUTF16(t *testing.T) {
	// U+FFFF sorts after U+10000 in UTF-16 (0xFFFF > 0xD800) but before it in code points.
	if compareUTF16("\uffff", "\U00010000") <= 0 {
		t.Error("U+FFFF should sort after U+10000")
	}
	if compareUTF16("a", "aa") >= 0 || compareUTF16("b", "a") <= 0 || compareUTF16("x", "x") != 0 {
		t.Error("basic order")
	}
}

func TestVerifyEntriesFromPosition(t *testing.T) {
	var v ledgerVectors
	loadVectors(t, "ledger.json", &v)
	c := v.Cases[0]
	fills := fillsByID(c.Fills)
	first := VerifyEntries(c.Entries[:1], fills, Genesis)
	if !first.OK || first.Height != 1 {
		t.Fatalf("first = %+v", first)
	}
	rest := VerifyEntries(c.Entries[1:], fills, ChainPosition{Height: first.Height, Head: first.Head})
	if rest != c.Expect {
		t.Errorf("rest = %+v, want %+v", rest, c.Expect)
	}
	// A fill whose id differs from the entry's ref is a mismatch.
	f := *fills[c.Entries[0].Ref]
	f.ID = "fil_other"
	if r := VerifyEntries(c.Entries[:1], map[string]*Fill{c.Entries[0].Ref: &f}, Genesis); r.Reason != BreakFillMismatch {
		t.Errorf("reason = %s", r.Reason)
	}
}

func TestVerifyWebhookRequest(t *testing.T) {
	secret := "whsec_test"
	body := `{"event":"test","sent_at":"2026-01-01T00:00:00.000Z","orders":[]}`
	now := time.Unix(1767225600, 0)
	ts := strconv.FormatInt(now.Unix(), 10)

	req := httptest.NewRequest("POST", "/hook", strings.NewReader(body))
	req.Header.Set("x-filobook-timestamp", ts)
	req.Header.Set("x-filobook-signature", SignWebhook(secret, ts, []byte(body)))
	req.Header.Set("x-filobook-event", EventTest)
	ev, err := VerifyWebhookRequest(req, secret, WithNow(now))
	if err != nil {
		t.Fatal(err)
	}
	if ev.Event != EventTest || ev.Orders == nil || len(ev.Orders) != 0 {
		t.Errorf("event = %+v", ev)
	}
	again, _ := io.ReadAll(req.Body)
	if string(again) != body {
		t.Errorf("body not restored: %q", again)
	}

	// Missing headers.
	req = httptest.NewRequest("POST", "/hook", strings.NewReader(body))
	_, err = VerifyWebhookRequest(req, secret, WithNow(now))
	var we *WebhookError
	if !errors.As(err, &we) || we.Code != WebhookMalformed {
		t.Errorf("missing headers: %v", err)
	}

	// Correctly signed but not JSON.
	raw := []byte("not json")
	_, err = VerifyWebhook(secret, raw, ts, SignWebhook(secret, ts, raw), WithNow(now))
	if !errors.As(err, &we) || we.Code != WebhookMalformed {
		t.Errorf("non-JSON body: %v", err)
	}

	// Default tolerance is 300 s; 13-digit timestamps are malformed.
	_, err = VerifyWebhook(secret, []byte(body), ts, SignWebhook(secret, ts, []byte(body)), WithNow(now.Add(301*time.Second)))
	if !errors.As(err, &we) || we.Code != WebhookStaleTimestamp {
		t.Errorf("default tolerance: %v", err)
	}
	long := "1767225600000"
	_, err = VerifyWebhook(secret, []byte(body), long, SignWebhook(secret, long, []byte(body)), WithNow(now))
	if !errors.As(err, &we) || we.Code != WebhookMalformed {
		t.Errorf("13-digit timestamp: %v", err)
	}
}
