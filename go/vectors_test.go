package filobook

import (
	"bytes"
	"encoding/json"
	"errors"
	"os"
	"path/filepath"
	"reflect"
	"testing"
	"time"
)

const vectorsDir = "../spec/vectors"

func loadVectors(t *testing.T, name string, out any) {
	t.Helper()
	data, err := os.ReadFile(filepath.Join(vectorsDir, name))
	if err != nil {
		t.Fatalf("read %s: %v", name, err)
	}
	dec := json.NewDecoder(bytes.NewReader(data))
	dec.UseNumber()
	if err := dec.Decode(out); err != nil {
		t.Fatalf("decode %s: %v", name, err)
	}
}

func TestVectorsPrices(t *testing.T) {
	var v struct {
		Parse []struct {
			Input string `json:"input"`
			Micro *int64 `json:"micro"`
		} `json:"parse"`
		Format []struct {
			Micro int64  `json:"micro"`
			Text  string `json:"text"`
		} `json:"format"`
	}
	loadVectors(t, "prices.json", &v)
	if len(v.Parse) == 0 || len(v.Format) == 0 {
		t.Fatal("no price vectors")
	}
	for _, c := range v.Parse {
		got, ok := ParsePrice(c.Input)
		if c.Micro == nil {
			if ok {
				t.Errorf("ParsePrice(%q) = %d, want rejected", c.Input, got)
			}
		} else if !ok || got != *c.Micro {
			t.Errorf("ParsePrice(%q) = %d, %v; want %d", c.Input, got, ok, *c.Micro)
		}
	}
	for _, c := range v.Format {
		if got := FormatMicro(c.Micro); got != c.Text {
			t.Errorf("FormatMicro(%d) = %q, want %q", c.Micro, got, c.Text)
		}
	}
}

func TestVectorsCanonicalJSON(t *testing.T) {
	var v struct {
		Cases []struct {
			Name   string `json:"name"`
			Input  any    `json:"input"`
			Output string `json:"output"`
		} `json:"cases"`
	}
	loadVectors(t, "canonical_json.json", &v)
	if len(v.Cases) == 0 {
		t.Fatal("no canonical JSON vectors")
	}
	for _, c := range v.Cases {
		t.Run(c.Name, func(t *testing.T) {
			got, err := CanonicalJSON(c.Input)
			if err != nil {
				t.Fatal(err)
			}
			if got != c.Output {
				t.Errorf("got  %s\nwant %s", got, c.Output)
			}
		})
	}
}

type ledgerVectors struct {
	Genesis  string `json:"genesis"`
	Payloads []struct {
		Fill        map[string]any `json:"fill"`
		Canonical   string         `json:"canonical"`
		PayloadHash string         `json:"payload_hash"`
	} `json:"payloads"`
	Cases []struct {
		Name    string        `json:"name"`
		Entries []LedgerEntry `json:"entries"`
		Fills   []Fill        `json:"fills"`
		Expect  ChainResult   `json:"expect"`
	} `json:"cases"`
}

func toFill(t *testing.T, m map[string]any) Fill {
	t.Helper()
	b, err := json.Marshal(m)
	if err != nil {
		t.Fatal(err)
	}
	var f Fill
	if err := json.Unmarshal(b, &f); err != nil {
		t.Fatal(err)
	}
	return f
}

func fillsByID(fills []Fill) map[string]*Fill {
	m := make(map[string]*Fill, len(fills))
	for i := range fills {
		m[fills[i].ID] = &fills[i]
	}
	return m
}

func TestVectorsLedger(t *testing.T) {
	var v ledgerVectors
	loadVectors(t, "ledger.json", &v)
	if v.Genesis != GenesisHash {
		t.Errorf("genesis = %s, want %s", GenesisHash, v.Genesis)
	}
	if len(v.Payloads) == 0 || len(v.Cases) == 0 {
		t.Fatal("no ledger vectors")
	}
	for _, p := range v.Payloads {
		got, err := CanonicalJSON(p.Fill)
		if err != nil {
			t.Fatal(err)
		}
		if got != p.Canonical {
			t.Errorf("canonical(fill %v)\n got  %s\n want %s", p.Fill["id"], got, p.Canonical)
		}
		f := toFill(t, p.Fill)
		if got, _ := CanonicalJSON(FillPayload(&f)); got != p.Canonical {
			t.Errorf("canonical(FillPayload %s) = %s", f.ID, got)
		}
		h, err := FillPayloadHash(&f)
		if err != nil {
			t.Fatal(err)
		}
		if h != p.PayloadHash {
			t.Errorf("FillPayloadHash(%s) = %s, want %s", f.ID, h, p.PayloadHash)
		}
	}
	for _, c := range v.Cases {
		t.Run(c.Name, func(t *testing.T) {
			got := VerifyEntries(c.Entries, fillsByID(c.Fills), Genesis)
			if got != c.Expect {
				t.Errorf("got  %+v\nwant %+v", got, c.Expect)
			}
		})
	}
}

func TestVectorsWebhook(t *testing.T) {
	var v struct {
		Secret string `json:"secret"`
		Sign   []struct {
			Timestamp string `json:"timestamp"`
			Body      string `json:"body"`
			Signature string `json:"signature"`
		} `json:"sign"`
		Verify []struct {
			Name       string `json:"name"`
			Timestamp  string `json:"timestamp"`
			Body       string `json:"body"`
			Signature  string `json:"signature"`
			Now        int64  `json:"now"`
			ToleranceS *int64 `json:"tolerance_s"`
			OK         bool   `json:"ok"`
			Error      string `json:"error"`
		} `json:"verify"`
	}
	loadVectors(t, "webhook.json", &v)
	if len(v.Sign) == 0 || len(v.Verify) == 0 {
		t.Fatal("no webhook vectors")
	}
	for _, c := range v.Sign {
		if got := SignWebhook(v.Secret, c.Timestamp, []byte(c.Body)); got != c.Signature {
			t.Errorf("SignWebhook(%s) = %s, want %s", c.Timestamp, got, c.Signature)
		}
	}
	for _, c := range v.Verify {
		t.Run(c.Name, func(t *testing.T) {
			opts := []WebhookOption{WithNow(time.Unix(c.Now, 0))}
			if c.ToleranceS != nil {
				opts = append(opts, WithTolerance(time.Duration(*c.ToleranceS)*time.Second))
			}
			ev, err := VerifyWebhook(v.Secret, []byte(c.Body), c.Timestamp, c.Signature, opts...)
			if c.OK {
				if err != nil {
					t.Fatalf("unexpected error: %v", err)
				}
				var want FeedEvent
				if err := json.Unmarshal([]byte(c.Body), &want); err != nil {
					t.Fatal(err)
				}
				if !reflect.DeepEqual(*ev, want) {
					t.Errorf("event = %+v, want %+v", *ev, want)
				}
				return
			}
			var we *WebhookError
			if !errors.As(err, &we) {
				t.Fatalf("error = %v, want *WebhookError %s", err, c.Error)
			}
			if string(we.Code) != c.Error {
				t.Errorf("code = %s, want %s", we.Code, c.Error)
			}
		})
	}
}
