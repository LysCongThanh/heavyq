package history

import (
	"encoding/json"
	"reflect"
	"strings"
	"testing"
	"time"
)

// sameAsJSON fails unless decodeRecord agrees with encoding/json on line: the
// same record, or an error from both.
func sameAsJSON(t testing.TB, line string) {
	t.Helper()
	var want Record
	wantErr := json.Unmarshal([]byte(line), &want)
	got, gotErr := decodeRecord([]byte(line))
	if (wantErr != nil) != (gotErr != nil) {
		t.Fatalf("%q: json err %v, decodeRecord err %v", line, wantErr, gotErr)
	}
	if wantErr == nil && !reflect.DeepEqual(got, want) {
		t.Fatalf("%q:\n got %+v\nwant %+v", line, got, want)
	}
}

// Every field of Record takes the fast path: a field added to Record without
// a case in set would silently fall back to the slow decoder.
func TestFastDecodeCoversEveryRecordField(t *testing.T) {
	var r Record
	v := reflect.ValueOf(&r).Elem()
	for i := 0; i < v.NumField(); i++ {
		switch f := v.Field(i); f.Kind() {
		case reflect.String:
			f.SetString("s" + v.Type().Field(i).Name)
		case reflect.Int:
			f.SetInt(int64(i + 1))
		case reflect.Float64:
			f.SetFloat(float64(i) + 0.25)
		case reflect.Bool:
			f.SetBool(true)
		default: // time.Time
			f.Set(reflect.ValueOf(time.Date(2026, 11, 2, 8, 0, 0, 123456789, time.UTC)))
		}
	}
	b, err := json.Marshal(r)
	if err != nil {
		t.Fatal(err)
	}
	got, ok := fastDecode(b)
	if !ok || !reflect.DeepEqual(got, r) {
		t.Fatalf("fast path: ok=%v\n got %+v\nwant %+v", ok, got, r)
	}
}

var oddLines = []string{
	`{}`, ` { } `, `{"exit":1}`, `{"exit": 1 , "lane" : "a"}`, `{"exit":1} x`, `{"exit":1}{"exit":2}`, `{"exit":1,}`, `{,}`,
	`{"exit":"1"}`, `{"exit":1.0}`, `{"exit":1e2}`, `{"exit":01}`, `{"exit":-0}`, `{"exit":-}`, `{"exit":99999999999999999999}`,
	`{"wait_s":1.5e-3}`, `{"wait_s":.5}`, `{"wait_s":1.}`, `{"wait_s":1e999}`, `{"wait_s":"x"}`,
	`{"lane":"a\"b"}`, `{"lane":"a\\b"}`, `{"cmd":"a \u0026\u0026 b \u003e c"}`, `{"cmd":"\ud83d\ude00"}`, `{"cmd":"\ud83d"}`,
	`{"cmd":"\x"}`, `{"cmd":"\u00"}`, "{\"cmd\":\"a\nb\"}", "{\"cmd\":\"\xff\"}", `{"cmd":"é日本"}`,
	`{"Exit":3}`, `{"EXIT":3}`, `{"unknown":1}`, `{"unknown":{"a":[1,2]}}`, `{"exit":null}`, `{"cmd":null,"exit":2}`,
	`{"light":true,"forced":false,"dirty":true}`, `{"light":1}`, `{"light":tru}`, `{"exit":1,"exit":2}`,
	`{"end":"2026-11-02T08:00:00Z"}`, `{"end":"2026-11-02T08:00:00.5+07:00"}`, `{"end":"yesterday"}`, `{"end":1}`, `{"end":""}`,
	`{"v":2,"id":"01J","end":"2026-11-02T08:00:00Z","machine":"local","lane":"api","prio":5,"wait_s":0,"run_s":1.5,"exit":0,"cmd":"go test"}`,
	``, `[]`, `null`, `"x"`, `{`, `{"exit"`, `{"exit":`, `{"exit":1`, `{"a`, "{\"exit\":1}\r",
}

func TestDecodeRecordAgreesWithEncodingJSON(t *testing.T) {
	for _, l := range oddLines {
		sameAsJSON(t, l)
	}
}

func FuzzDecodeRecord(f *testing.F) {
	for _, l := range oddLines {
		f.Add(l)
	}
	f.Add(`{"v":2,"end":"2026-11-02T08:00:00Z","lane":"` + strings.Repeat("x", 100) + `","tree":"ab","exit":1}`)
	f.Fuzz(func(t *testing.T, line string) { sameAsJSON(t, line) })
}
